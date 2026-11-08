package ui

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"github.com/qepan0520/SysGauge-Go/internal/config"
	"github.com/qepan0520/SysGauge-Go/internal/i18n"
	"github.com/qepan0520/SysGauge-Go/internal/metrics"
	"github.com/qepan0520/SysGauge-Go/internal/sampler"
	"github.com/qepan0520/SysGauge-Go/internal/win32"
)

const (
	timerTooltip = 1
	mkControl    = 0x0004
)

var (
	canvasClassOnce sync.Once
	canvasClassName string
	canvasByHWND    sync.Map
)

// CreateCanvas creates the custom-drawn child window inside parent.
func CreateCanvas(parent uintptr, cfg *config.Config, smp *sampler.Sampler, events <-chan sampler.Event, hooks Hooks) (uintptr, *Dash, error) {
	canvasClassOnce.Do(func() {
		name, err := win32.RegisterClass("SysGaugeCanvas", canvasProc)
		if err != nil {
			return
		}
		canvasClassName = name
	})
	if canvasClassName == "" {
		return 0, nil, fmt.Errorf("cannot register canvas window class")
	}
	w, h := win32.ClientRect(parent)
	hwnd, err := win32.CreateWindow(canvasClassName, "", 0,
		win32.WS_CHILD|win32.WS_VISIBLE|win32.WS_CLIPSIBLINGS, 0, 0, int32(w), int32(h), parent)
	if err != nil {
		return 0, nil, err
	}
	dash := NewDash(hwnd, cfg, smp, events, hooks)
	canvasByHWND.Store(hwnd, dash)
	win32.SetFocus(hwnd)
	return hwnd, dash, nil
}

func canvasProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	value, ok := canvasByHWND.Load(hwnd)
	if !ok {
		return win32.DefWindowProc(hwnd, msg, wparam, lparam)
	}
	d := value.(*Dash)

	switch msg {
	case win32.WM_PAINT:
		d.onPaint()
		return 0
	case win32.WM_ERASEBKGND:
		return 1
	case win32.WM_SIZE:
		d.Invalidate()
		return 0
	case win32.WM_MOUSEMOVE:
		x, y := mousePoint(lparam)
		d.onMouseMove(x, y)
		return 0
	case win32.WM_MOUSELEAVE:
		d.onMouseLeave()
		return 0
	case win32.WM_LBUTTONDOWN:
		x, y := mousePoint(lparam)
		d.onMouseDown(x, y)
		return 0
	case win32.WM_LBUTTONUP:
		x, y := mousePoint(lparam)
		d.onMouseUp(x, y)
		return 0
	case win32.WM_LBUTTONDBLCLK:
		x, y := mousePoint(lparam)
		d.onDoubleClick(x, y)
		return 0
	case win32.WM_MOUSEWHEEL:
		x, y := mousePoint(lparam)
		p := win32.ScreenToClient(hwnd, win32.Point{X: int32(x), Y: int32(y)})
		d.onWheel(int(int16((wparam>>16)&0xFFFF)), int(p.X), int(p.Y), wparam&mkControl != 0)
		return 0
	case win32.WM_KEYDOWN:
		d.onKeyDown(int(wparam))
		return 0
	case win32.WM_CHAR:
		d.onChar(rune(wparam))
		return 0
	case win32.WM_KILLFOCUS:
		d.commitEdit()
		d.Invalidate()
		return 0
	case win32.WM_TIMER:
		if wparam == timerTooltip {
			d.onTooltipTimer()
		}
		return 0
	case win32.WM_SETCURSOR:
		d.onSetCursor()
		return 1
	}

	if msg == MessageSample {
		d.drainEvents()
		return 0
	}
	if msg == MessageActivate {
		if d.hooks.Activate != nil {
			d.hooks.Activate()
		}
		return 0
	}
	return win32.DefWindowProc(hwnd, msg, wparam, lparam)
}

func mousePoint(lparam uintptr) (int, int) {
	return int(int16(lparam & 0xFFFF)), int(int16((lparam >> 16) & 0xFFFF))
}

func (d *Dash) onPaint() {
	dc, ps := win32.BeginPaint(d.hwnd)
	w, h := win32.ClientRect(d.hwnd)
	if w > 0 && h > 0 {
		if d.surf.Resize(w, h) {
			d.cov.Ensure(w, h)
			d.paint()
			d.surf.Blit(dc)
		}
	}
	win32.EndPaint(d.hwnd, ps)
}

func (d *Dash) onMouseMove(x, y int) {
	d.mouseX, d.mouseY = x, y

	if d.dragging {
		dx := x - d.dragX
		if dx != 0 {
			d.dragMoved = true
			d.dragX = x
			d.chart.PanPixels(dx, d.plotRect.W)
			win32.InvalidateArea(d.hwnd, int32(d.layout.chart.X), int32(d.layout.chart.Y),
				int32(d.layout.chart.W), int32(d.layout.chart.H))
		}
		return
	}

	id, _ := d.hitTest(x, y)
	if id != d.hover {
		d.hover = id
		d.tooltipOn = false
		d.tooltipText = d.tooltipFor(id)
		win32.KillTimer(d.hwnd, timerTooltip)
		if d.tooltipText != "" {
			win32.SetTimer(d.hwnd, timerTooltip, 650)
			d.hoverTick = win32.TickCount()
		}
		d.Invalidate()
	} else if d.tooltipOn {
		d.tooltipOn = false
		d.Invalidate()
	}

	if id == idChart {
		if d.chart.SetHover(d.chart.XToValue(x, d.plotRect)) {
			win32.InvalidateArea(d.hwnd, int32(d.layout.chart.X), int32(d.layout.chart.Y),
				int32(d.layout.chart.W), int32(d.layout.chart.H))
		}
	} else if d.chart.Hovering {
		d.chart.Hovering = false
		win32.InvalidateArea(d.hwnd, int32(d.layout.chart.X), int32(d.layout.chart.Y),
			int32(d.layout.chart.W), int32(d.layout.chart.H))
	}
	win32.TrackLeave(d.hwnd)
}

func (d *Dash) onMouseLeave() {
	d.hover = -1
	d.tooltipOn = false
	win32.KillTimer(d.hwnd, timerTooltip)
	d.chart.Hovering = false
	d.Invalidate()
}

func (d *Dash) onMouseDown(x, y int) {
	win32.SetFocus(d.hwnd)
	if d.editing {
		if c := d.findControl(idInterval); c == nil || !c.rect.Contains(x, y) {
			d.commitEdit()
		}
	}
	d.mouseX, d.mouseY = x, y
	id, _ := d.hitTest(x, y)
	d.pressX, d.pressY = x, y

	if id == idChart {
		d.dragging = true
		d.dragX = x
		d.dragMoved = false
		return
	}
	d.active = id
	d.Invalidate()
}

func (d *Dash) onMouseUp(x, y int) {
	if d.dragging {
		d.dragging = false
		d.active = -1
		d.Invalidate()
		return
	}
	if d.active < 0 {
		return
	}
	id, _ := d.hitTest(x, y)
	active := d.active
	d.active = -1
	if id == active || (active == idWindow1 && id == idWindow1) {
		d.activate(active, x)
	}
	d.Invalidate()
}

func (d *Dash) onDoubleClick(x, y int) {
	if id, _ := d.hitTest(x, y); id == idChart {
		d.chart.ResetView()
		d.Invalidate()
	}
}

func (d *Dash) onWheel(delta int, x, y int, ctrl bool) {
	if id, _ := d.hitTest(x, y); id != idChart {
		return
	}
	factor := 0.85
	if delta < 0 {
		factor = 1.0 / 0.85
	}
	pivot := d.chart.XToValue(x, d.plotRect)
	d.chart.Zoom(factor, pivot)
	win32.InvalidateArea(d.hwnd, int32(d.layout.chart.X), int32(d.layout.chart.Y),
		int32(d.layout.chart.W), int32(d.layout.chart.H))
}

func (d *Dash) onKeyDown(key int) {
	if !d.editing {
		return
	}
	switch key {
	case win32.VK_ESCAPE:
		d.editing = false
		d.Invalidate()
	case win32.VK_RETURN:
		d.commitEdit()
	case win32.VK_BACK:
		if len(d.editText) > 0 {
			d.editText = d.editText[:len(d.editText)-1]
			d.Invalidate()
		}
	}
}

func (d *Dash) onChar(ch rune) {
	if !d.editing {
		return
	}
	if (ch >= '0' && ch <= '9') || ch == '.' {
		if len(d.editText) < 6 {
			d.editText = append(d.editText, ch)
			d.Invalidate()
		}
	}
}

func (d *Dash) onSetCursor() {
	switch d.hover {
	case idStart, idStop, idHistory, idChooseDir, idOpenDir, idPreset1, idPreset2, idPreset3, idPreset4,
		idCores, idFreq, idSwap, idTray, idLanguage, idWindow1, idAutoScale, idIntervalUp, idIntervalDown:
		win32.SetCursor(win32.IDC_HAND)
	case idInterval:
		win32.SetCursor(win32.IDC_IBEAM)
	case idChart:
		if d.dragging {
			win32.SetCursor(win32.IDC_SIZEWE)
			return
		}
		win32.SetCursor(win32.IDC_ARROW)
	default:
		win32.SetCursor(win32.IDC_ARROW)
	}
}

func (d *Dash) onTooltipTimer() {
	win32.KillTimer(d.hwnd, timerTooltip)
	if d.hover < 0 || d.tooltipText == "" {
		return
	}
	d.tooltipOn = true
	d.Invalidate()
}

func (d *Dash) hitTest(x, y int) (int, int) {
	for i := len(d.controls) - 1; i >= 0; i-- {
		c := d.controls[i]
		if !c.rect.Contains(x, y) {
			continue
		}
		switch {
		case c.kind == kindSegmented:
			return c.id, d.segmentAt(c, x)
		case c.id == idInterval:
			if d.stepUp.Contains(x, y) {
				return idIntervalUp, -1
			}
			if d.stepDown.Contains(x, y) {
				return idIntervalDown, -1
			}
			return idInterval, -1
		default:
			if c.id == 0 {
				return 0, -1
			}
			return c.id, -1
		}
	}
	return idNone, -1
}

func (d *Dash) findControl(id int) *control {
	for i := range d.controls {
		if d.controls[i].id == id {
			return &d.controls[i]
		}
	}
	return nil
}

func (d *Dash) tooltipFor(id int) string {
	switch id {
	case idInterval:
		return i18n.Tr("interval.tooltip")
	case idPreset1:
		return i18n.Tr("preset.tooltip", "sec", "0.1")
	case idPreset2:
		return i18n.Tr("preset.tooltip", "sec", "0.5")
	case idPreset3:
		return i18n.Tr("preset.tooltip", "sec", "1")
	case idPreset4:
		return i18n.Tr("preset.tooltip", "sec", "5")
	case idWindow1:
		return i18n.Tr("window.tooltip")
	case idAutoScale:
		return i18n.Tr("auto_scale.tooltip")
	case idChooseDir, idOpenDir:
		return i18n.Tr("dir.tooltip")
	case idTray:
		if !d.trayAvailable {
			return i18n.Tr("tray.disabled.tooltip")
		}
	}
	return ""
}

// activate runs the action behind a control.
func (d *Dash) activate(id int, x int) {
	log.Printf("触发控件 id=%d x=%d", id, x)
	switch {
	case id == idStart:
		d.startRecording()
	case id == idStop:
		d.stopRecording(true)
	case id >= idPreset1 && id <= idPreset4:
		presets := []float64{0.1, 0.5, 1, 5}
		d.applyInterval(presets[id-idPreset1])
	case id == idInterval:
		d.beginEdit()
	case id == idIntervalUp:
		d.applyInterval(d.cfg.IntervalSec + 0.1)
	case id == idIntervalDown:
		d.applyInterval(d.cfg.IntervalSec - 0.1)
	case id == idChooseDir:
		if d.hooks.ChooseDir != nil {
			if dir, ok := d.hooks.ChooseDir(); ok && dir != "" {
				d.cfg.SaveDir = dir
				_ = d.cfg.Save()
				d.Invalidate()
			}
		}
	case id == idOpenDir:
		if d.hooks.OpenDir != nil {
			d.hooks.OpenDir()
		}
	case id == idHistory:
		if d.hooks.OpenHistory != nil {
			d.hooks.OpenHistory()
		}
	case id == idCores:
		d.cfg.EnableCores = !d.cfg.EnableCores
		d.applyFlags()
	case id == idFreq:
		d.cfg.EnableFreq = !d.cfg.EnableFreq
		d.applyFlags()
	case id == idSwap:
		d.cfg.EnableSwap = !d.cfg.EnableSwap
		d.applyFlags()
	case id == idTray:
		if d.trayAvailable {
			d.cfg.MinimizeToTray = !d.cfg.MinimizeToTray
			_ = d.cfg.Save()
			d.Invalidate()
		}
	case id == idAutoScale:
		d.chart.AutoScale = !d.chart.AutoScale
		d.chart.keyValid = false
		d.cfg.AutoScale = d.chart.AutoScale
		_ = d.cfg.Save()
		d.Invalidate()
	case id == idWindow1:
		if c := d.findControl(idWindow1); c != nil {
			index := d.segmentAt(*c, x)
			windows := []float64{60, 300, 900, 3600}
			if index >= 0 && index < len(windows) {
				d.chart.SetWindow(windows[index])
				d.Invalidate()
			}
		}
	case id == idLanguage:
		d.openLanguageMenu()
	}
}

func (d *Dash) applyFlags() {
	d.sampler.SetFlags(d.cfg.EnableCores, d.cfg.EnableFreq, d.cfg.EnableSwap)
	_ = d.cfg.Save()
	d.Invalidate()
}

func (d *Dash) beginEdit() {
	d.editing = true
	d.editText = []rune(fmt.Sprintf("%.2f", d.cfg.IntervalSec))
	d.editCaret = len(d.editText)
	d.Invalidate()
}

func (d *Dash) commitEdit() {
	if !d.editing {
		return
	}
	text := string(d.editText)
	d.editing = false
	if value, ok := parseInterval(text); ok {
		d.applyInterval(value)
	}
	d.Invalidate()
}

func parseInterval(text string) (float64, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		return 0, false
	}
	if value < config.MinInterval {
		value = config.MinInterval
	}
	if value > config.MaxInterval {
		value = config.MaxInterval
	}
	return value, true
}

func (d *Dash) applyInterval(value float64) {
	if value < config.MinInterval {
		value = config.MinInterval
	}
	if value > config.MaxInterval {
		value = config.MaxInterval
	}
	d.cfg.IntervalSec = value
	_ = d.cfg.Save()
	d.sampler.SetInterval(value)
	d.Invalidate()
}

func (d *Dash) startRecording() {
	if d.sampler.Recording() {
		return
	}
	if !d.cfg.EnsureSaveDir() {
		message := i18n.Tr("error.dir_unavailable.body", "dir", d.cfg.SaveDir)
		if d.hooks.Error != nil {
			d.hooks.Error(i18n.Tr("error.dir_unavailable"), message)
		}
		d.Invalidate()
		return
	}
	_ = d.cfg.Save()
	d.sampler.SetInterval(d.cfg.IntervalSec)
	d.sampler.SetFlags(d.cfg.EnableCores, d.cfg.EnableFreq, d.cfg.EnableSwap)
	d.chart.Clear()
	d.rows = 0
	d.lastWrite = ""

	path, err := d.sampler.Start(d.cfg.SaveDir)
	if err != nil {
		log.Printf("开始记录失败: %v", err)
		if d.hooks.Error != nil {
			d.hooks.Error(i18n.Tr("notify.cannot_start"), err.Error())
		}
		return
	}
	log.Printf("开始记录: %s", path)
	d.recording = true
	d.filePath = path
	if d.hooks.RecordingChanged != nil {
		d.hooks.RecordingChanged()
	}
	if d.hooks.Notify != nil {
		d.hooks.Notify(i18n.Tr("notify.started"), i18n.Tr("notify.started.body", "name", baseName(path)))
	}
	d.Invalidate()
}

func (d *Dash) stopRecording(notify bool) {
	if !d.sampler.Recording() {
		d.recording = false
		d.Invalidate()
		return
	}
	path := d.sampler.Stop()
	rows := d.sampler.RowCount()
	log.Printf("停止记录: %s (%d 行)", path, rows)
	d.recording = false
	if d.hooks.RecordingChanged != nil {
		d.hooks.RecordingChanged()
	}
	if notify && path != "" && d.hooks.Notify != nil {
		d.hooks.Notify(i18n.Tr("notify.stopped"),
			i18n.Tr("notify.stopped.body", "n", fmt.Sprintf("%d", rows), "name", baseName(path)))
	}
	d.Invalidate()
}

// StartRecording is the tray-facing entry point.
func (d *Dash) StartRecording() { d.startRecording() }

// StopRecording is the tray-facing entry point.
func (d *Dash) StopRecording() { d.stopRecording(true) }

// Recording reports whether a session is running.
func (d *Dash) Recording() bool { return d.sampler.Recording() }

// drainEvents consumes sampler events queued on the sampler goroutine.
func (d *Dash) drainEvents() {
	if d.events == nil {
		return
	}
	dirty := false
	for {
		select {
		case event := <-d.events:
			switch event.Kind {
			case sampler.EventSample:
				if event.Sample != nil {
					d.onSample(event.Sample)
					d.rows = event.Rows
					dirty = true
				}
			case sampler.EventStarted:
				d.recording = true
				d.filePath = event.Path
				dirty = true
			case sampler.EventStopped:
				d.recording = false
				dirty = true
			case sampler.EventRotated:
				d.filePath = event.Path
				if d.hooks.Notify != nil {
					d.hooks.Notify(i18n.Tr("notify.file_rotated"), baseName(event.Path))
				}
				dirty = true
			case sampler.EventWriteError:
				d.recording = false
				message := ""
				if event.Err != nil {
					message = event.Err.Error()
				}
				if d.hooks.Error != nil {
					d.hooks.Error(i18n.Tr("notify.write_failed"),
						i18n.Tr("notify.write_failed.body", "error", message))
				}
				dirty = true
			}
		default:
			if dirty {
				d.Invalidate()
			}
			return
		}
	}
}

func (d *Dash) onSample(sample *metrics.Sample) {
	d.lastSample = sample
	wall := float64(sample.TimeUnixMs) / 1000
	d.chart.Append(wall, sample.CPUPercent, sample.MemPercent)
	if iso := sample.TimeISO; len(iso) >= 19 {
		d.lastWrite = iso[11:19]
	}
	if d.hooks.Sample != nil {
		d.hooks.Sample(sample)
	}
}

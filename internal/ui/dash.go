package ui

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"github.com/qepan0520/SysGauge-Go/internal/config"
	"github.com/qepan0520/SysGauge-Go/internal/i18n"
	"github.com/qepan0520/SysGauge-Go/internal/metrics"
	"github.com/qepan0520/SysGauge-Go/internal/sampler"
	"github.com/qepan0520/SysGauge-Go/internal/win32"
)

// Control identifiers used for hit testing.
const (
	idNone = iota
	idStart
	idStop
	idInterval
	idIntervalUp
	idIntervalDown
	idPreset1
	idPreset2
	idPreset3
	idPreset4
	idChooseDir
	idOpenDir
	idHistory
	idCores
	idFreq
	idSwap
	idTray
	idLanguage
	idWindow1
	idWindow2
	idWindow3
	idWindow4
	idAutoScale
	idChart
)

// MessageSample tells the dashboard that sampler events are waiting.
const MessageSample = win32.WM_APP + 1

// MessageActivate asks the dashboard to raise the host window.
const MessageActivate = win32.WM_APP + 2

type controlKind int

const (
	kindButton controlKind = iota
	kindCheckbox
	kindSegmented
	kindField
	kindDropdown
)

type control struct {
	id       int
	kind     controlKind
	rect     Rect
	label    string
	suffix   string
	style    ButtonStyle
	segments []string
	selected int
	checked  bool
	disabled bool
	readOnly bool
}

type textItem struct {
	text   string
	rect   Rect
	size   float64
	muted  bool
	bold   bool
	color  color.RGBA
	hasCol bool
	align  uint32
}

// Hooks connects the dashboard to the hosting window.
type Hooks struct {
	Notify           func(title, message string)
	Error            func(title, message string)
	ChooseDir        func() (string, bool)
	OpenDir          func()
	OpenHistory      func()
	LanguageChanged  func()
	RecordingChanged func()
	Sample           func(*metrics.Sample)
	Activate         func()
}

// Dash is the custom-drawn content area of the main window.
type Dash struct {
	hwnd uintptr

	surf  *win32.Surface
	cov   *win32.Paint
	ctx   *Ctx
	theme *Theme
	scale float64
	dpi   int

	cfg     *config.Config
	sampler *sampler.Sampler
	events  <-chan sampler.Event
	hooks   Hooks

	chart      *Chart
	lastSample *metrics.Sample
	rows       int
	recording  bool
	filePath   string
	lastWrite  string

	trayAvailable bool

	controls []control
	labels   []textItem
	hover    int
	active   int
	pressX   int
	pressY   int

	stepUp   Rect
	stepDown Rect

	editing   bool
	editText  []rune
	editCaret int

	mouseX, mouseY int
	hoverTick      uint64
	tooltipOn      bool
	tooltipText    string

	dragging  bool
	dragX     int
	dragMoved bool

	plotRect Rect
	layout   recorderLayout
}

type recorderLayout struct {
	recorder Rect
	values   Rect
	chart    Rect
	status   Rect
}

// NewDash creates the dashboard for a window handle.
func NewDash(hwnd uintptr, cfg *config.Config, smp *sampler.Sampler, events <-chan sampler.Event, hooks Hooks) *Dash {
	d := &Dash{
		hwnd:          hwnd,
		surf:          win32.NewSurface(),
		cov:           &win32.Paint{},
		theme:         LoadTheme(),
		cfg:           cfg,
		sampler:       smp,
		events:        events,
		hooks:         hooks,
		chart:         NewChart(false),
		hover:         -1,
		active:        -1,
		trayAvailable: true,
	}
	d.dpi = win32.DPIForWindow(hwnd)
	if d.dpi <= 0 {
		d.dpi = 96
	}
	d.scale = float64(d.dpi) / 96.0
	if d.scale < 1 {
		d.scale = 1
	}
	d.ctx = &Ctx{S: d.surf, P: d.cov, T: d.theme, Scale: d.scale}
	d.chart.SetWindow(300)
	d.chart.SetAutoScale(cfg.AutoScale)
	d.recording = smp.Recording()
	if d.recording {
		d.filePath = smp.DataPath()
	}
	return d
}

// Chart exposes the live chart.
func (d *Dash) Chart() *Chart { return d.chart }

// HWND returns the canvas window handle.
func (d *Dash) HWND() uintptr { return d.hwnd }

// SetTheme replaces the palette and repaints.
func (d *Dash) SetTheme(theme *Theme) {
	d.theme = theme
	d.ctx.T = theme
	d.Invalidate()
}

// SetTrayAvailable enables or disables the tray checkbox.
func (d *Dash) SetTrayAvailable(available bool) {
	d.trayAvailable = available
	if !available {
		d.cfg.MinimizeToTray = false
	}
	d.Invalidate()
}

// SetRecordingState refreshes the button state after a session change.
func (d *Dash) SetRecordingState(recording bool, path string) {
	d.recording = recording
	if path != "" {
		d.filePath = path
	}
	d.Invalidate()
}

// Invalidate repaints the whole canvas.
func (d *Dash) Invalidate() { win32.Invalidate(d.hwnd) }

// UpdateDPI re-scales the drawing when the window moves to another monitor.
func (d *Dash) UpdateDPI(dpi int) {
	if dpi <= 0 || dpi == d.dpi {
		return
	}
	d.dpi = dpi
	d.scale = math.Max(1, float64(dpi)/96)
	d.ctx.Scale = d.scale
	d.Invalidate()
}

// p converts logical units to device pixels.
func (d *Dash) p(v float64) int { return int(math.Round(v * d.scale)) }

func (d *Dash) font(size float64, weight int) *win32.Font { return d.ctx.Font(size, weight) }

func (d *Dash) textWidth(text string, size float64, weight int) int {
	return d.ctx.Measure(text, d.font(size, weight))
}

// layoutControls recomputes every rectangle for the current canvas size.
func (d *Dash) layoutControls() {
	w, h := d.surf.W, d.surf.H
	d.controls = d.controls[:0]
	d.labels = d.labels[:0]
	if w < d.p(640) || h < d.p(380) {
		return
	}

	m := d.p(14)
	gap := d.p(8)
	rowH := d.p(30)
	inner := d.p(14)

	cardX := m
	cardW := w - 2*m
	row1 := m + inner
	row2 := row1 + rowH + d.p(10)
	row3 := row2 + rowH + d.p(10)
	d.layout.recorder = Rect{X: cardX, Y: m, W: cardW, H: row3 + rowH + inner - m}

	addLabel := func(text string, rect Rect, muted bool) {
		d.labels = append(d.labels, textItem{text: text, rect: rect, size: 13, muted: muted, align: win32.DT_LEFT})
	}

	x := cardX + inner
	startW := d.textWidth(i18n.Tr("btn.start"), 13, win32.FW_NORMAL) + d.p(30)
	stopW := d.textWidth(i18n.Tr("btn.stop"), 13, win32.FW_NORMAL) + d.p(30)
	d.controls = append(d.controls,
		control{id: idStart, kind: kindButton, rect: Rect{x, row1, startW, rowH}, label: i18n.Tr("btn.start"), style: ButtonPrimary},
	)
	x += startW + gap
	d.controls = append(d.controls,
		control{id: idStop, kind: kindButton, rect: Rect{x, row1, stopW, rowH}, label: i18n.Tr("btn.stop"), style: ButtonDanger},
	)
	x += stopW + d.p(18)

	intervalLabel := i18n.Tr("label.interval")
	labelW := d.textWidth(intervalLabel, 13, win32.FW_NORMAL)
	addLabel(intervalLabel, Rect{x, row1, labelW, rowH}, true)
	x += labelW + gap

	fieldW := d.p(124)
	d.controls = append(d.controls,
		control{id: idInterval, kind: kindField, rect: Rect{x, row1, fieldW, rowH}, suffix: i18n.Tr("interval.suffix")},
	)
	x += fieldW + gap

	presets := []float64{0.1, 0.5, 1, 5}
	for i, preset := range presets {
		d.controls = append(d.controls, control{
			id:    idPreset1 + i,
			kind:  kindButton,
			rect:  Rect{x, row1 + (rowH-d.p(26))/2, d.p(46), d.p(26)},
			label: fmt.Sprintf("%gs", preset),
			style: ButtonSecondary,
		})
		x += d.p(46) + d.p(6)
	}

	if d.effectiveInterval() < 0.5 {
		hint := i18n.Tr("hint.highfreq")
		hintW := d.textWidth(hint, 11.5, win32.FW_NORMAL)
		d.labels = append(d.labels, textItem{text: hint, size: 11.5, rect: Rect{x + d.p(8), row1, hintW, rowH}, color: d.theme.Warn, hasCol: true, align: win32.DT_LEFT})
	}

	histLabel := i18n.Tr("btn.history")
	histW := d.textWidth(histLabel, 13, win32.FW_NORMAL) + d.p(28)
	d.controls = append(d.controls, control{
		id: idHistory, kind: kindButton, style: ButtonSecondary, label: histLabel,
		rect: Rect{w - m - inner - histW, row1, histW, rowH},
	})

	// Row 2: save folder
	x = cardX + inner
	dirLabel := i18n.Tr("label.save_dir")
	dirW := d.textWidth(dirLabel, 13, win32.FW_NORMAL)
	addLabel(dirLabel, Rect{x, row2, dirW, rowH}, true)
	x += dirW + gap

	chooseLabel := i18n.Tr("btn.choose")
	openLabel := i18n.Tr("btn.open_dir")
	chooseW := d.textWidth(chooseLabel, 13, win32.FW_NORMAL) + d.p(28)
	openW := d.textWidth(openLabel, 13, win32.FW_NORMAL) + d.p(28)
	pathW := (cardX + cardW - inner - chooseW - gap - openW - gap) - x
	d.controls = append(d.controls,
		control{id: 0, kind: kindField, rect: Rect{x, row2, pathW, rowH}, label: d.cfg.SaveDir, readOnly: true, disabled: true},
	)
	x += pathW + gap
	d.controls = append(d.controls,
		control{id: idChooseDir, kind: kindButton, rect: Rect{x, row2, chooseW, rowH}, label: chooseLabel, style: ButtonSecondary},
	)
	x += chooseW + gap
	d.controls = append(d.controls,
		control{id: idOpenDir, kind: kindButton, rect: Rect{x, row2, openW, rowH}, label: openLabel, style: ButtonSecondary},
	)

	// Row 3: metrics, tray, language
	x = cardX + inner
	metricsLabel := i18n.Tr("label.metrics")
	metricsW := d.textWidth(metricsLabel, 13, win32.FW_NORMAL)
	addLabel(metricsLabel, Rect{x, row3, metricsW, rowH}, true)
	x += metricsW + gap

	coresLabel := i18n.Tr("check.cores")
	freqLabel := i18n.Tr("check.freq")
	swapLabel := i18n.Tr("check.swap")
	coresW := d.p(23) + d.textWidth(coresLabel, 13, win32.FW_NORMAL)
	freqW := d.p(23) + d.textWidth(freqLabel, 13, win32.FW_NORMAL)
	swapW := d.p(23) + d.textWidth(swapLabel, 13, win32.FW_NORMAL)
	d.controls = append(d.controls,
		control{id: idCores, kind: kindCheckbox, rect: Rect{x, row3, coresW, rowH}, label: coresLabel, checked: d.cfg.EnableCores},
	)
	x += coresW + d.p(18)
	d.controls = append(d.controls,
		control{id: idFreq, kind: kindCheckbox, rect: Rect{x, row3, freqW, rowH}, label: freqLabel, checked: d.cfg.EnableFreq},
	)
	x += freqW + d.p(18)
	d.controls = append(d.controls,
		control{id: idSwap, kind: kindCheckbox, rect: Rect{x, row3, swapW, rowH}, label: swapLabel, checked: d.cfg.EnableSwap},
	)
	x += swapW + d.p(26)

	trayLabel := i18n.Tr("check.tray")
	trayW := d.p(23) + d.textWidth(trayLabel, 13, win32.FW_NORMAL)
	d.controls = append(d.controls,
		control{id: idTray, kind: kindCheckbox, rect: Rect{x, row3, trayW, rowH}, label: trayLabel, checked: d.cfg.MinimizeToTray, disabled: !d.trayAvailable},
	)

	langLabel := i18n.Tr("label.language")
	langW := d.textWidth(langLabel, 13, win32.FW_NORMAL)
	dropdownW := d.p(132)
	dropdownX := cardX + cardW - inner - dropdownW
	addLabel(langLabel, Rect{dropdownX - gap - langW, row3, langW, rowH}, true)
	d.controls = append(d.controls,
		control{id: idLanguage, kind: kindDropdown, rect: Rect{dropdownX, row3, dropdownW, rowH}, label: i18n.Name(i18n.Current())},
	)

	// Value card
	d.layout.values = Rect{X: m, Y: d.layout.recorder.Bottom() + d.p(10), W: cardW, H: d.p(84)}

	// Status bar
	statusH := d.p(30)
	d.layout.status = Rect{X: 0, Y: h - statusH, W: w, H: statusH}

	// Chart card
	chartY := d.layout.values.Bottom() + d.p(10)
	chartH := d.layout.status.Y - d.p(10) - chartY
	if chartH < d.p(130) {
		chartH = d.p(130)
	}
	d.layout.chart = Rect{X: m, Y: chartY, W: cardW, H: chartH}

	headerY := d.layout.chart.Y + d.p(9)
	segX := d.layout.chart.X + d.p(14)
	segLabels := []string{
		i18n.Tr("window.60"), i18n.Tr("window.300"), i18n.Tr("window.900"), i18n.Tr("window.3600"),
	}
	segW := 0
	for _, label := range segLabels {
		segW += d.textWidth(label, 12.5, win32.FW_NORMAL) + d.p(24)
	}
	if segW < d.p(240) {
		segW = d.p(240)
	}
	windowIndex := 1
	switch int(d.chart.WindowSec) {
	case 60:
		windowIndex = 0
	case 900:
		windowIndex = 2
	case 3600:
		windowIndex = 3
	}
	d.controls = append(d.controls, control{
		id: idWindow1, kind: kindSegmented, rect: Rect{segX, headerY, segW, d.p(26)},
		segments: segLabels, selected: windowIndex,
	})
	x = segX + segW + d.p(16)
	autoLabel := i18n.Tr("check.auto_scale")
	autoW := d.p(23) + d.textWidth(autoLabel, 13, win32.FW_NORMAL)
	d.controls = append(d.controls,
		control{id: idAutoScale, kind: kindCheckbox, rect: Rect{x, headerY, autoW, d.p(26)}, label: autoLabel, checked: d.chart.AutoScale},
	)

	chartArea := Rect{
		X: d.layout.chart.X + d.p(14),
		Y: headerY + d.p(34),
		W: d.layout.chart.W - d.p(28),
		H: d.layout.chart.Bottom() - d.p(8) - (headerY + d.p(34)),
	}
	d.plotRect = d.ctx.PlotRect(chartArea)
	d.controls = append(d.controls, control{id: idChart, kind: kindButton, rect: chartArea})
}

func (d *Dash) effectiveInterval() float64 {
	if d.editing {
		if value, ok := parseInterval(string(d.editText)); ok {
			return value
		}
	}
	if d.sampler.Recording() {
		return d.sampler.Interval()
	}
	return d.cfg.IntervalSec
}

// paint renders the whole dashboard.
func (d *Dash) paint() {
	if d.surf.W == 0 || d.surf.H == 0 {
		return
	}
	d.surf.Fill(d.theme.Window)
	d.layoutControls()
	if d.layout.recorder.W == 0 {
		return
	}

	d.ctx.Panel(d.layout.recorder, d.theme.Panel, d.theme.Border)
	d.ctx.Panel(d.layout.values, d.theme.Panel, d.theme.Border)
	d.ctx.Panel(d.layout.chart, d.theme.Panel, d.theme.Border)

	for _, item := range d.labels {
		col := d.theme.Muted
		if item.hasCol {
			col = item.color
		}
		weight := win32.FW_NORMAL
		if item.bold {
			weight = win32.FW_SEMIBOLD
		}
		d.ctx.Text(item.text, item.rect, d.font(item.size, weight), col, item.align)
	}

	for _, c := range d.controls {
		d.paintControl(c)
	}

	d.paintValues()
	d.paintLegend()
	d.ctx.DrawChart(d.chart, d.plotRect)
	d.paintStatus()

	if d.tooltipOn {
		d.paintTooltip()
	}
}

func (d *Dash) paintControl(c control) {
	hovered := d.hover == c.id
	switch c.kind {
	case kindButton:
		switch c.id {
		case idStart:
			c.disabled = d.recording
		case idStop:
			c.disabled = !d.recording
		}
		if c.id == idChart {
			return
		}
		d.ctx.Button(c.rect, c.label, c.style, hovered, d.active == c.id, c.disabled)
	case kindCheckbox:
		d.ctx.Checkbox(c.rect, c.label, c.checked, hovered && !c.disabled)
	case kindSegmented:
		hoverSegment := -1
		if hovered {
			hoverSegment = d.segmentAt(c, d.mouseX)
		}
		d.ctx.Segmented(c.rect, c.segments, c.selected, hoverSegment)
	case kindField:
		if c.id == idInterval {
			text := fmt.Sprintf("%.2f", d.effectiveInterval())
			editing := d.editing
			caret := 0
			if editing {
				text = string(d.editText)
				caret = len(d.editText)
			}
			d.ctx.Field(c.rect, text, c.suffix, hovered, editing, caret, d.p(18), false)
			d.stepUp, d.stepDown = d.ctx.Stepper(c.rect, d.hover == idIntervalUp, d.hover == idIntervalDown)
			return
		}
		text := ellipsize(c.label, d.font(13, win32.FW_NORMAL), c.rect.W-d.p(18), d.ctx)
		d.ctx.Field(c.rect, text, "", false, false, 0, 0, false)
	case kindDropdown:
		d.ctx.Dropdown(c.rect, c.label, hovered, false)
	}
}

func (d *Dash) paintValues() {
	values := d.layout.values
	left := values.X + d.p(16)
	labelFont := d.font(13, win32.FW_NORMAL)
	bigFont := d.font(25, win32.FW_SEMIBOLD)
	midFont := d.font(15, win32.FW_NORMAL)
	smallFont := d.font(11.5, win32.FW_NORMAL)

	rowTop := values.Y + d.p(10)
	rowH := d.p(34)

	cpuLabel := i18n.Tr("label.cpu")
	w := d.textWidth(cpuLabel, 13, win32.FW_NORMAL)
	d.ctx.Text(cpuLabel, Rect{left, rowTop, w, rowH}, labelFont, d.theme.Muted, win32.DT_LEFT)
	left += w + d.p(10)

	cpuText := "--"
	if d.lastSample != nil && d.lastSample.CPUPercent != nil {
		cpuText = fmt.Sprintf("%.1f%%", *d.lastSample.CPUPercent)
	}
	cpuW := d.textWidth(cpuText, 25, win32.FW_SEMIBOLD)
	d.ctx.Text(cpuText, Rect{left, rowTop, cpuW, rowH}, bigFont, d.theme.CPU, win32.DT_LEFT)
	left += cpuW + d.p(30)

	memLabel := i18n.Tr("label.mem")
	w = d.textWidth(memLabel, 13, win32.FW_NORMAL)
	d.ctx.Text(memLabel, Rect{left, rowTop, w, rowH}, labelFont, d.theme.Muted, win32.DT_LEFT)
	left += w + d.p(10)

	memText := "--"
	detail := ""
	if d.lastSample != nil && d.lastSample.MemPercent != nil {
		memText = fmt.Sprintf("%.1f%%", *d.lastSample.MemPercent)
		if d.lastSample.MemTotal != nil {
			used := float64(0)
			if d.lastSample.MemUsed != nil {
				used = float64(*d.lastSample.MemUsed) / (1024 * 1024 * 1024)
			}
			detail = fmt.Sprintf("%.2f / %.2f GB", used, float64(*d.lastSample.MemTotal)/(1024*1024*1024))
		}
	}
	memW := d.textWidth(memText, 25, win32.FW_SEMIBOLD)
	d.ctx.Text(memText, Rect{left, rowTop, memW, rowH}, bigFont, d.theme.Mem, win32.DT_LEFT)
	left += memW + d.p(10)
	if detail != "" {
		detailW := d.textWidth(detail, 12, win32.FW_NORMAL)
		d.ctx.Text(detail, Rect{left, rowTop, detailW, rowH}, d.font(12, win32.FW_NORMAL), d.theme.Faint, win32.DT_LEFT)
		left += detailW
	}
	left += d.p(30)

	freqLabel := i18n.Tr("label.freq")
	w = d.textWidth(freqLabel, 13, win32.FW_NORMAL)
	d.ctx.Text(freqLabel, Rect{left, rowTop, w, rowH}, labelFont, d.theme.Muted, win32.DT_LEFT)
	left += w + d.p(10)

	freqText := "--"
	if d.lastSample != nil && d.lastSample.CPUFreqMHz != nil {
		freqText = fmt.Sprintf("%.2f GHz", *d.lastSample.CPUFreqMHz/1000)
	}
	freqW := d.textWidth(freqText, 15, win32.FW_NORMAL)
	d.ctx.Text(freqText, Rect{left, rowTop, freqW, rowH}, midFont, d.theme.Text, win32.DT_LEFT)

	if d.cfg.EnableCores && d.lastSample != nil && len(d.lastSample.Cores) > 0 {
		cores := d.lastSample.Cores
		shown := cores
		if len(shown) > 16 {
			shown = shown[:16]
		}
		parts := make([]string, len(shown))
		sum := 0.0
		for i, value := range shown {
			parts[i] = fmt.Sprintf("%d:%.0f%%", i, value)
		}
		for _, value := range cores {
			sum += value
		}
		text := strings.Join(parts, "  ")
		if len(cores) > len(shown) {
			text += i18n.Tr("cores.more", "n", fmt.Sprintf("%d", len(cores)))
		}
		summary := i18n.Tr("cores.summary",
			"avg", fmt.Sprintf("%.1f", sum/float64(len(cores))),
			"text", text)
		d.ctx.Text(summary, Rect{values.X + d.p(16), values.Y + d.p(48), values.W - d.p(32), d.p(20)},
			smallFont, d.theme.Faint, win32.DT_LEFT|win32.DT_END_ELLIPSIS)
	}
}

func (d *Dash) paintLegend() {
	cx := d.layout.chart.Right() - d.p(16)
	font := d.font(11.5, win32.FW_NORMAL)
	draw := func(text string, col color.RGBA) {
		labelW := d.textWidth(text, 11.5, win32.FW_NORMAL)
		cx -= labelW
		d.ctx.Text(text, Rect{cx, d.layout.chart.Y + d.p(9), labelW, d.p(26)}, font, d.theme.Muted, win32.DT_LEFT)
		lineW := d.p(14)
		cx -= lineW + d.p(6)
		y := d.layout.chart.Y + d.p(9) + d.p(13)
		d.cov.Segment(float64(cx), float64(y), float64(cx+lineW), float64(y), 2.2*d.scale)
		d.cov.Composite(d.surf, col)
	}
	draw(i18n.Tr("chart.mem"), d.theme.Mem)
	cx -= d.p(18)
	draw("CPU", d.theme.CPU)
}

func (d *Dash) paintStatus() {
	status := d.layout.status
	if status.W == 0 {
		return
	}
	d.surf.Line(0, status.Y, status.W, status.Y, d.theme.BorderSoft)

	font := d.font(12, win32.FW_NORMAL)
	dotX := d.p(16)
	dotY := status.Y + status.H/2
	dotColor := d.theme.Muted
	text := i18n.Tr("status.stopped")
	if d.recording {
		dotColor = d.theme.Bad
		text = i18n.Tr("status.recording")
	}
	d.cov.FillCircle(float64(dotX)+4*d.scale, float64(dotY), 4*d.scale)
	d.cov.Composite(d.surf, dotColor)

	textX := dotX + d.p(16)
	d.ctx.Text(text, Rect{textX, status.Y, d.p(120), status.H}, font, d.theme.Muted, win32.DT_LEFT)

	right := status.W - d.p(16)
	if d.lastWrite != "" {
		label := i18n.Tr("status.last_write", "time", d.lastWrite)
		w := d.textWidth(label, 12, win32.FW_NORMAL)
		right -= w
		d.ctx.Text(label, Rect{right, status.Y, w, status.H}, font, d.theme.Faint, win32.DT_LEFT)
		right -= d.p(20)
	}
	rowsLabel := i18n.Tr("status.rows", "n", fmt.Sprintf("%d", d.rows))
	w := d.textWidth(rowsLabel, 12, win32.FW_NORMAL)
	right -= w
	d.ctx.Text(rowsLabel, Rect{right, status.Y, w, status.H}, font, d.theme.Faint, win32.DT_LEFT)
	right -= d.p(20)
	if d.filePath != "" {
		name := baseName(d.filePath)
		w := d.textWidth(name, 12, win32.FW_NORMAL)
		right -= w
		d.ctx.Text(ellipsize(name, font, status.W/2, d.ctx), Rect{right, status.Y, w, status.H}, font, d.theme.Muted, win32.DT_LEFT)
	}
}

func (d *Dash) paintTooltip() {
	if d.tooltipText == "" {
		return
	}
	font := d.font(11.5, win32.FW_NORMAL)
	maxW := d.p(300)
	lines := wrapText(d.tooltipText, maxW, font, d.ctx)
	lineH := int(15 * d.scale)
	padX := d.p(9)
	padY := d.p(7)
	boxW := 0
	for _, line := range lines {
		if w := d.textWidth(line, 11.5, win32.FW_NORMAL); w > boxW {
			boxW = w
		}
	}
	boxW += 2 * padX
	boxH := len(lines)*lineH + 2*padY

	x := d.mouseX + d.p(16)
	y := d.mouseY + d.p(20)
	if x+boxW > d.surf.W-d.p(6) {
		x = d.mouseX - boxW - d.p(12)
	}
	if y+boxH > d.surf.H-d.p(6) {
		y = d.mouseY - boxH - d.p(12)
	}
	if x < d.p(4) {
		x = d.p(4)
	}
	if y < d.p(4) {
		y = d.p(4)
	}
	box := Rect{x, y, boxW, boxH}
	d.ctx.FillRoundRect(Rect{box.X + 2, box.Y + 3, box.W, box.H}, 6, alpha(d.theme.Shadow, 45))
	d.ctx.Panel(box, d.theme.Tooltip, alpha(d.theme.Text, 40))
	for i, line := range lines {
		d.ctx.Text(line, Rect{box.X + padX, box.Y + padY + i*lineH, box.W - 2*padX, lineH},
			font, d.theme.TooltipText, win32.DT_LEFT)
	}
}

// segmentAt returns the segment index under an X coordinate.
func (d *Dash) segmentAt(c control, x int) int {
	if len(c.segments) == 0 || c.rect.W == 0 {
		return -1
	}
	if !c.rect.Contains(x, d.rectCenterY(c.rect)) {
		return -1
	}
	index := (x - c.rect.X) * len(c.segments) / c.rect.W
	if index < 0 || index >= len(c.segments) {
		return -1
	}
	return index
}

func (d *Dash) rectCenterY(r Rect) int { return r.Y + r.H/2 }

func baseName(path string) string {
	if idx := strings.LastIndexAny(path, `\/`); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

func ellipsize(text string, font *win32.Font, maxWidth int, c *Ctx) string {
	if maxWidth <= 0 {
		return ""
	}
	if c.S.Measure(text, font) <= maxWidth {
		return text
	}
	runes := []rune(text)
	for len(runes) > 1 {
		runes = runes[:len(runes)-1]
		candidate := string(runes) + "…"
		if c.S.Measure(candidate, font) <= maxWidth {
			return candidate
		}
	}
	return "…"
}

func wrapText(text string, maxWidth int, font *win32.Font, c *Ctx) []string {
	if text == "" {
		return nil
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{text}
	}
	var lines []string
	current := ""
	for _, word := range words {
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if c.S.Measure(candidate, font) <= maxWidth {
			current = candidate
			continue
		}
		if current != "" {
			lines = append(lines, current)
		}
		current = word
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

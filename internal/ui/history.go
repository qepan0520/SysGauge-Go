package ui

import (
	"fmt"
	"image/color"
	"math"
	"sync"
	"time"

	"github.com/qepan0520/SysGauge-Go/internal/config"
	"github.com/qepan0520/SysGauge-Go/internal/history"
	"github.com/qepan0520/SysGauge-Go/internal/i18n"
	"github.com/qepan0520/SysGauge-Go/internal/win32"
)

const (
	hsOpen = 100 + iota
	hsReload
	hsAutoScale
	hsChart
)

type historyView struct {
	hwnd uintptr

	surf  *win32.Surface
	cov   *win32.Paint
	ctx   *Ctx
	theme *Theme
	scale float64
	dpi   int

	cfg  *config.Config
	host uintptr

	chart *Chart
	data  *history.Data
	path  string
	notes string
	fail  string

	controls []control
	labels   []textItem
	hover    int
	active   int
	mouseX   int
	mouseY   int

	dragging  bool
	dragX     int
	dragMoved bool

	plotRect  Rect
	chartCard Rect
	layout    historyLayout
}

type historyLayout struct {
	toolbar Rect
	summary Rect
	notice  Rect
	chart   Rect
}

var (
	historyClassOnce sync.Once
	historyClassName string
	historyByHWND    sync.Map

	historyHostOnce  sync.Once
	historyHostClass string
	historyHosts     sync.Map
)

// historyWindow is the native frame that hosts the history view.
type historyWindow struct {
	hwnd     uintptr
	canvas   uintptr
	owner    uintptr
	view     *historyView
	cfg      *config.Config
	onClosed func()
}

// newHistoryWindow opens the history viewer in its own top-level window.
func newHistoryWindow(owner uintptr, cfg *config.Config, theme *Theme, icon uintptr, presetPath string, onClosed func()) *historyWindow {
	historyHostOnce.Do(func() {
		name, err := win32.RegisterClass("SysGaugeHistoryHost", historyHostProc)
		if err != nil {
			return
		}
		historyHostClass = name
	})
	if historyHostClass == "" {
		return nil
	}

	scale := float64(win32.SystemDPI()) / 96.0
	width := int(1080 * scale)
	height := int(720 * scale)
	x, y := win32.CenterWindow(width, height)

	hwnd, err := win32.CreateTopLevel(historyHostClass, i18n.Tr("history.title"),
		x, y, width, height, owner, 0, win32.WS_OVERLAPPEDWINDOW|win32.WS_CLIPCHILDREN)
	if err != nil {
		return nil
	}

	host := &historyWindow{hwnd: hwnd, owner: owner, cfg: cfg, onClosed: onClosed}
	view := &historyView{
		cfg:    cfg,
		host:   hwnd,
		theme:  theme,
		chart:  NewChart(true),
		hover:  -1,
		active: -1,
	}
	view.chart.SetAutoScale(cfg.AutoScale)
	now := float64(time.Now().UnixMilli()) / 1000
	view.chart.ViewMax = now
	view.chart.ViewMin = now - DefaultWindowSec
	historyHosts.Store(hwnd, host)

	canvas, err := createHistoryCanvas(hwnd, view)
	if err != nil {
		win32.DestroyWindow(hwnd)
		return nil
	}
	host.canvas = canvas
	host.view = view
	view.hwnd = canvas
	historyByHWND.Store(canvas, view)

	win32.SetIcon(hwnd, icon)
	win32.SetDarkTitleBar(hwnd, theme.Dark)
	win32.EnableWindow(owner, false)
	win32.ShowWindow(hwnd, win32.SW_SHOW)
	if presetPath != "" {
		view.load(presetPath)
	}
	return host
}

func historyHostProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	value, ok := historyHosts.Load(hwnd)
	if !ok {
		return win32.DefWindowProc(hwnd, msg, wparam, lparam)
	}
	host := value.(*historyWindow)

	switch msg {
	case win32.WM_SIZE:
		w, h := win32.ClientRect(hwnd)
		if w > 0 && h > 0 && host.canvas != 0 {
			win32.SetWindowPos(host.canvas, 0, 0, int32(w), int32(h))
			host.view.updateDPI(win32.DPIForWindow(hwnd))
		}
		return 0
	case win32.WM_GETMINMAXINFO:
		scale := float64(win32.DPIForWindow(hwnd)) / 96.0
		info := (*win32.MinMaxInfo)(win32.Ptr(lparam))
		info.PtMinTrackSize.X = int32(860 * scale)
		info.PtMinTrackSize.Y = int32(560 * scale)
		return 0
	case win32.WM_CLOSE:
		win32.DestroyWindow(hwnd)
		return 0
	case win32.WM_DESTROY:
		historyHosts.Delete(hwnd)
		historyByHWND.Delete(host.canvas)
		host.view.surf.Free()
		win32.EnableWindow(host.owner, true)
		win32.SetForegroundWindow(host.owner)
		if host.onClosed != nil {
			host.onClosed()
		}
		return 0
	}
	return win32.DefWindowProc(hwnd, msg, wparam, lparam)
}

func createHistoryCanvas(parent uintptr, view *historyView) (uintptr, error) {
	historyClassOnce.Do(func() {
		name, err := win32.RegisterClass("SysGaugeHistory", historyProc)
		if err != nil {
			return
		}
		historyClassName = name
	})
	if historyClassName == "" {
		return 0, fmt.Errorf("cannot register history window class")
	}
	w, h := win32.ClientRect(parent)
	hwnd, err := win32.CreateWindow(historyClassName, "", 0,
		win32.WS_CHILD|win32.WS_VISIBLE|win32.WS_CLIPSIBLINGS, 0, 0, int32(w), int32(h), parent)
	if err != nil {
		return 0, err
	}
	view.dpi = win32.DPIForWindow(hwnd)
	view.scale = math.Max(1, float64(view.dpi)/96)
	view.surf = win32.NewSurface()
	view.cov = &win32.Paint{}
	view.ctx = &Ctx{S: view.surf, P: view.cov, T: view.theme, Scale: view.scale}
	return hwnd, nil
}

func (v *historyView) updateDPI(dpi int) {
	if dpi <= 0 || dpi == v.dpi {
		return
	}
	v.dpi = dpi
	v.scale = math.Max(1, float64(dpi)/96)
	v.ctx.Scale = v.scale
	win32.Invalidate(v.hwnd)
}

// setTheme applies a new palette to the history window and its canvas.
func (h *historyWindow) setTheme(theme *Theme) {
	if h.view != nil {
		h.view.setTheme(theme)
	}
	win32.SetDarkTitleBar(h.hwnd, theme.Dark)
	win32.RedrawFrame(h.hwnd)
}

func (v *historyView) setTheme(theme *Theme) {
	v.theme = theme
	v.ctx.T = theme
	win32.Invalidate(v.hwnd)
}

func historyProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	value, ok := historyByHWND.Load(hwnd)
	if !ok {
		return win32.DefWindowProc(hwnd, msg, wparam, lparam)
	}
	v := value.(*historyView)

	switch msg {
	case win32.WM_PAINT:
		dc, ps := win32.BeginPaint(hwnd)
		w, h := win32.ClientRect(hwnd)
		if w > 0 && h > 0 && v.surf.Resize(w, h) {
			v.cov.Ensure(w, h)
			v.draw()
			v.surf.Blit(dc)
		}
		win32.EndPaint(hwnd, ps)
		return 0
	case win32.WM_ERASEBKGND:
		return 1
	case win32.WM_SIZE:
		win32.Invalidate(hwnd)
		return 0
	case win32.WM_MOUSEMOVE:
		x, y := mousePoint(lparam)
		v.onMouseMove(x, y)
		return 0
	case win32.WM_MOUSELEAVE:
		v.hover = -1
		v.chart.Hovering = false
		win32.Invalidate(hwnd)
		return 0
	case win32.WM_LBUTTONDOWN:
		x, y := mousePoint(lparam)
		v.onMouseDown(x, y)
		return 0
	case win32.WM_LBUTTONUP:
		x, y := mousePoint(lparam)
		v.onMouseUp(x, y)
		return 0
	case win32.WM_LBUTTONDBLCLK:
		x, y := mousePoint(lparam)
		if id, _ := v.hitTest(x, y); id == hsChart {
			v.chart.ResetView()
			win32.InvalidateArea(hwnd, int32(v.chartCard.X), int32(v.chartCard.Y),
				int32(v.chartCard.W), int32(v.chartCard.H))
		}
		return 0
	case win32.WM_MOUSEWHEEL:
		x, y := mousePoint(lparam)
		p := win32.ScreenToClient(hwnd, win32.Point{X: int32(x), Y: int32(y)})
		v.onWheel(int(int16((wparam>>16)&0xFFFF)), int(p.X), int(p.Y))
		return 0
	case win32.WM_SETCURSOR:
		switch v.hover {
		case hsOpen, hsReload, hsAutoScale:
			win32.SetCursor(win32.IDC_HAND)
		default:
			win32.SetCursor(win32.IDC_ARROW)
		}
		return 1
	}
	return win32.DefWindowProc(hwnd, msg, wparam, lparam)
}

func (v *historyView) onMouseMove(x, y int) {
	v.mouseX, v.mouseY = x, y
	if v.dragging {
		dx := x - v.dragX
		if dx != 0 {
			v.dragMoved = true
			v.dragX = x
			v.chart.PanPixels(dx, v.plotRect.W)
			win32.InvalidateArea(v.hwnd, int32(v.chartCard.X), int32(v.chartCard.Y),
				int32(v.chartCard.W), int32(v.chartCard.H))
		}
		return
	}
	id, _ := v.hitTest(x, y)
	if id != v.hover {
		v.hover = id
		win32.Invalidate(v.hwnd)
	}
	if id == hsChart {
		if v.chart.SetHover(v.chart.XToValue(x, v.plotRect)) {
			win32.InvalidateArea(v.hwnd, int32(v.chartCard.X), int32(v.chartCard.Y),
				int32(v.chartCard.W), int32(v.chartCard.H))
		}
	}
	win32.TrackLeave(v.hwnd)
}

func (v *historyView) onMouseDown(x, y int) {
	win32.SetFocus(v.hwnd)
	v.mouseX, v.mouseY = x, y
	id, _ := v.hitTest(x, y)
	if id == hsChart {
		v.dragging = true
		v.dragX = x
		v.dragMoved = false
		return
	}
	v.active = id
	win32.Invalidate(v.hwnd)
}

func (v *historyView) onMouseUp(x, y int) {
	if v.dragging {
		v.dragging = false
		return
	}
	if v.active < 0 {
		return
	}
	id, _ := v.hitTest(x, y)
	active := v.active
	v.active = -1
	if id == active {
		switch active {
		case hsOpen:
			v.openFile()
		case hsReload:
			if v.path != "" {
				v.load(v.path)
			}
		case hsAutoScale:
			v.chart.AutoScale = !v.chart.AutoScale
			v.chart.keyValid = false
			win32.Invalidate(v.hwnd)
		}
	}
	win32.Invalidate(v.hwnd)
}

func (v *historyView) onWheel(delta, x, y int) {
	if id, _ := v.hitTest(x, y); id != hsChart {
		return
	}
	factor := 0.85
	if delta < 0 {
		factor = 1.0 / 0.85
	}
	v.chart.Zoom(factor, v.chart.XToValue(x, v.plotRect))
	win32.InvalidateArea(v.hwnd, int32(v.chartCard.X), int32(v.chartCard.Y),
		int32(v.chartCard.W), int32(v.chartCard.H))
}

func (v *historyView) hitTest(x, y int) (int, int) {
	for i := len(v.controls) - 1; i >= 0; i-- {
		c := v.controls[i]
		if c.rect.Contains(x, y) {
			return c.id, -1
		}
	}
	return 0, -1
}

func (v *historyView) openFile() {
	path, ok := win32.OpenFileDialog(v.host, i18n.Tr("history.choose"),
		i18n.Tr("history.filter"), v.cfg.SaveDir)
	if !ok || path == "" {
		return
	}
	v.load(path)
}

func (v *historyView) load(path string) {
	win32.SetCursor(win32.IDC_WAIT)
	data, err := history.Parse(path)
	win32.SetCursor(win32.IDC_ARROW)

	v.path = path
	v.data = data
	v.fail = ""
	v.notes = ""

	if err != nil {
		v.chart.Clear()
		switch err {
		case history.ErrEmpty:
			v.fail = i18n.Tr("parse.error.empty")
		case history.ErrNoTimestamp:
			v.fail = i18n.Tr("parse.error.no_timestamp")
		default:
			v.fail = err.Error()
		}
		win32.Invalidate(v.hwnd)
		return
	}

	if data.Count == 0 {
		v.chart.Clear()
		v.notes = i18n.Tr("history.empty")
		win32.Invalidate(v.hwnd)
		return
	}

	v.chart.SetSeries(data.Times, data.CPU, data.Mem)
	var notes []string
	if data.Skipped > 0 {
		notes = append(notes, i18n.Tr("history.skipped", "n", fmt.Sprintf("%d", data.Skipped)))
	}
	if data.Count >= history.BigFileRows {
		notes = append(notes, i18n.Tr("history.downsampled", "n", fmt.Sprintf("%d", data.Count)))
	}
	v.notes = joinNotes(notes)
	win32.Invalidate(v.hwnd)
}

func joinNotes(notes []string) string {
	out := ""
	for i, note := range notes {
		if i > 0 {
			out += "   "
		}
		out += note
	}
	return out
}

func (v *historyView) layoutView() {
	w, h := v.surf.W, v.surf.H
	v.controls = v.controls[:0]
	v.labels = v.labels[:0]
	if w < 200 || h < 200 {
		return
	}
	m := int(math.Round(14 * v.scale))
	rowH := int(math.Round(30 * v.scale))
	inner := int(math.Round(12 * v.scale))

	toolbar := Rect{X: m, Y: m, W: w - 2*m, H: rowH}
	v.layout.toolbar = toolbar

	font := v.ctx.Font(13, win32.FW_NORMAL)
	x := toolbar.X
	openLabel := i18n.Tr("history.open")
	openW := v.ctx.Measure(openLabel, font) + int(math.Round(28*v.scale))
	v.controls = append(v.controls, control{id: hsOpen, kind: kindButton, rect: Rect{x, m, openW, rowH}, label: openLabel, style: ButtonPrimary})
	x += openW + int(math.Round(8*v.scale))

	reloadLabel := i18n.Tr("history.reload")
	reloadW := v.ctx.Measure(reloadLabel, font) + int(math.Round(28*v.scale))
	reload := control{id: hsReload, kind: kindButton, rect: Rect{x, m, reloadW, rowH}, label: reloadLabel, style: ButtonSecondary}
	reload.disabled = v.path == ""
	v.controls = append(v.controls, reload)
	x += reloadW + int(math.Round(16*v.scale))

	autoLabel := i18n.Tr("check.auto_scale")
	autoW := int(math.Round(23*v.scale)) + v.ctx.Measure(autoLabel, font)
	v.controls = append(v.controls, control{id: hsAutoScale, kind: kindCheckbox, rect: Rect{x, m, autoW, rowH}, label: autoLabel, checked: v.chart.AutoScale})

	pathText := v.path
	if pathText == "" {
		pathText = i18n.Tr("history.no_file")
	}
	pathFont := v.ctx.Font(12, win32.FW_NORMAL)
	pathW := v.ctx.Measure(pathText, pathFont)
	maxPath := w - m - x - int(math.Round(200*v.scale))
	if pathW > maxPath {
		pathW = maxPath
	}
	v.labels = append(v.labels, textItem{
		text: ellipsize(pathText, pathFont, pathW, v.ctx), rect: Rect{w - m - pathW, m, pathW, rowH},
		size: 12, muted: true, align: win32.DT_RIGHT,
	})

	summaryH := 3*int(math.Round(36*v.scale)) + 2*inner
	v.layout.summary = Rect{X: m, Y: toolbar.Bottom() + int(math.Round(10*v.scale)), W: w - 2*m, H: summaryH}

	y := v.layout.summary.Bottom() + int(math.Round(8*v.scale))
	if v.notes != "" || v.fail != "" {
		v.layout.notice = Rect{X: m + int(math.Round(4*v.scale)), Y: y, W: w - 2*m, H: int(math.Round(18 * v.scale))}
		y += int(math.Round(24 * v.scale))
	} else {
		v.layout.notice = Rect{}
	}
	v.layout.chart = Rect{X: m, Y: y, W: w - 2*m, H: h - m - y}
	if v.layout.chart.H < int(math.Round(120*v.scale)) {
		v.layout.chart.H = int(math.Round(120 * v.scale))
	}
	v.chartCard = v.layout.chart
	area := Rect{X: v.chartCard.X + int(math.Round(14*v.scale)), Y: v.chartCard.Y + int(math.Round(12*v.scale)),
		W: v.chartCard.W - int(math.Round(28*v.scale)), H: v.chartCard.H - int(math.Round(24*v.scale))}
	v.plotRect = v.ctx.PlotRect(area)
	v.controls = append(v.controls, control{id: hsChart, kind: kindButton, rect: area})
}

func (v *historyView) draw() {
	v.surf.Fill(v.theme.Window)
	v.layoutView()
	if v.layout.toolbar.W == 0 {
		return
	}

	for _, item := range v.labels {
		v.ctx.Text(item.text, item.rect, v.ctx.Font(item.size, win32.FW_NORMAL), v.theme.Muted, item.align)
	}
	for _, c := range v.controls {
		if c.id == hsChart {
			continue
		}
		hovered := v.hover == c.id
		switch c.kind {
		case kindButton:
			v.ctx.Button(c.rect, c.label, c.style, hovered, v.active == c.id, c.disabled)
		case kindCheckbox:
			v.ctx.Checkbox(c.rect, c.label, c.checked, hovered)
		}
	}

	v.drawSummary()
	if v.layout.notice.W > 0 {
		text := v.notes
		color := v.theme.Warn
		if v.fail != "" {
			text = i18n.Tr("history.cannot_read", "error", v.fail)
			color = v.theme.Bad
		}
		v.ctx.Text(text, v.layout.notice, v.ctx.Font(12, win32.FW_NORMAL), color, win32.DT_LEFT|win32.DT_END_ELLIPSIS)
	}

	v.ctx.Panel(v.chartCard, v.theme.Panel, v.theme.Border)
	v.ctx.DrawChart(v.chart, v.plotRect)
	if v.chart.Empty() {
		v.ctx.Text(i18n.Tr("history.no_file"), Rect{v.plotRect.X, v.plotRect.Y, v.plotRect.W, v.plotRect.H},
			v.ctx.Font(13, win32.FW_NORMAL), v.theme.Faint, win32.DT_CENTER|win32.DT_VCENTER)
	}
	v.drawLegend(v.chartCard)
}

func (v *historyView) drawSummary() {
	card := v.layout.summary
	v.ctx.Panel(card, v.theme.Panel, v.theme.Border)

	fontKey := v.ctx.Font(11, win32.FW_NORMAL)
	fontValue := v.ctx.Font(14, win32.FW_SEMIBOLD)

	rows := v.stats()
	if len(rows) == 0 {
		return
	}
	inner := int(math.Round(14 * v.scale))
	cellW := (card.W - 2*inner) / 4
	rowH := int(math.Round(36 * v.scale))
	for i, row := range rows {
		col := i % 4
		line := i / 4
		x := card.X + inner + col*cellW
		y := card.Y + int(math.Round(12*v.scale)) + line*rowH
		v.ctx.Text(row[0], Rect{x, y, cellW - int(math.Round(8*v.scale)), int(math.Round(15 * v.scale))},
			fontKey, v.theme.Faint, win32.DT_LEFT)
		v.ctx.Text(row[1], Rect{x, y + int(math.Round(15*v.scale)), cellW - int(math.Round(8*v.scale)), int(math.Round(19 * v.scale))},
			fontValue, v.theme.Text, win32.DT_LEFT|win32.DT_END_ELLIPSIS)
	}
}

func (v *historyView) stats() [][2]string {
	if v.data == nil {
		return [][2]string{{i18n.Tr("history.samples"), "--"}}
	}
	d := v.data
	rows := [][2]string{
		{i18n.Tr("history.samples"), fmt.Sprintf("%d", d.Count)},
	}
	if d.Count == 0 {
		return rows
	}
	rows = append(rows,
		[2]string{i18n.Tr("history.start"), d.Start.Format("2006-01-02 15:04:05")},
		[2]string{i18n.Tr("history.end"), d.End.Format("2006-01-02 15:04:05")},
		[2]string{i18n.Tr("history.duration"), formatDuration(d.Duration)},
	)
	if d.CPUCount > 0 {
		rows = append(rows,
			[2]string{i18n.Tr("history.cpu_mean"), fmt.Sprintf("%.1f%%", d.CPUMean)},
			[2]string{i18n.Tr("history.cpu_max"), fmt.Sprintf("%.1f%%", d.CPUMax)},
			[2]string{i18n.Tr("history.cpu_min"), fmt.Sprintf("%.1f%%", d.CPUMin)},
		)
	} else {
		rows = append(rows, [2]string{"CPU", i18n.Tr("history.no_data")})
	}
	if d.MemCount > 0 {
		rows = append(rows,
			[2]string{i18n.Tr("history.mem_mean"), fmt.Sprintf("%.1f%%", d.MemMean)},
			[2]string{i18n.Tr("history.mem_max"), fmt.Sprintf("%.1f%%", d.MemMax)},
			[2]string{i18n.Tr("history.mem_min"), fmt.Sprintf("%.1f%%", d.MemMin)},
			[2]string{i18n.Tr("history.mem_mean_gb"), fmt.Sprintf("%.2f / %.2f GB", d.MemUsedMean/history.BytesPerGB, d.MemTotalGB)},
		)
	} else {
		rows = append(rows, [2]string{i18n.Tr("chart.mem"), i18n.Tr("history.no_data")})
	}
	return rows
}

func formatDuration(d time.Duration) string {
	seconds := int(math.Round(d.Seconds()))
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60
	if hours > 0 {
		return i18n.Tr("duration.hms", "h", fmt.Sprintf("%d", hours), "m", fmt.Sprintf("%d", minutes), "s", fmt.Sprintf("%d", secs))
	}
	if minutes > 0 {
		return i18n.Tr("duration.ms", "m", fmt.Sprintf("%d", minutes), "s", fmt.Sprintf("%d", secs))
	}
	return i18n.Tr("duration.s", "s", fmt.Sprintf("%d", secs))
}

func (v *historyView) drawLegend(card Rect) {
	cx := card.Right() - int(math.Round(16*v.scale))
	font := v.ctx.Font(11.5, win32.FW_NORMAL)
	draw := func(text string, col color.RGBA) {
		labelW := v.ctx.Measure(text, font)
		cx -= labelW
		v.ctx.Text(text, Rect{cx, card.Y + int(math.Round(10*v.scale)), labelW, int(math.Round(20 * v.scale))},
			font, v.theme.Muted, win32.DT_LEFT)
		lineW := int(math.Round(14 * v.scale))
		cx -= lineW + int(math.Round(6*v.scale))
		y := card.Y + int(math.Round(20*v.scale))
		v.cov.Segment(float64(cx), float64(y), float64(cx+lineW), float64(y), 2.2*v.scale)
		v.cov.Composite(v.surf, col)
	}
	draw(i18n.Tr("chart.mem"), v.theme.Mem)
	cx -= int(math.Round(18 * v.scale))
	draw("CPU", v.theme.CPU)
}

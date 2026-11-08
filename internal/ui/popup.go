package ui

import (
	"sync"

	"github.com/qepan0520/SysGauge-Go/internal/i18n"
	"github.com/qepan0520/SysGauge-Go/internal/win32"
)

type popupWindow struct {
	hwnd     uintptr
	surf     *win32.Surface
	cov      *win32.Paint
	ctx      *Ctx
	theme    *Theme
	items    []string
	selected int
	hover    int
	itemH    int
	onPick   func(int)
	width    int
	height   int
}

var (
	popupClassOnce sync.Once
	popupClassName string
	popupByHWND    sync.Map
)

// ShowPopup opens a borderless list at the screen position of the anchor.
func ShowPopup(owner uintptr, anchor Rect, items []string, selected int, theme *Theme, scale float64, onPick func(int)) {
	popupClassOnce.Do(func() {
		name, err := win32.RegisterClass("SysGaugePopup", popupProc)
		if err != nil {
			return
		}
		popupClassName = name
	})
	if popupClassName == "" || len(items) == 0 {
		return
	}

	itemH := int(30 * scale)
	pad := int(5 * scale)
	width := anchor.W
	if width < int(120*scale) {
		width = int(120 * scale)
	}
	height := len(items)*itemH + 2*pad

	origin := win32.ClientToScreen(owner, win32.Point{X: int32(anchor.X), Y: int32(anchor.Bottom() + int(2*scale))})
	screenW := win32.ScreenMetrics(0)
	screenH := win32.ScreenMetrics(1)
	x := int(origin.X)
	y := int(origin.Y)
	if x+width > screenW {
		x = screenW - width - int(4*scale)
	}
	if y+height > screenH {
		y = int(origin.Y) - height - anchor.H - int(4*scale)
	}

	hwnd, err := win32.CreateWindow(popupClassName, "", win32.WS_EX_TOOLWINDOW, win32.WS_POPUP,
		int32(x), int32(y), int32(width), int32(height), owner)
	if err != nil {
		return
	}

	p := &popupWindow{
		hwnd:     hwnd,
		surf:     win32.NewSurface(),
		cov:      &win32.Paint{},
		theme:    theme,
		items:    items,
		selected: selected,
		hover:    selected,
		itemH:    itemH,
		onPick:   onPick,
		width:    width,
		height:   height,
	}
	p.ctx = &Ctx{S: p.surf, P: p.cov, T: theme, Scale: scale}
	popupByHWND.Store(hwnd, p)
	win32.ShowWindow(hwnd, win32.SW_SHOW)
	win32.SetFocus(hwnd)
	win32.SetCapture(hwnd)
	win32.Invalidate(hwnd)
}

func popupProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	value, ok := popupByHWND.Load(hwnd)
	if !ok {
		return win32.DefWindowProc(hwnd, msg, wparam, lparam)
	}
	p := value.(*popupWindow)

	switch msg {
	case win32.WM_PAINT:
		dc, ps := win32.BeginPaint(hwnd)
		w, h := win32.ClientRect(hwnd)
		if p.surf.Resize(w, h) {
			p.cov.Ensure(w, h)
			p.paint()
			p.surf.Blit(dc)
		}
		win32.EndPaint(hwnd, ps)
		return 0
	case win32.WM_ERASEBKGND:
		return 1
	case win32.WM_MOUSEMOVE:
		x, y := mousePoint(lparam)
		index := p.indexAt(y)
		if index != p.hover {
			p.hover = index
			win32.Invalidate(hwnd)
		}
		_ = x
		win32.TrackLeave(hwnd)
		return 0
	case win32.WM_LBUTTONDOWN:
		x, y := mousePoint(lparam)
		index := p.indexAt(y)
		inside := x >= 0 && x < p.width && y >= 0 && y < p.height
		p.close()
		if inside && index >= 0 {
			if p.onPick != nil {
				p.onPick(index)
			}
		}
		return 0
	case win32.WM_KEYDOWN:
		switch int(wparam) {
		case win32.VK_ESCAPE:
			p.close()
		case win32.VK_UP:
			if p.hover > 0 {
				p.hover--
				win32.Invalidate(hwnd)
			}
		case win32.VK_DOWN:
			if p.hover < len(p.items)-1 {
				p.hover++
				win32.Invalidate(hwnd)
			}
		case win32.VK_RETURN:
			index := p.hover
			p.close()
			if index >= 0 && p.onPick != nil {
				p.onPick(index)
			}
		}
		return 0
	case win32.WM_KILLFOCUS, win32.WM_CAPTURECHANGED:
		p.close()
		return 0
	}
	return win32.DefWindowProc(hwnd, msg, wparam, lparam)
}

func (p *popupWindow) indexAt(y int) int {
	pad := p.height - len(p.items)*p.itemH
	pad /= 2
	index := (y - pad) / p.itemH
	if index < 0 || index >= len(p.items) {
		return -1
	}
	return index
}

func (p *popupWindow) paint() {
	p.surf.Fill(p.theme.Panel)
	p.ctx.Panel(Rect{0, 0, p.width, p.height}, p.theme.Panel, p.theme.Border)

	pad := p.height - len(p.items)*p.itemH
	pad /= 2
	font := p.ctx.Font(13, win32.FW_NORMAL)
	for i, item := range p.items {
		row := Rect{int(4 * p.ctx.Scale), pad + i*p.itemH, p.width - int(8*p.ctx.Scale), p.itemH}
		if i == p.hover {
			p.ctx.FillRoundRect(row, 4, p.theme.ButtonHover)
		}
		textColor := p.theme.Text
		if i == p.selected {
			textColor = p.theme.Accent
		}
		p.ctx.Text(item, Rect{row.X + int(9*p.ctx.Scale), row.Y, row.W - int(34*p.ctx.Scale), row.H},
			font, textColor, win32.DT_LEFT)

		if i == p.selected {
			cx := float64(row.Right()) - float64(int(13*p.ctx.Scale))
			cy := float64(row.Y) + float64(row.H)/2
			points := []win32.PointF{
				{X: cx - 4*p.ctx.Scale, Y: cy},
				{X: cx - 1*p.ctx.Scale, Y: cy + 3*p.ctx.Scale},
				{X: cx + 5*p.ctx.Scale, Y: cy - 4*p.ctx.Scale},
			}
			p.cov.Polyline(points, 2*p.ctx.Scale)
			p.cov.Composite(p.surf, p.theme.Accent)
		}
	}
}

func (p *popupWindow) close() {
	if p.hwnd == 0 {
		return
	}
	win32.ReleaseCapture()
	popupByHWND.Delete(p.hwnd)
	win32.DestroyWindow(p.hwnd)
	p.surf.Free()
	p.hwnd = 0
}

// openLanguageMenu shows the language picker below the dropdown.
func (d *Dash) openLanguageMenu() {
	c := d.findControl(idLanguage)
	if c == nil {
		return
	}
	items := make([]string, len(i18n.Languages))
	selected := 0
	current := i18n.Current()
	for i, lang := range i18n.Languages {
		items[i] = lang.Name
		if lang.Code == current {
			selected = i
		}
	}
	ShowPopup(d.hwnd, c.rect, items, selected, d.theme, d.scale, func(index int) {
		if index < 0 || index >= len(i18n.Languages) {
			return
		}
		code := i18n.Languages[index].Code
		i18n.SetLanguage(code)
		d.cfg.Language = code
		_ = d.cfg.Save()
		d.Invalidate()
		if d.hooks.LanguageChanged != nil {
			d.hooks.LanguageChanged()
		}
	})
}

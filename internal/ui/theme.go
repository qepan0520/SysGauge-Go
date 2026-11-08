package ui

import (
	"image/color"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"

	"github.com/qepan0520/SysGauge-Go/internal/win32"
)

// Theme is the colour set used by every custom-drawn widget.
type Theme struct {
	Dark bool

	Window     color.RGBA
	Panel      color.RGBA
	PanelSoft  color.RGBA
	Border     color.RGBA
	BorderSoft color.RGBA

	Text  color.RGBA
	Muted color.RGBA
	Faint color.RGBA

	Accent     color.RGBA
	AccentText color.RGBA

	Button        color.RGBA
	ButtonHover   color.RGBA
	ButtonPressed color.RGBA
	ButtonBorder  color.RGBA

	Input       color.RGBA
	InputBorder color.RGBA

	CPU color.RGBA
	Mem color.RGBA

	Good color.RGBA
	Warn color.RGBA
	Bad  color.RGBA

	Grid        color.RGBA
	Axis        color.RGBA
	Tooltip     color.RGBA
	TooltipText color.RGBA
	Shadow      color.RGBA
}

// DarkTheme returns the dark palette.
func DarkTheme() *Theme {
	return &Theme{
		Dark:          true,
		Window:        hex("#17191C"),
		Panel:         hex("#202327"),
		PanelSoft:     hex("#1B1E22"),
		Border:        hex("#343A41"),
		BorderSoft:    hex("#2A2F35"),
		Text:          hex("#E9EDF1"),
		Muted:         hex("#B9C1CB"),
		Faint:         hex("#8D97A2"),
		Accent:        hex("#4C8DFF"),
		AccentText:    hex("#FFFFFF"),
		Button:        hex("#2A2E34"),
		ButtonHover:   hex("#343A41"),
		ButtonPressed: hex("#3D444C"),
		ButtonBorder:  hex("#3D444C"),
		Input:         hex("#15181B"),
		InputBorder:   hex("#3C434B"),
		CPU:           hex("#6FB0FF"),
		Mem:           hex("#FFB870"),
		Good:          hex("#34D399"),
		Warn:          hex("#FBBF24"),
		Bad:           hex("#F87171"),
		Grid:          hex("#FFFFFF"),
		Axis:          hex("#9AA4AF"),
		Tooltip:       hex("#2E343B"),
		TooltipText:   hex("#F5F7FA"),
		Shadow:        hex("#000000"),
	}
}

// LightTheme returns the light palette.
func LightTheme() *Theme {
	return &Theme{
		Dark:          false,
		Window:        hex("#F1F3F6"),
		Panel:         hex("#FFFFFF"),
		PanelSoft:     hex("#F8FAFC"),
		Border:        hex("#D7DDE5"),
		BorderSoft:    hex("#E7EBF0"),
		Text:          hex("#111827"),
		Muted:         hex("#475467"),
		Faint:         hex("#667085"),
		Accent:        hex("#2563EB"),
		AccentText:    hex("#FFFFFF"),
		Button:        hex("#FFFFFF"),
		ButtonHover:   hex("#EEF2F7"),
		ButtonPressed: hex("#E2E8F0"),
		ButtonBorder:  hex("#D2D9E2"),
		Input:         hex("#FFFFFF"),
		InputBorder:   hex("#C7D0DA"),
		CPU:           hex("#2563EB"),
		Mem:           hex("#B45309"),
		Good:          hex("#15803D"),
		Warn:          hex("#B45309"),
		Bad:           hex("#B91C1C"),
		Grid:          hex("#0F172A"),
		Axis:          hex("#475467"),
		Tooltip:       hex("#1F2937"),
		TooltipText:   hex("#F9FAFB"),
		Shadow:        hex("#0F172A"),
	}
}

// LoadTheme follows the Windows "app mode" preference.
func LoadTheme() *Theme {
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err == nil {
		defer key.Close()
		if value, _, err := key.GetIntegerValue("AppsUseLightTheme"); err == nil {
			if value == 0 {
				return DarkTheme()
			}
			return LightTheme()
		}
	}
	return LightTheme()
}

// alpha returns the colour with a different alpha channel.
func alpha(c color.RGBA, a uint8) color.RGBA {
	c.A = a
	return c
}

// mix blends two colours; t=0 returns a, t=1 returns b.
func mix(a, b color.RGBA, t float64) color.RGBA {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return color.RGBA{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
		A: 255,
	}
}

func hex(value string) color.RGBA {
	value = strings.TrimPrefix(value, "#")
	if len(value) == 8 {
		value = value[:6]
	}
	if len(value) != 6 {
		return color.RGBA{A: 255}
	}
	num, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return color.RGBA{A: 255}
	}
	return color.RGBA{
		R: uint8(num >> 16),
		G: uint8(num >> 8),
		B: uint8(num),
		A: 255,
	}
}

// Rect is a device-pixel rectangle.
type Rect struct {
	X, Y, W, H int
}

// Contains reports whether a point lies inside the rectangle.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Right returns the exclusive right edge.
func (r Rect) Right() int { return r.X + r.W }

// Bottom returns the exclusive bottom edge.
func (r Rect) Bottom() int { return r.Y + r.H }

// Inset shrinks the rectangle on every side.
func (r Rect) Inset(d int) Rect {
	return Rect{X: r.X + d, Y: r.Y + d, W: r.W - 2*d, H: r.H - 2*d}
}

// Scale returns a scaled copy of the rectangle.
func (r Rect) Scale(s float64) Rect {
	return Rect{X: int(float64(r.X) * s), Y: int(float64(r.Y) * s),
		W: int(float64(r.W) * s), H: int(float64(r.H) * s)}
}

// Ctx bundles everything a paint routine needs.
type Ctx struct {
	S     *win32.Surface
	P     *win32.Paint
	T     *Theme
	Scale float64
}

// Font returns a scaled font.
func (c *Ctx) Font(size float64, weight int) *win32.Font {
	h := int(size*c.Scale + 0.5)
	if h < 8 {
		h = 8
	}
	return win32.FontSize(h, weight)
}

// Text width in device pixels.
func (c *Ctx) Measure(text string, font *win32.Font) int {
	return c.S.Measure(text, font)
}

// Text draws a single line with the requested alignment.
func (c *Ctx) Text(text string, r Rect, font *win32.Font, col color.RGBA, align uint32) {
	c.S.Text(text, r.X, r.Y, r.W, r.H, font, col, align)
}

// Panel draws a rounded card with a 1px border.
func (c *Ctx) Panel(r Rect, fill, border color.RGBA) {
	radius := 8 * c.Scale
	if fill.A != 0 {
		c.P.FillRoundRect(float64(r.X), float64(r.Y), float64(r.W), float64(r.H), radius)
		c.P.Composite(c.S, fill)
	}
	if border.A != 0 {
		c.P.StrokeRoundRect(float64(r.X)+0.5, float64(r.Y)+0.5, float64(r.W)-1, float64(r.H)-1, radius, c.Scale)
		c.P.Composite(c.S, border)
	}
}

// FillRoundRect fills a rounded rectangle without a border.
func (c *Ctx) FillRoundRect(r Rect, radius float64, col color.RGBA) {
	c.P.FillRoundRect(float64(r.X), float64(r.Y), float64(r.W), float64(r.H), radius*c.Scale)
	c.P.Composite(c.S, col)
}

// StrokeRoundRect draws a rounded outline.
func (c *Ctx) StrokeRoundRect(r Rect, radius, width float64, col color.RGBA) {
	c.P.StrokeRoundRect(float64(r.X)+0.5, float64(r.Y)+0.5, float64(r.W)-1, float64(r.H)-1, radius*c.Scale, width*c.Scale)
	c.P.Composite(c.S, col)
}

// Divider draws a 1px horizontal separator.
func (c *Ctx) Divider(x, y, w int, col color.RGBA) {
	c.S.Line(x, y, x+w, y, col)
}

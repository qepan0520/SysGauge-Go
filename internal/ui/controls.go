package ui

import (
	"image/color"

	"github.com/qepan0520/SysGauge-Go/internal/win32"
)

// ButtonStyle selects the visual weight of a button.
type ButtonStyle int

const (
	// ButtonSecondary is the default neutral button.
	ButtonSecondary ButtonStyle = iota
	// ButtonPrimary is the accent-coloured call to action.
	ButtonPrimary
	// ButtonDanger is used for destructive actions.
	ButtonDanger
	// ButtonGhost has no fill until hovered.
	ButtonGhost
)

// Button paints a push button.
func (c *Ctx) Button(r Rect, label string, style ButtonStyle, hovered, pressed, disabled bool) {
	fill := c.T.Button
	border := c.T.ButtonBorder
	textColor := c.T.Text

	switch style {
	case ButtonPrimary:
		fill = c.T.Accent
		border = c.T.Accent
		textColor = c.T.AccentText
	case ButtonDanger:
		fill = mix(c.T.Panel, c.T.Bad, 0.10)
		border = mix(c.T.Panel, c.T.Bad, 0.35)
		textColor = c.T.Bad
	case ButtonGhost:
		fill = color.RGBA{A: 0}
		border = color.RGBA{A: 0}
		textColor = c.T.Muted
	}

	if disabled {
		fill = mix(c.T.Panel, c.T.Button, 0.5)
		border = c.T.BorderSoft
		textColor = c.T.Faint
	} else if pressed {
		if style == ButtonPrimary {
			fill = mix(c.T.Accent, color.RGBA{R: 0, G: 0, B: 0, A: 255}, 0.16)
		} else {
			fill = c.T.ButtonPressed
		}
	} else if hovered {
		switch style {
		case ButtonPrimary:
			fill = mix(c.T.Accent, color.RGBA{R: 255, G: 255, B: 255, A: 255}, 0.12)
		case ButtonDanger:
			fill = mix(c.T.Panel, c.T.Bad, 0.18)
		case ButtonGhost:
			fill = c.T.ButtonHover
		default:
			fill = c.T.ButtonHover
		}
	}

	c.Panel(r, fill, border)
	font := c.Font(13, win32.FW_NORMAL)
	c.Text(label, r, font, textColor, win32.DT_CENTER)
}

// Checkbox paints a checkbox with a label and returns the box rectangle.
func (c *Ctx) Checkbox(r Rect, label string, checked, hovered bool) Rect {
	box := int(16 * c.Scale)
	if box < 12 {
		box = 12
	}
	boxRect := Rect{X: r.X, Y: r.Y + (r.H-box)/2, W: box, H: box}
	radius := 4.0

	border := c.T.InputBorder
	if hovered {
		border = c.T.Accent
	}
	fill := c.T.Input
	if checked {
		fill = c.T.Accent
		border = c.T.Accent
	}
	c.FillRoundRect(boxRect, radius, fill)
	c.StrokeRoundRect(boxRect, radius, 1, border)

	if checked {
		cx := float64(boxRect.X) + float64(boxRect.W)*0.24
		cy := float64(boxRect.Y) + float64(boxRect.H)*0.52
		points := []win32.PointF{
			{X: cx, Y: cy},
			{X: cx + float64(boxRect.W)*0.20, Y: cy + float64(boxRect.H)*0.22},
			{X: cx + float64(boxRect.W)*0.53, Y: cy - float64(boxRect.H)*0.20},
		}
		c.P.Polyline(points, 2*c.Scale)
		c.P.Composite(c.S, c.T.AccentText)
	}

	textColor := c.T.Text
	if !hovered {
		textColor = c.T.Text
	}
	font := c.Font(13, win32.FW_NORMAL)
	gap := int(7 * c.Scale)
	labelRect := Rect{X: boxRect.Right() + gap, Y: r.Y, W: r.W - boxRect.W - gap, H: r.H}
	c.Text(label, labelRect, font, textColor, win32.DT_LEFT)
	return Rect{X: r.X, Y: r.Y, W: boxRect.W + gap + c.Measure(label, font), H: r.H}
}

// Segmented paints a segmented control and returns the segment rectangles.
func (c *Ctx) Segmented(r Rect, labels []string, selected, hover int) []Rect {
	radius := 6.0
	c.FillRoundRect(r, radius, c.T.Input)
	c.StrokeRoundRect(r, radius, 1, c.T.InputBorder)

	font := c.Font(12.5, win32.FW_NORMAL)
	rects := make([]Rect, len(labels))
	width := r.W / len(labels)
	for i, label := range labels {
		seg := Rect{X: r.X + i*width, Y: r.Y, W: width, H: r.H}
		if i == len(labels)-1 {
			seg.W = r.Right() - seg.X
		}
		rects[i] = seg
		if i == selected {
			pill := Rect{X: seg.X + 2, Y: seg.Y + 2, W: seg.W - 4, H: seg.H - 4}
			c.FillRoundRect(pill, 4.5, c.T.Accent)
			c.Text(label, seg, font, c.T.AccentText, win32.DT_CENTER)
			continue
		}
		if i == hover {
			pill := Rect{X: seg.X + 2, Y: seg.Y + 2, W: seg.W - 4, H: seg.H - 4}
			c.FillRoundRect(pill, 4.5, c.T.ButtonHover)
		}
		if i > 0 {
			c.S.Line(seg.X, seg.Y+4, seg.X, seg.Bottom()-4, c.T.BorderSoft)
		}
		c.Text(label, seg, font, c.T.Muted, win32.DT_CENTER)
	}
	return rects
}

// Dropdown paints a combo-like button with a chevron.
func (c *Ctx) Dropdown(r Rect, label string, hovered, open bool) {
	fill := c.T.Input
	border := c.T.InputBorder
	if hovered || open {
		border = c.T.Accent
	}
	if open {
		fill = mix(c.T.Input, c.T.Accent, 0.10)
	}
	c.FillRoundRect(r, 6, fill)
	c.StrokeRoundRect(r, 6, 1, border)

	font := c.Font(13, win32.FW_NORMAL)
	pad := int(10 * c.Scale)
	arrowRoom := int(22 * c.Scale)
	textRect := Rect{X: r.X + pad, Y: r.Y, W: r.W - pad - arrowRoom, H: r.H}
	c.Text(label, textRect, font, c.T.Text, win32.DT_LEFT)

	drawChevron(c, r.Right()-int(13*c.Scale), r.Y+r.H/2, 4.2*c.Scale, c.T.Muted, open)
}

// Field paints an input box, optionally with a caret, a suffix and reserved
// space on the right (for a stepper).
func (c *Ctx) Field(r Rect, text, suffix string, hovered, editing bool, caret, reserveRight int, muted bool) {
	fill := c.T.Input
	border := c.T.InputBorder
	if editing {
		border = c.T.Accent
	} else if hovered {
		border = mix(c.T.InputBorder, c.T.Accent, 0.55)
	}
	c.FillRoundRect(r, 5, fill)
	c.StrokeRoundRect(r, 5, 1, border)

	font := c.Font(13, win32.FW_NORMAL)
	pad := int(9 * c.Scale)
	textColor := c.T.Text
	if muted {
		textColor = c.T.Muted
	}
	contentRight := r.Right() - pad - reserveRight
	suffixWidth := 0
	if suffix != "" {
		suffixWidth = c.Measure(suffix, c.Font(12, win32.FW_NORMAL))
		c.Text(suffix, Rect{X: r.X + pad, Y: r.Y, W: contentRight - (r.X + pad), H: r.H},
			c.Font(12, win32.FW_NORMAL), c.T.Faint, win32.DT_RIGHT)
	}
	textRect := Rect{X: r.X + pad, Y: r.Y, W: contentRight - (r.X + pad) - suffixWidth, H: r.H}
	c.Text(text, textRect, font, textColor, win32.DT_LEFT)

	if editing {
		caretX := r.X + pad + c.Measure(text[:caretByte(text, caret)], font) + 1
		c.S.Line(caretX, r.Y+int(6*c.Scale), caretX, r.Bottom()-int(6*c.Scale), c.T.Text)
	}
}

func caretByte(text string, runeIndex int) int {
	if runeIndex <= 0 {
		return 0
	}
	count := 0
	for i := range text {
		if count == runeIndex {
			return i
		}
		count++
	}
	return len(text)
}

// Stepper draws the up/down chevrons of a numeric field and returns both rects.
func (c *Ctx) Stepper(r Rect, upHover, downHover bool) (up, down Rect) {
	w := int(18 * c.Scale)
	half := r.H / 2
	up = Rect{X: r.Right() - w, Y: r.Y, W: w, H: half}
	down = Rect{X: r.Right() - w, Y: r.Y + half, W: w, H: r.H - half}
	col := c.T.Muted
	if upHover {
		col = c.T.Text
	}
	drawChevron(c, up.X+w/2, up.Y+up.H/2+int(c.Scale), 3.2*c.Scale, col, true)
	if downHover {
		col = c.T.Text
	} else {
		col = c.T.Muted
	}
	drawChevron(c, down.X+w/2, down.Y+down.H/2-int(c.Scale), 3.2*c.Scale, col, false)
	return up, down
}

// drawChevron draws a small v-shaped arrow.
func drawChevron(c *Ctx, cx, cy int, size float64, col color.RGBA, up bool) {
	half := size
	dir := 1.0
	if up {
		dir = -1
	}
	points := []win32.PointF{
		{X: float64(cx) - half, Y: float64(cy) - dir*half/2},
		{X: float64(cx), Y: float64(cy) + dir*half/2},
		{X: float64(cx) + half, Y: float64(cy) - dir*half/2},
	}
	c.P.Polyline(points, 1.6*c.Scale)
	c.P.Composite(c.S, col)
}

// Badge draws a small rounded tag.
func (c *Ctx) Badge(r Rect, text string, fill, textColor color.RGBA) {
	c.FillRoundRect(r, 4, fill)
	c.Text(text, r, c.Font(11, win32.FW_NORMAL), textColor, win32.DT_CENTER)
}

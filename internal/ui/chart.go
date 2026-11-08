package ui

import (
	"fmt"
	"image/color"
	"math"
	"sort"
	"time"

	"github.com/qepan0520/SysGauge-Go/internal/i18n"
	"github.com/qepan0520/SysGauge-Go/internal/win32"
)

// MaxPoints caps the live buffer; history files are loaded in full.
const MaxPoints = 200_000

// DefaultWindowSec is the initial live chart window.
const DefaultWindowSec = 300.0

// Chart holds the samples shown by the plot and its view state.
type Chart struct {
	X    []float64
	CPU  []float64
	Mem  []float64
	Wall []float64

	DateAxis  bool
	WindowSec float64
	Follow    bool
	AutoScale bool

	ViewMin float64
	ViewMax float64

	// base is the wall-clock time of the first live point.
	base float64

	Hovering bool
	HoverX   float64
	HoverIdx int

	yMin, yMax      float64
	lowVal, highVal float64
	showLowHigh     bool

	cacheKey cacheKey
	cpuCache [][]win32.PointF
	memCache [][]win32.PointF
	keyValid bool
}

type cacheKey struct {
	lo, hi     float64
	width      int
	height     int
	points     int
	yMin, yMax float64
}

// NewChart creates a chart. dateAxis switches the X axis to wall-clock time.
func NewChart(dateAxis bool) *Chart {
	return &Chart{
		DateAxis:  dateAxis,
		WindowSec: DefaultWindowSec,
		Follow:    !dateAxis,
		ViewMax:   DefaultWindowSec,
	}
}

// Empty reports whether the chart holds no points.
func (c *Chart) Empty() bool { return len(c.X) == 0 }

// Append adds one live sample. wall is a Unix timestamp in seconds.
func (c *Chart) Append(wall float64, cpu, mem *float64) {
	x := wall
	if !c.DateAxis {
		if len(c.Wall) == 0 {
			c.base = wall
			c.ViewMin = 0
			c.ViewMax = 0
		}
		x = wall - c.base
	}
	c.X = append(c.X, x)
	c.Wall = append(c.Wall, wall)
	c.CPU = append(c.CPU, valueOrNaN(cpu))
	c.Mem = append(c.Mem, valueOrNaN(mem))

	if len(c.X) > MaxPoints {
		drop := len(c.X) - MaxPoints
		c.X = append(c.X[:0], c.X[drop:]...)
		c.CPU = append(c.CPU[:0], c.CPU[drop:]...)
		c.Mem = append(c.Mem[:0], c.Mem[drop:]...)
		c.Wall = append(c.Wall[:0], c.Wall[drop:]...)
	}
	if c.Follow {
		c.ViewMax = x
		c.ViewMin = x - c.WindowSec
		if c.ViewMin < 0 {
			c.ViewMin = 0
		}
	}
	c.keyValid = false
}

// SetSeries replaces the whole series (history mode).
func (c *Chart) SetSeries(times, cpu, mem []float64) {
	c.X = times
	c.Wall = times
	c.CPU = cpu
	c.Mem = mem
	if len(times) > 0 {
		c.base = times[0]
		c.ViewMin = times[0]
		c.ViewMax = times[len(times)-1]
		if c.ViewMax-c.ViewMin < 1 {
			c.ViewMax = c.ViewMin + 1
		}
	}
	c.keyValid = false
}

// Clear drops all data.
func (c *Chart) Clear() {
	c.X = c.X[:0]
	c.CPU = c.CPU[:0]
	c.Mem = c.Mem[:0]
	c.Wall = c.Wall[:0]
	c.base = 0
	c.ViewMin = -c.WindowSec
	c.ViewMax = 0
	c.Hovering = false
	c.keyValid = false
}

// SetWindow changes how much live history is visible.
func (c *Chart) SetWindow(seconds float64) {
	if seconds < 5 {
		seconds = 5
	}
	c.WindowSec = seconds
	if c.Follow && len(c.X) > 0 {
		c.ViewMax = c.X[len(c.X)-1]
		c.ViewMin = c.ViewMax - seconds
	}
	c.keyValid = false
}

// ResetView returns to the default range.
func (c *Chart) ResetView() {
	if len(c.X) == 0 {
		return
	}
	if c.DateAxis {
		c.ViewMin = c.X[0]
		c.ViewMax = c.X[len(c.X)-1]
		return
	}
	c.Follow = true
	c.ViewMax = c.X[len(c.X)-1]
	c.ViewMin = c.ViewMax - c.WindowSec
	c.keyValid = false
}

// SetAutoScale enables or disables adaptive Y scaling.
func (c *Chart) SetAutoScale(enabled bool) {
	c.AutoScale = enabled
	c.keyValid = false
}

// Zoom scales the visible X range around a pivot value.
func (c *Chart) Zoom(factor, pivot float64) {
	span := c.ViewMax - c.ViewMin
	if span <= 0 {
		return
	}
	newSpan := span * factor
	minSpan := 2.0
	if newSpan < minSpan {
		newSpan = minSpan
	}
	if len(c.X) > 1 {
		total := c.X[len(c.X)-1] - c.X[0]
		if maxSpan := total * 1.5; maxSpan > minSpan && newSpan > maxSpan {
			newSpan = maxSpan
		}
	}
	ratio := (pivot - c.ViewMin) / span
	c.ViewMin = pivot - ratio*newSpan
	c.ViewMax = c.ViewMin + newSpan
	c.clampView()
}

// PanPixels shifts the view by a number of device pixels.
func (c *Chart) PanPixels(dx, plotWidth int) {
	if plotWidth <= 0 {
		return
	}
	span := c.ViewMax - c.ViewMin
	delta := -float64(dx) / float64(plotWidth) * span
	c.ViewMin += delta
	c.ViewMax += delta
	c.clampView()
}

func (c *Chart) clampView() {
	if len(c.X) == 0 {
		return
	}
	first, last := c.X[0], c.X[len(c.X)-1]
	span := c.ViewMax - c.ViewMin
	slack := span * 0.1
	if c.ViewMin < first-slack {
		c.ViewMin = first - slack
		c.ViewMax = c.ViewMin + span
	}
	if c.ViewMax > last+slack {
		c.ViewMax = last + slack
		c.ViewMin = c.ViewMax - span
	}
	if !c.DateAxis {
		c.Follow = c.ViewMax >= last-slack*0.25
		if c.Follow {
			c.ViewMax = last
			c.ViewMin = last - c.WindowSec
			if c.ViewMin < 0 {
				c.ViewMin = 0
			}
		}
	}
	c.keyValid = false
}

// SetHover updates the hovered X position; returns whether a point was found.
func (c *Chart) SetHover(x float64) bool {
	if len(c.X) == 0 || x < c.X[0] || x > c.X[len(c.X)-1] {
		c.Hovering = false
		return false
	}
	index := sort.SearchFloat64s(c.X, x)
	if index <= 0 {
		index = 0
	} else if index >= len(c.X) {
		index = len(c.X) - 1
	} else if math.Abs(c.X[index-1]-x) <= math.Abs(c.X[index]-x) {
		index--
	}
	c.Hovering = true
	c.HoverIdx = index
	c.HoverX = c.X[index]
	return true
}

// XToValue converts a pixel offset inside the plot to a chart X value.
func (c *Chart) XToValue(px int, plot Rect) float64 {
	if plot.W <= 0 {
		return c.ViewMin
	}
	ratio := float64(px-plot.X) / float64(plot.W)
	return c.ViewMin + ratio*(c.ViewMax-c.ViewMin)
}

// PlotRect returns the drawable rectangle of the chart for a given card area.
func (c *Ctx) PlotRect(card Rect) Rect {
	left := c.Measure("100", c.Font(11, win32.FW_NORMAL)) + int(10*c.Scale)
	right := int(10 * c.Scale)
	top := int(4 * c.Scale)
	bottom := int(30 * c.Scale)
	return Rect{X: card.X + left, Y: card.Y + top,
		W: card.W - left - right, H: card.H - top - bottom}
}

// DrawChart renders grid, axes, curves, hover and auto-scale markers.
func (c *Ctx) DrawChart(ch *Chart, plot Rect) {
	if plot.W <= 4 || plot.H <= 4 {
		return
	}
	if !ch.keyValid || ch.cacheKey.width != plot.W || ch.cacheKey.height != plot.H ||
		ch.cacheKey.lo != ch.ViewMin || ch.cacheKey.hi != ch.ViewMax ||
		ch.cacheKey.points != len(ch.X) {
		ch.updateRange(plot)
		ch.cpuCache = buildPaths(ch.X, ch.CPU, ch.ViewMin, ch.ViewMax, plot, ch.yMin, ch.yMax)
		ch.memCache = buildPaths(ch.X, ch.Mem, ch.ViewMin, ch.ViewMax, plot, ch.yMin, ch.yMax)
		ch.cacheKey = cacheKey{lo: ch.ViewMin, hi: ch.ViewMax, width: plot.W,
			height: plot.H, points: len(ch.X), yMin: ch.yMin, yMax: ch.yMax}
		ch.keyValid = true
	}

	fontTick := c.Font(11, win32.FW_NORMAL)
	fontTitle := c.Font(11, win32.FW_NORMAL)

	// Y axis title and ticks.
	c.Text(i18n.Tr("chart.y_label")+" (%)", Rect{X: plot.X, Y: plot.Y - int(2*c.Scale), W: plot.W, H: int(14 * c.Scale)},
		fontTitle, c.T.Faint, win32.DT_LEFT)

	step := niceStep(ch.yMax-ch.yMin, plot.H/40)
	if step <= 0 {
		step = 10
	}
	gridColor := mix(c.T.Panel, c.T.Grid, 0.10)
	if !c.T.Dark {
		gridColor = mix(c.T.Panel, c.T.Grid, 0.09)
	}
	labelRoom := c.Measure("100", fontTick) + int(8*c.Scale)
	for value := math.Ceil(ch.yMin/step) * step; value <= ch.yMax+1e-9; value += step {
		y := ch.yOf(value, plot)
		c.S.Line(plot.X, y, plot.Right(), y, gridColor)
		label := trimFloat(value)
		c.Text(label, Rect{X: plot.X - labelRoom, Y: y - int(8*c.Scale), W: labelRoom - int(4*c.Scale), H: int(16 * c.Scale)},
			fontTick, c.T.Faint, win32.DT_RIGHT)
	}

	// X ticks and labels.
	xStep, isTime := c.xTickStep(ch, plot)
	if xStep > 0 {
		start := math.Ceil(ch.ViewMin/xStep) * xStep
		for value := start; value <= ch.ViewMax; value += xStep {
			x := ch.xOf(value, plot)
			c.S.Line(x, plot.Y, x, plot.Bottom(), gridColor)
			label := c.xTickLabel(value, xStep, isTime)
			w := c.Measure(label, fontTick) + int(12*c.Scale)
			c.Text(label, Rect{X: x - w/2, Y: plot.Bottom() + int(5*c.Scale), W: w, H: int(15 * c.Scale)},
				fontTick, c.T.Faint, win32.DT_CENTER)
		}
	}

	xTitle := i18n.Tr("chart.x_time")
	if !ch.DateAxis {
		xTitle = i18n.Tr("chart.x_elapsed") + " (s)"
	}
	c.Text(xTitle, Rect{X: plot.X, Y: plot.Bottom() + int(20*c.Scale), W: plot.W, H: int(14 * c.Scale)},
		fontTitle, c.T.Faint, win32.DT_CENTER)

	// Curves.
	c.drawPaths(ch.cpuCache, c.T.CPU)
	c.drawPaths(ch.memCache, c.T.Mem)

	if ch.showLowHigh {
		c.drawAutoScaleMarkers(ch, plot, fontTick)
	}

	if ch.Hovering && len(ch.X) > 0 {
		c.drawHover(ch, plot, fontTick)
	}
}

func (c *Ctx) drawPaths(paths [][]win32.PointF, col color.RGBA) {
	width := 2 * c.Scale
	if width < 1.6 {
		width = 1.6
	}
	for _, path := range paths {
		if len(path) < 2 {
			continue
		}
		c.P.Polyline(path, width)
	}
	c.P.Composite(c.S, col)
}

func (c *Ctx) drawAutoScaleMarkers(ch *Chart, plot Rect, font *win32.Font) {
	lowY := ch.yOf(ch.lowVal, plot)
	highY := ch.yOf(ch.highVal, plot)
	lowColor := color.RGBA{R: 0x02, G: 0x84, B: 0xC7, A: 255}
	highColor := color.RGBA{R: 0xEA, G: 0x58, B: 0x0C, A: 255}
	if c.T.Dark {
		lowColor = color.RGBA{R: 0x38, G: 0xBD, B: 0xF8, A: 255}
		highColor = color.RGBA{R: 0xFB, G: 0x92, B: 0x3C, A: 255}
	}
	c.dashedLine(plot.X, lowY, plot.Right(), lowY, lowColor)
	c.dashedLine(plot.X, highY, plot.Right(), highY, highColor)
	c.Text(i18n.Tr("chart.low", "value", fmt.Sprintf("%.1f", ch.lowVal)),
		Rect{X: plot.X + int(6*c.Scale), Y: lowY - int(15*c.Scale), W: int(110 * c.Scale), H: int(14 * c.Scale)},
		font, lowColor, win32.DT_LEFT)
	c.Text(i18n.Tr("chart.high", "value", fmt.Sprintf("%.1f", ch.highVal)),
		Rect{X: plot.X + int(6*c.Scale), Y: highY + int(2*c.Scale), W: int(110 * c.Scale), H: int(14 * c.Scale)},
		font, highColor, win32.DT_LEFT)
}

func (c *Ctx) drawHover(ch *Chart, plot Rect, font *win32.Font) {
	x := ch.xOf(ch.HoverX, plot)
	c.dashedLine(x, plot.Y, x, plot.Bottom(), mix(c.T.Axis, c.T.Panel, 0.35))

	wall := ch.Wall[ch.HoverIdx]
	timeText := time.UnixMilli(int64(wall * 1000)).Format("01-02 15:04:05")
	cpuText := "--"
	if v := ch.CPU[ch.HoverIdx]; !math.IsNaN(v) {
		cpuText = fmt.Sprintf("%.1f%%", v)
	}
	memText := "--"
	if v := ch.Mem[ch.HoverIdx]; !math.IsNaN(v) {
		memText = fmt.Sprintf("%.1f%%", v)
	}
	lines := []string{timeText, "CPU    " + cpuText, i18n.Tr("chart.mem") + "  " + memText}

	fontHeight := int(15 * c.Scale)
	padX := int(9 * c.Scale)
	padY := int(6 * c.Scale)
	maxWidth := 0
	for _, line := range lines {
		if w := c.Measure(line, font); w > maxWidth {
			maxWidth = w
		}
	}
	boxW := maxWidth + 2*padX
	boxH := len(lines)*fontHeight + 2*padY

	bx := x + int(14*c.Scale)
	if bx+boxW > plot.Right()-2 {
		bx = x - boxW - int(14*c.Scale)
	}
	if bx < plot.X+2 {
		bx = plot.X + 2
	}
	by := plot.Y + int(8*c.Scale)
	if by+boxH > plot.Bottom() {
		by = plot.Bottom() - boxH - 2
	}

	box := Rect{X: bx, Y: by, W: boxW, H: boxH}
	c.FillRoundRect(Rect{X: box.X + 2, Y: box.Y + 2, W: box.W, H: box.H}, 6, alpha(c.T.Shadow, 40))
	c.Panel(box, c.T.Tooltip, alpha(c.T.Text, 40))
	for i, line := range lines {
		col := c.T.TooltipText
		if i == 1 {
			col = c.T.CPU
		} else if i == 2 {
			col = c.T.Mem
		}
		c.Text(line, Rect{X: box.X + padX, Y: box.Y + padY + i*fontHeight, W: box.W - 2*padX, H: fontHeight},
			font, col, win32.DT_LEFT)
	}
}

func (c *Ctx) dashedLine(x0, y0, x1, y1 int, col color.RGBA) {
	if y0 == y1 {
		for x := x0; x < x1; x += 6 {
			end := x + 3
			if end > x1 {
				end = x1
			}
			c.S.Line(x, y0, end, y0, col)
		}
		return
	}
	if x0 == x1 {
		for y := y0; y < y1; y += 6 {
			end := y + 3
			if end > y1 {
				end = y1
			}
			c.S.Line(x0, y, x0, end, col)
		}
	}
}

func (ch *Chart) updateRange(plot Rect) {
	ch.showLowHigh = false
	if ch.AutoScale {
		low, high, ok := ch.visibleMinMax()
		if ok {
			pad := math.Max(1.0, (high-low)*0.08)
			bottom := math.Max(0, low-pad)
			top := math.Min(100, high+pad)
			if top-bottom < 1 {
				center := (top + bottom) / 2
				bottom = math.Max(0, center-0.5)
				top = math.Min(100, center+0.5)
			}
			if top <= bottom {
				top = math.Min(100, bottom+1)
			}
			ch.yMin, ch.yMax = bottom, top
			ch.lowVal, ch.highVal = low, high
			ch.showLowHigh = true
			return
		}
	}
	ch.yMin, ch.yMax = 0, 100
}

func (ch *Chart) visibleMinMax() (float64, float64, bool) {
	if len(ch.X) == 0 {
		return 0, 0, false
	}
	left := sort.SearchFloat64s(ch.X, ch.ViewMin)
	right := sort.SearchFloat64s(ch.X, ch.ViewMax)
	if right < len(ch.X) {
		right++
	}
	if right <= left {
		return 0, 0, false
	}
	low, high := math.Inf(1), math.Inf(-1)
	for _, series := range [][]float64{ch.CPU, ch.Mem} {
		for i := left; i < right; i++ {
			v := series[i]
			if math.IsNaN(v) {
				continue
			}
			low = math.Min(low, v)
			high = math.Max(high, v)
		}
	}
	if math.IsInf(low, 1) {
		return 0, 0, false
	}
	return low, high, true
}

func (ch *Chart) xOf(value float64, plot Rect) int {
	span := ch.ViewMax - ch.ViewMin
	if span <= 0 {
		return plot.X
	}
	ratio := (value - ch.ViewMin) / span
	return plot.X + int(ratio*float64(plot.W))
}

func (ch *Chart) yOf(value float64, plot Rect) int {
	span := ch.yMax - ch.yMin
	if span <= 0 {
		return plot.Bottom()
	}
	ratio := (value - ch.yMin) / span
	return plot.Bottom() - int(ratio*float64(plot.H))
}

func buildPaths(x, values []float64, lo, hi float64, plot Rect, yMin, yMax float64) [][]win32.PointF {
	if len(x) == 0 || plot.W <= 1 {
		return nil
	}
	left := sort.SearchFloat64s(x, lo)
	if left > 0 {
		left--
	}
	right := sort.SearchFloat64s(x, hi)
	if right < len(x) {
		right++
	}
	if right <= left {
		return nil
	}

	xOf := func(v float64) float64 {
		span := hi - lo
		if span <= 0 {
			return float64(plot.X)
		}
		return float64(plot.X) + (v-lo)/span*float64(plot.W)
	}
	yOf := func(v float64) float64 {
		span := yMax - yMin
		if span <= 0 {
			return float64(plot.Bottom())
		}
		return float64(plot.Bottom()) - (v-yMin)/span*float64(plot.H)
	}

	count := right - left
	if count <= plot.W*2 {
		var paths [][]win32.PointF
		current := make([]win32.PointF, 0, count)
		for i := left; i < right; i++ {
			v := values[i]
			if math.IsNaN(v) {
				if len(current) > 1 {
					paths = append(paths, current)
				}
				current = current[:0]
				continue
			}
			current = append(current, win32.PointF{X: xOf(x[i]), Y: yOf(v)})
		}
		if len(current) > 1 {
			paths = append(paths, current)
		}
		return paths
	}

	// Peak decimation: one min/max envelope per pixel column.
	bucketWidth := (hi - lo) / float64(plot.W)
	if bucketWidth <= 0 {
		return nil
	}
	var paths [][]win32.PointF
	current := make([]win32.PointF, 0, plot.W*2)
	column := -1
	var minV, maxV, firstV, lastV float64
	valid := false

	flush := func() {
		if !valid {
			if len(current) > 1 {
				paths = append(paths, current)
			}
			current = make([]win32.PointF, 0, plot.W*2)
			return
		}
		px := xOf(lo + (float64(column)+0.5)*bucketWidth)
		current = append(current, win32.PointF{X: px, Y: yOf(firstV)})
		if minV != firstV && minV != lastV {
			current = append(current, win32.PointF{X: px, Y: yOf(minV)})
		}
		if maxV != firstV && maxV != lastV {
			current = append(current, win32.PointF{X: px, Y: yOf(maxV)})
		}
		current = append(current, win32.PointF{X: px, Y: yOf(lastV)})
	}

	for i := left; i < right; i++ {
		col := int((x[i] - lo) / bucketWidth)
		if col >= plot.W {
			col = plot.W - 1
		}
		if col != column {
			flush()
			column = col
			valid = false
		}
		v := values[i]
		if math.IsNaN(v) {
			if valid {
				flush()
				valid = false
			}
			continue
		}
		if !valid {
			minV, maxV, firstV = v, v, v
			valid = true
		} else {
			minV = math.Min(minV, v)
			maxV = math.Max(maxV, v)
		}
		lastV = v
	}
	flush()
	if len(current) > 1 {
		paths = append(paths, current)
	}
	return paths
}

func valueOrNaN(v *float64) float64 {
	if v == nil {
		return math.NaN()
	}
	return *v
}

func niceStep(span float64, target int) float64 {
	if span <= 0 || target <= 0 {
		return 0
	}
	raw := span / float64(target)
	candidates := []float64{1, 2, 5, 10, 20, 25, 50, 100, 200, 250, 500, 1000}
	for _, c := range candidates {
		if c >= raw {
			return c
		}
	}
	return candidates[len(candidates)-1]
}

func (c *Ctx) xTickStep(ch *Chart, plot Rect) (float64, bool) {
	span := ch.ViewMax - ch.ViewMin
	if span <= 0 {
		return 0, false
	}
	target := plot.W / int(95*c.Scale)
	if target < 2 {
		target = 2
	}
	if ch.DateAxis {
		candidates := []float64{1, 5, 15, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 14400, 21600, 43200, 86400}
		raw := span / float64(target)
		for _, value := range candidates {
			if value >= raw {
				return value, true
			}
		}
		return candidates[len(candidates)-1], true
	}
	return niceStep(span, target), false
}

func (c *Ctx) xTickLabel(value, step float64, isTime bool) string {
	if isTime {
		stamp := time.UnixMilli(int64(value * 1000))
		if step >= 86400 {
			return stamp.Format("01-02")
		}
		if step >= 60 {
			return stamp.Format("15:04")
		}
		return stamp.Format("15:04:05")
	}
	return trimFloat(value)
}

func trimFloat(value float64) string {
	if math.Abs(value-math.Round(value)) < 0.01 {
		return fmt.Sprintf("%.0f", value)
	}
	return fmt.Sprintf("%.1f", value)
}

package win32

import (
	"image/color"
	"math"
)

// PointF is a device-space point.
type PointF struct {
	X, Y float64
}

// Paint rasterizes anti-aliased shapes into an 8-bit coverage buffer and
// composites the result onto a Surface. Reusing one buffer keeps allocations
// out of the paint path.
type Paint struct {
	w, h       int
	cov        []float32
	minX, minY int
	maxX, maxY int
	dirty      bool
}

func (p *Paint) ensure(w, h int) {
	if p.w == w && p.h == h && p.cov != nil {
		return
	}
	p.w, p.h = w, h
	p.cov = make([]float32, w*h)
	p.dirty = false
}

// Ensure makes sure the coverage buffer matches the surface size.
func (p *Paint) Ensure(w, h int) { p.ensure(w, h) }

func (p *Paint) reset() {
	if !p.dirty {
		return
	}
	for y := p.minY; y <= p.maxY; y++ {
		row := p.cov[y*p.w : (y+1)*p.w]
		for x := p.minX; x <= p.maxX; x++ {
			row[x] = 0
		}
	}
	p.dirty = false
}

func (p *Paint) mark(x0, y0, x1, y1 int) {
	if !p.dirty {
		p.minX, p.minY, p.maxX, p.maxY = x0, y0, x1, y1
		p.dirty = true
		return
	}
	if x0 < p.minX {
		p.minX = x0
	}
	if y0 < p.minY {
		p.minY = y0
	}
	if x1 > p.maxX {
		p.maxX = x1
	}
	if y1 > p.maxY {
		p.maxY = y1
	}
}

// Composite blends the covered pixels onto the surface.
func (p *Paint) Composite(s *Surface, c color.RGBA) {
	if !p.dirty {
		return
	}
	for y := p.minY; y <= p.maxY; y++ {
		row := p.cov[y*p.w : (y+1)*p.w]
		dst := y * s.W
		for x := p.minX; x <= p.maxX; x++ {
			if a := row[x]; a > 0 {
				s.Pix[dst+x] = blend(s.Pix[dst+x], c, float64(a))
			}
		}
	}
	p.reset()
}

// Segment adds an anti-aliased capsule between two points.
func (p *Paint) Segment(x0, y0, x1, y1, width float64) {
	if p.cov == nil {
		return
	}
	half := width / 2
	pad := half + 1
	minX := int(math.Floor(math.Min(x0, x1) - pad))
	maxX := int(math.Ceil(math.Max(x0, x1) + pad))
	minY := int(math.Floor(math.Min(y0, y1) - pad))
	maxY := int(math.Ceil(math.Max(y0, y1) + pad))
	if minX < 0 {
		minX = 0
	}
	if minY < 0 {
		minY = 0
	}
	if maxX >= p.w {
		maxX = p.w - 1
	}
	if maxY >= p.h {
		maxY = p.h - 1
	}
	if minX > maxX || minY > maxY {
		return
	}

	dx := x1 - x0
	dy := y1 - y0
	lengthSq := dx*dx + dy*dy

	for y := minY; y <= maxY; y++ {
		py := float64(y) + 0.5
		row := p.cov[y*p.w : (y+1)*p.w]
		for x := minX; x <= maxX; x++ {
			px := float64(x) + 0.5
			t := 0.0
			if lengthSq > 0 {
				t = ((px-x0)*dx + (py-y0)*dy) / lengthSq
				if t < 0 {
					t = 0
				} else if t > 1 {
					t = 1
				}
			}
			nx := x0 + t*dx
			ny := y0 + t*dy
			dist := math.Hypot(px-nx, py-ny)
			coverage := half + 0.5 - dist
			if coverage <= 0 {
				continue
			}
			if coverage > 1 {
				coverage = 1
			}
			if float32(coverage) > row[x] {
				row[x] = float32(coverage)
			}
		}
	}
	p.mark(minX, minY, maxX, maxY)
}

// Polyline strokes a connected path.
func (p *Paint) Polyline(points []PointF, width float64) {
	if len(points) < 2 {
		return
	}
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		p.Segment(a.X, a.Y, b.X, b.Y, width)
	}
}

// FillRoundRect adds an anti-aliased rounded rectangle.
func (p *Paint) FillRoundRect(x, y, w, h, radius float64) {
	p.roundRectSDF(x, y, w, h, radius, 0, 0)
}

// StrokeRoundRect adds an anti-aliased rounded rectangle outline.
func (p *Paint) StrokeRoundRect(x, y, w, h, radius, width float64) {
	p.roundRectSDF(x, y, w, h, radius, width, 1)
}

func (p *Paint) roundRectSDF(x, y, w, h, radius, width, mode float64) {
	if p.cov == nil || w <= 0 || h <= 0 {
		return
	}
	maxRadius := math.Min(w, h) / 2
	if radius > maxRadius {
		radius = maxRadius
	}
	if radius < 0 {
		radius = 0
	}
	halfW := w / 2
	halfH := h / 2
	cx := x + halfW
	cy := y + halfH

	pad := 1.0
	if mode > 0 {
		pad = width/2 + 1
	}
	minX := int(math.Floor(x - pad))
	maxX := int(math.Ceil(x + w + pad))
	minY := int(math.Floor(y - pad))
	maxY := int(math.Ceil(y + h + pad))
	if minX < 0 {
		minX = 0
	}
	if minY < 0 {
		minY = 0
	}
	if maxX >= p.w {
		maxX = p.w - 1
	}
	if maxY >= p.h {
		maxY = p.h - 1
	}
	if minX > maxX || minY > maxY {
		return
	}

	innerX := halfW - radius
	innerY := halfH - radius

	for py := minY; py <= maxY; py++ {
		row := p.cov[py*p.w : (py+1)*p.w]
		fy := float64(py) + 0.5
		for px := minX; px <= maxX; px++ {
			fx := float64(px) + 0.5
			dx := math.Abs(fx-cx) - innerX
			dy := math.Abs(fy-cy) - innerY
			var dist float64
			if dx > 0 && dy > 0 {
				dist = math.Hypot(dx, dy) - radius
			} else {
				dist = math.Max(dx, dy) - radius
			}
			var coverage float64
			if mode == 0 {
				coverage = 0.5 - dist
			} else {
				coverage = width/2 + 0.5 - math.Abs(dist)
			}
			if coverage <= 0 {
				continue
			}
			if coverage > 1 {
				coverage = 1
			}
			if float32(coverage) > row[px] {
				row[px] = float32(coverage)
			}
		}
	}
	p.mark(minX, minY, maxX, maxY)
}

// FillCircle adds an anti-aliased disc.
func (p *Paint) FillCircle(cx, cy, radius float64) {
	if p.cov == nil || radius <= 0 {
		return
	}
	minX := int(math.Floor(cx - radius - 1))
	maxX := int(math.Ceil(cx + radius + 1))
	minY := int(math.Floor(cy - radius - 1))
	maxY := int(math.Ceil(cy + radius + 1))
	if minX < 0 {
		minX = 0
	}
	if minY < 0 {
		minY = 0
	}
	if maxX >= p.w {
		maxX = p.w - 1
	}
	if maxY >= p.h {
		maxY = p.h - 1
	}
	for y := minY; y <= maxY; y++ {
		row := p.cov[y*p.w : (y+1)*p.w]
		fy := float64(y) + 0.5
		for x := minX; x <= maxX; x++ {
			fx := float64(x) + 0.5
			coverage := radius + 0.5 - math.Hypot(fx-cx, fy-cy)
			if coverage <= 0 {
				continue
			}
			if coverage > 1 {
				coverage = 1
			}
			if float32(coverage) > row[x] {
				row[x] = float32(coverage)
			}
		}
	}
	p.mark(minX, minY, maxX, maxY)
}

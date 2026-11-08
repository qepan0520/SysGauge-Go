package ui

import (
	"image"
	"image/color"
	"math"

	"github.com/qepan0520/SysGauge-Go/internal/win32"
)

// drawCircleIcon renders the tray status dot.
func drawCircleIcon(size int, fill color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	center := float64(size) / 2
	radius := float64(size)*0.42 - 0.5
	fillCircle(img, center, center, radius, fill)
	border := color.RGBA{R: 0x1F, G: 0x29, B: 0x37, A: 0xFF}
	strokeCircle(img, center, center, radius-1, math.Max(2, float64(size)/18), border)
	return img
}

// drawAppIcon renders the window icon: a dial on a rounded square.
func drawAppIcon(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	radius := s * 0.22

	top := color.RGBA{R: 0x4C, G: 0x8D, B: 0xFF, A: 0xFF}
	bottom := color.RGBA{R: 0x2A, G: 0x5B, B: 0xD6, A: 0xFF}
	for y := 0; y < size; y++ {
		t := float64(y) / s
		c := color.RGBA{
			R: uint8(float64(top.R) + (float64(bottom.R)-float64(top.R))*t),
			G: uint8(float64(top.G) + (float64(bottom.G)-float64(top.G))*t),
			B: uint8(float64(top.B) + (float64(bottom.B)-float64(top.B))*t),
			A: 255,
		}
		for x := 0; x < size; x++ {
			coverage := roundRectCoverage(float64(x)+0.5, float64(y)+0.5, 0, 0, s, s, radius)
			if coverage > 0 {
				blendPixel(img, x, y, c, coverage)
			}
		}
	}

	cx, cy := s/2, s*0.60
	r := s * 0.29
	white := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	strokeArc(img, cx, cy, r, math.Max(2, s*0.075), math.Pi, 2*math.Pi, white)

	angle := math.Pi * 1.28
	x2 := cx + math.Cos(angle)*r*0.72
	y2 := cy + math.Sin(angle)*r*0.72
	strokeLine(img, cx, cy, x2, y2, math.Max(2, s*0.06), white)
	fillCircle(img, cx, cy, math.Max(1.5, s*0.045), white)
	return img
}

func roundRectCoverage(px, py, x, y, w, h, radius float64) float64 {
	cx := x + w/2
	cy := y + h/2
	dx := math.Abs(px-cx) - (w/2 - radius)
	dy := math.Abs(py-cy) - (h/2 - radius)
	var dist float64
	if dx > 0 && dy > 0 {
		dist = math.Hypot(dx, dy) - radius
	} else {
		dist = math.Max(dx, dy) - radius
	}
	return clamp01(0.5 - dist)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func blendPixel(img *image.RGBA, x, y int, c color.RGBA, alpha float64) {
	if alpha <= 0 || x < 0 || y < 0 || x >= img.Rect.Dx() || y >= img.Rect.Dy() {
		return
	}
	if alpha > 1 {
		alpha = 1
	}
	i := img.PixOffset(x, y)
	a := alpha * float64(c.A) / 255
	inv := 1 - a
	img.Pix[i+0] = uint8(float64(c.R)*a + float64(img.Pix[i+0])*inv)
	img.Pix[i+1] = uint8(float64(c.G)*a + float64(img.Pix[i+1])*inv)
	img.Pix[i+2] = uint8(float64(c.B)*a + float64(img.Pix[i+2])*inv)
	img.Pix[i+3] = uint8(math.Max(float64(img.Pix[i+3]), 255*alpha))
}

func fillCircle(img *image.RGBA, cx, cy, r float64, c color.RGBA) {
	for y := int(cy - r - 1); y <= int(cy+r+1); y++ {
		for x := int(cx - r - 1); x <= int(cx+r+1); x++ {
			coverage := r + 0.5 - math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			blendPixel(img, x, y, c, clamp01(coverage))
		}
	}
}

func strokeCircle(img *image.RGBA, cx, cy, r, width float64, c color.RGBA) {
	strokeArc(img, cx, cy, r, width, 0, 2*math.Pi, c)
}

func strokeArc(img *image.RGBA, cx, cy, r, width, start, end float64, c color.RGBA) {
	steps := int(math.Max(24, r*4))
	for i := 0; i <= steps; i++ {
		a := start + (end-start)*float64(i)/float64(steps)
		x := cx + math.Cos(a)*r
		y := cy + math.Sin(a)*r
		fillCircle(img, x, y, width/2, c)
	}
}

func strokeLine(img *image.RGBA, x0, y0, x1, y1, width float64, c color.RGBA) {
	length := math.Hypot(x1-x0, y1-y0)
	steps := int(math.Max(1, length))
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x := x0 + (x1-x0)*t
		y := y0 + (y1-y0)*t
		fillCircle(img, x, y, width/2, c)
	}
}

// imagePixels converts an image to the packed BGRA slice win32.IconFromImage
// expects.
func imagePixels(img image.Image) (int, int, []uint32) {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	pixels := make([]uint32, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, a := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			pixels[y*w+x] = (a>>8)<<24 | (r>>8)<<16 | (g>>8)<<8 | (b >> 8)
		}
	}
	return w, h, pixels
}

// trayIcon builds (and caches) a tray dot for a status level.
func (m *MainWindow) trayIcon(level string) uintptr {
	if icon, ok := m.icons[level]; ok {
		return icon
	}
	col := map[string]color.RGBA{
		"low":  {R: 0x10, G: 0xB9, B: 0x81, A: 0xFF},
		"mid":  {R: 0xF5, G: 0x9E, B: 0x0B, A: 0xFF},
		"high": {R: 0xEF, G: 0x44, B: 0x44, A: 0xFF},
		"idle": {R: 0x6B, G: 0x72, B: 0x80, A: 0xFF},
	}[level]
	w, h, pixels := imagePixels(drawCircleIcon(32, col))
	icon := win32.IconFromImage(w, h, pixels)
	m.icons[level] = icon
	return icon
}

// appIcon builds the window/taskbar icon once.
func (m *MainWindow) appIcon() uintptr {
	if m.windowIcon != 0 {
		return m.windowIcon
	}
	w, h, pixels := imagePixels(drawAppIcon(64))
	m.windowIcon = win32.IconFromImage(w, h, pixels)
	return m.windowIcon
}

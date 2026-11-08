package win32

import (
	"image/color"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Surface is a double-buffered 32-bit DIB with a memory DC.
//
// Everything is rendered into Pix first and blitted to the window in one
// BitBlt, which removes flicker entirely.
type Surface struct {
	DC    uintptr
	bmp   uintptr
	old   uintptr
	W, H  int
	Pix   []uint32
	stale bool
}

// NewSurface creates an empty surface; Resize allocates the pixels.
func NewSurface() *Surface {
	dc, _, _ := procCreateCompatibleDC.Call(0)
	return &Surface{DC: dc}
}

// Resize reallocates the backing bitmap when the size changes.
func (s *Surface) Resize(w, h int) bool {
	if w <= 0 || h <= 0 {
		return false
	}
	if s.W == w && s.H == h && s.bmp != 0 {
		return true
	}
	s.freeBitmap()

	info := bitmapInfo{}
	info.Header.Size = uint32(unsafe.Sizeof(info.Header))
	info.Header.Width = int32(w)
	info.Header.Height = -int32(h) // top-down
	info.Header.Planes = 1
	info.Header.BitCount = 32
	info.Header.Compression = 0 // BI_RGB

	var bits unsafe.Pointer
	bmp, _, _ := procCreateDIBSection.Call(
		s.DC,
		uintptr(unsafe.Pointer(&info)),
		0,
		uintptr(unsafe.Pointer(&bits)),
		0,
		0,
	)
	if bmp == 0 || bits == nil {
		return false
	}
	s.bmp = bmp
	s.old, _, _ = procSelectObject.Call(s.DC, bmp)
	s.W = w
	s.H = h
	s.Pix = unsafe.Slice((*uint32)(bits), w*h)
	s.Fill(color.RGBA{A: 255})
	return true
}

// Free releases all GDI objects.
func (s *Surface) Free() {
	s.freeBitmap()
	if s.DC != 0 {
		procDeleteDC.Call(s.DC)
		s.DC = 0
	}
}

func (s *Surface) freeBitmap() {
	if s.bmp != 0 {
		if s.old != 0 {
			procSelectObject.Call(s.DC, s.old)
			s.old = 0
		}
		procDeleteObject.Call(s.bmp)
		s.bmp = 0
		s.Pix = nil
		s.W, s.H = 0, 0
	}
}

// Blit copies the surface to a device context.
func (s *Surface) Blit(dst uintptr) {
	if s.bmp == 0 {
		return
	}
	// GDI ClearType text leaves the alpha channel of glyph pixels at zero.
	// BitBlt ignores alpha, but DWM and screenshot APIs do not, so force the
	// whole surface opaque before presenting it.
	for i := range s.Pix {
		s.Pix[i] |= 0xFF000000
	}
	procBitBlt.Call(dst, 0, 0, uintptr(s.W), uintptr(s.H), s.DC, 0, 0, SRCCOPY)
}

// Fill paints the whole surface.
func (s *Surface) Fill(c color.RGBA) {
	s.FillRect(0, 0, s.W, s.H, c)
}

// FillRect paints an axis-aligned rectangle.
func (s *Surface) FillRect(x, y, w, h int, c color.RGBA) {
	if w <= 0 || h <= 0 {
		return
	}
	x0, y0 := x, y
	x1, y1 := x+w, y+h
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > s.W {
		x1 = s.W
	}
	if y1 > s.H {
		y1 = s.H
	}
	if x0 >= x1 || y0 >= y1 {
		return
	}
	px := pack(c)
	for row := y0; row < y1; row++ {
		rowStart := s.Pix[row*s.W+x0 : row*s.W+x1]
		for i := range rowStart {
			rowStart[i] = px
		}
	}
}

// BlendRect paints a translucent rectangle.
func (s *Surface) BlendRect(x, y, w, h int, c color.RGBA, alpha float64) {
	if alpha <= 0 {
		return
	}
	if alpha >= 1 {
		s.FillRect(x, y, w, h, c)
		return
	}
	s.forEachPixel(x, y, w, h, func(index int) {
		s.Pix[index] = blend(s.Pix[index], c, alpha)
	})
}

// Line draws a crisp 1px horizontal or vertical line.
func (s *Surface) Line(x0, y0, x1, y1 int, c color.RGBA) {
	px := pack(c)
	if y0 == y1 {
		if y0 < 0 || y0 >= s.H {
			return
		}
		if x0 > x1 {
			x0, x1 = x1, x0
		}
		if x0 < 0 {
			x0 = 0
		}
		if x1 > s.W {
			x1 = s.W
		}
		row := s.Pix[y0*s.W : (y0+1)*s.W]
		for x := x0; x < x1; x++ {
			row[x] = px
		}
		return
	}
	if x0 == x1 {
		if x0 < 0 || x0 >= s.W {
			return
		}
		if y0 > y1 {
			y0, y1 = y1, y0
		}
		if y0 < 0 {
			y0 = 0
		}
		if y1 > s.H {
			y1 = s.H
		}
		for y := y0; y < y1; y++ {
			s.Pix[y*s.W+x0] = px
		}
		return
	}
	s.ddaLine(x0, y0, x1, y1, c)
}

// ddaLine draws a thin diagonal line without anti-aliasing (used for grid
// decorations only; chart curves go through the coverage rasterizer).
func (s *Surface) ddaLine(x0, y0, x1, y1 int, c color.RGBA) {
	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		if x0 >= 0 && x0 < s.W && y0 >= 0 && y0 < s.H {
			s.Pix[y0*s.W+x0] = pack(c)
		}
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

// Text draws a single line of text, vertically centred inside the rectangle.
func (s *Surface) Text(text string, x, y, w, h int, font *Font, c color.RGBA, align uint32) {
	if text == "" || font == nil || w <= 0 || h <= 0 {
		return
	}
	procSetBkMode.Call(s.DC, TRANSPARENT)
	procSetTextColor.Call(s.DC, uintptr(colorRef(c)))
	old, _, _ := procSelectObject.Call(s.DC, font.handle)
	r := Rect{Left: int32(x), Top: int32(y), Right: int32(x + w), Bottom: int32(y + h)}
	DrawText(s.DC, text, r, align|DT_SINGLELINE|DT_VCENTER|DT_NOPREFIX)
	if old != 0 {
		procSelectObject.Call(s.DC, old)
	}
}

// Measure returns the width of a text run in device pixels.
func (s *Surface) Measure(text string, font *Font) int {
	if font == nil || text == "" {
		return 0
	}
	old, _, _ := procSelectObject.Call(s.DC, font.handle)
	width := TextWidth(s.DC, text)
	if old != 0 {
		procSelectObject.Call(s.DC, old)
	}
	return width
}

func (s *Surface) forEachPixel(x, y, w, h int, fn func(index int)) {
	x0, y0 := x, y
	x1, y1 := x+w, y+h
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > s.W {
		x1 = s.W
	}
	if y1 > s.H {
		y1 = s.H
	}
	for row := y0; row < y1; row++ {
		for col := x0; col < x1; col++ {
			fn(row*s.W + col)
		}
	}
}

// ---- Colours --------------------------------------------------------------

func pack(c color.RGBA) uint32 {
	return uint32(c.A)<<24 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
}

// colorRef converts a colour to the COLORREF layout GDI expects (0x00BBGGRR).
func colorRef(c color.RGBA) uint32 {
	return uint32(c.B)<<16 | uint32(c.G)<<8 | uint32(c.R)
}

func unpack(v uint32) color.RGBA {
	return color.RGBA{A: uint8(v >> 24), R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v)}
}

func blend(dst uint32, c color.RGBA, alpha float64) uint32 {
	if alpha <= 0 {
		return dst
	}
	if alpha > 1 {
		alpha = 1
	}
	d := unpack(dst)
	a := alpha * float64(c.A) / 255
	if a >= 1 {
		return pack(c)
	}
	inv := 1 - a
	return pack(color.RGBA{
		R: uint8(float64(c.R)*a + float64(d.R)*inv),
		G: uint8(float64(c.G)*a + float64(d.G)*inv),
		B: uint8(float64(c.B)*a + float64(d.B)*inv),
		A: 0xFF,
	})
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// ---- Fonts ----------------------------------------------------------------

// Font wraps an HFONT.
type Font struct {
	handle uintptr
	Height int
	Weight int
}

var (
	fontMu    sync.Mutex
	fontCache = map[[2]int]*Font{}
	fontFace  = "Microsoft YaHei UI"
)

// Face returns the UI font family in use.
func Face() string { return fontFace }

// FontSize returns a cached font for the given pixel height and weight.
func FontSize(height, weight int) *Font {
	if height < 6 {
		height = 6
	}
	key := [2]int{height, weight}
	fontMu.Lock()
	defer fontMu.Unlock()
	if f, ok := fontCache[key]; ok {
		return f
	}
	face, err := windows.UTF16PtrFromString(fontFace)
	if err != nil {
		return nil
	}
	handle, _, _ := procCreateFontW.Call(
		uintptr(-int32(height)), 0, 0, 0,
		uintptr(weight),
		0, 0, 0,
		DEFAULT_CHARSET,
		0, 0,
		CLEARTYPE_QUALITY,
		0,
		uintptr(unsafe.Pointer(face)),
	)
	if handle == 0 {
		return nil
	}
	f := &Font{handle: handle, Height: height, Weight: weight}
	fontCache[key] = f
	return f
}

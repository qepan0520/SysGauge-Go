// Package win32 is a small, dependency-free facade over the User32 / GDI32
// calls this application needs: one child window with a custom window
// procedure, a double-buffered DIB surface and crisp text rendering.
package win32

import (
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Common window messages.
const (
	WM_DESTROY        = 0x0002
	WM_SIZE           = 0x0005
	WM_SETFOCUS       = 0x0007
	WM_KILLFOCUS      = 0x0008
	WM_PAINT          = 0x000F
	WM_CLOSE          = 0x0010
	WM_ERASEBKGND     = 0x0014
	WM_SETCURSOR      = 0x0020
	WM_MOUSEMOVE      = 0x0200
	WM_LBUTTONDOWN    = 0x0201
	WM_LBUTTONUP      = 0x0202
	WM_LBUTTONDBLCLK  = 0x0203
	WM_RBUTTONDOWN    = 0x0204
	WM_RBUTTONUP      = 0x0205
	WM_MOUSEWHEEL     = 0x020A
	WM_MOUSELEAVE     = 0x02A3
	WM_TIMER          = 0x0113
	WM_KEYDOWN        = 0x0100
	WM_KEYUP          = 0x0101
	WM_CHAR           = 0x0102
	WM_ACTIVATE       = 0x0006
	WM_CAPTURECHANGED = 0x0215

	// WM_APP is the base for private messages.
	WM_APP = 0x8000

	WS_CHILD            = 0x40000000
	WS_VISIBLE          = 0x10000000
	WS_CLIPCHILDREN     = 0x02000000
	WS_CLIPSIBLINGS     = 0x04000000
	WS_POPUP            = 0x80000000
	WS_TABSTOP          = 0x00010000
	WS_OVERLAPPEDWINDOW = 0x00CF0000

	WM_GETMINMAXINFO = 0x0024
	WM_DPICHANGED    = 0x02E0
	WM_SETICON       = 0x0080
	WM_CONTEXTMENU   = 0x007B

	// WM_SETTINGCHANGE arrives when the user flips the Windows app theme;
	// WM_THEMECHANGED is the visual-style counterpart.
	WM_SETTINGCHANGE = 0x001A
	WM_THEMECHANGED  = 0x031D

	WS_EX_TOOLWINDOW = 0x00000080

	SW_SHOW     = 5
	SW_HIDE     = 0
	SW_SHOWNA   = 8
	SW_MAXIMIZE = 3
	SW_RESTORE  = 9

	VK_RETURN = 0x0D
	VK_ESCAPE = 0x1B
	VK_BACK   = 0x08
	VK_UP     = 0x26
	VK_DOWN   = 0x28
	VK_TAB    = 0x09

	DT_LEFT         = 0x0000
	DT_CENTER       = 0x0001
	DT_RIGHT        = 0x0002
	DT_VCENTER      = 0x0004
	DT_SINGLELINE   = 0x0020
	DT_NOPREFIX     = 0x0800
	DT_END_ELLIPSIS = 0x00008000
	DT_CALCRECT     = 0x0400

	TRANSPARENT       = 1
	CLEARTYPE_QUALITY = 5
	DEFAULT_CHARSET   = 1
	FW_NORMAL         = 400
	FW_SEMIBOLD       = 600
	FW_BOLD           = 700

	IDC_ARROW  = 32512
	IDC_IBEAM  = 32513
	IDC_HAND   = 32649
	IDC_SIZEWE = 32644
	IDC_WAIT   = 32514

	SRCCOPY = 0x00CC0020

	TME_LEAVE = 0x00000002
)

// Rect mirrors the Win32 RECT.
type Rect struct {
	Left, Top, Right, Bottom int32
}

// Size mirrors SIZE.
type Size struct {
	CX, CY int32
}

// Point mirrors POINT.
type Point struct {
	X, Y int32
}

// WndClassEx mirrors WNDCLASSEXW.
type WndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type paintStruct struct {
	Hdc       uintptr
	Erase     int32
	Paint     Rect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

type trackMouseEvent struct {
	Size      uint32
	Flags     uint32
	Track     uintptr
	HoverTime uint32
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type rgbQuad struct {
	Blue, Green, Red, Reserved byte
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]rgbQuad
}

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW      = user32.NewProc("RegisterClassExW")
	procCreateWindowExW       = user32.NewProc("CreateWindowExW")
	procDefWindowProcW        = user32.NewProc("DefWindowProcW")
	procDestroyWindow         = user32.NewProc("DestroyWindow")
	procShowWindow            = user32.NewProc("ShowWindow")
	procShowWindowAsync       = user32.NewProc("ShowWindowAsync")
	procSetWindowPos          = user32.NewProc("SetWindowPos")
	procGetClientRect         = user32.NewProc("GetClientRect")
	procInvalidateRect        = user32.NewProc("InvalidateRect")
	procBeginPaint            = user32.NewProc("BeginPaint")
	procEndPaint              = user32.NewProc("EndPaint")
	procSetFocus              = user32.NewProc("SetFocus")
	procGetFocus              = user32.NewProc("GetFocus")
	procSetCapture            = user32.NewProc("SetCapture")
	procReleaseCapture        = user32.NewProc("ReleaseCapture")
	procLoadCursorW           = user32.NewProc("LoadCursorW")
	procPostMessageW          = user32.NewProc("PostMessageW")
	procMessageBoxW           = user32.NewProc("MessageBoxW")
	procSendMessageW          = user32.NewProc("SendMessageW")
	procSetTimer              = user32.NewProc("SetTimer")
	procKillTimer             = user32.NewProc("KillTimer")
	procGetCursorPos          = user32.NewProc("GetCursorPos")
	procScreenToClient        = user32.NewProc("ScreenToClient")
	procClientToScreen        = user32.NewProc("ClientToScreen")
	procTrackMouseEvent       = user32.NewProc("TrackMouseEvent")
	procSetCursor             = user32.NewProc("SetCursor")
	procGetDpiForWindow       = user32.NewProc("GetDpiForWindow")
	procGetDpiForSystem       = user32.NewProc("GetDpiForSystem")
	procGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	procIsZoomed              = user32.NewProc("IsZoomed")
	procIsIconic              = user32.NewProc("IsIconic")
	procSetProcessDpiAwareCtx = user32.NewProc("SetProcessDpiAwarenessContext")
	procSetProcessDPIAware    = user32.NewProc("SetProcessDPIAware")
	procEnumWindows           = user32.NewProc("EnumWindows")
	procGetWindowTextW        = user32.NewProc("GetWindowTextW")
	procIsWindowVisible       = user32.NewProc("IsWindowVisible")
	procRegisterWindowMessage = user32.NewProc("RegisterWindowMessageW")
	procGetModuleHandleW      = kernel32.NewProc("GetModuleHandleW")
	procGetTickCount64        = kernel32.NewProc("GetTickCount64")

	procCreateCompatibleDC    = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection      = gdi32.NewProc("CreateDIBSection")
	procSelectObject          = gdi32.NewProc("SelectObject")
	procDeleteObject          = gdi32.NewProc("DeleteObject")
	procDeleteDC              = gdi32.NewProc("DeleteDC")
	procBitBlt                = gdi32.NewProc("BitBlt")
	procCreateFontW           = gdi32.NewProc("CreateFontW")
	procSetTextColor          = gdi32.NewProc("SetTextColor")
	procSetBkMode             = gdi32.NewProc("SetBkMode")
	procDrawTextW             = user32.NewProc("DrawTextW")
	procGetTextExtentPoint32W = gdi32.NewProc("GetTextExtentPoint32W")
	procGdiFlush              = gdi32.NewProc("GdiFlush")
)

// EnableDPIAwareness switches the process to per-monitor v2 scaling when the
// OS supports it and falls back to system DPI awareness otherwise.
func EnableDPIAwareness() {
	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 == (HANDLE)-4
	const perMonitorV2 = ^uintptr(3)
	if r, _, _ := procSetProcessDpiAwareCtx.Call(perMonitorV2); r != 0 {
		return
	}
	procSetProcessDPIAware.Call()
}

// Procedure is the message handler signature used by callbacks.
type Procedure func(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr

// RegisterClass registers a window class backed by the given procedure.
// The returned name can be passed to CreateWindow.
func RegisterClass(name string, proc Procedure) (string, error) {
	className, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return "", err
	}
	cursor, _, _ := procLoadCursorW.Call(0, IDC_ARROW)
	instance, _, _ := procGetModuleHandleW.Call(0)
	wc := WndClassEx{
		Style: 0x0020 | 0x0001 | 0x0002, // CS_OWNDC | CS_VREDRAW | CS_HREDRAW
		WndProc: syscall.NewCallback(func(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
			return proc(hwnd, msg, wp, lp)
		}),
		Instance:  instance,
		Cursor:    cursor,
		ClassName: className,
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		return "", err
	}
	return name, nil
}

// CreateWindow creates a window of a class registered by RegisterClass.
func CreateWindow(class, title string, exStyle, style uint32, x, y, w, h int32, parent uintptr) (uintptr, error) {
	className, err := windows.UTF16PtrFromString(class)
	if err != nil {
		return 0, err
	}
	titlePtr, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return 0, err
	}
	instance, _, _ := procGetModuleHandleW.Call(0)
	hwnd, _, callErr := procCreateWindowExW.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent, 0, instance, 0,
	)
	if hwnd == 0 {
		return 0, callErr
	}
	return hwnd, nil
}

// DefWindowProc forwards a message to the default handler.
func DefWindowProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wparam, lparam)
	return r
}

// DestroyWindow destroys the window.
func DestroyWindow(hwnd uintptr) { procDestroyWindow.Call(hwnd) }

// ShowWindow shows or hides the window.
func ShowWindow(hwnd uintptr, cmd int32) { procShowWindow.Call(hwnd, uintptr(cmd)) }

// ShowWindowAsync posts the show request instead of processing it inline.
// Showing a window synchronously can deadlock with the shell while it is still
// finishing its own startup work, so the first reveal uses the async variant.
func ShowWindowAsync(hwnd uintptr, cmd int32) bool {
	r, _, _ := procShowWindowAsync.Call(hwnd, uintptr(cmd))
	return r != 0
}

// SetWindowPos moves and resizes a window.
func SetWindowPos(hwnd uintptr, x, y, w, h int32) {
	procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0x0004|0x0010)
}

// RedrawFrame forces the non-client area to repaint, which is required after
// the dark-mode title bar attribute changes.
func RedrawFrame(hwnd uintptr) {
	const swpNoSize, swpNoMove, swpNoZOrder, swpNoActivate, swpFrameChanged = 0x0001, 0x0002, 0x0004, 0x0010, 0x0020
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0,
		swpNoSize|swpNoMove|swpNoZOrder|swpNoActivate|swpFrameChanged)
}

// ClientRect returns the client area in pixels.
func ClientRect(hwnd uintptr) (w, h int) {
	var r Rect
	if ok, _, _ := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return 0, 0
	}
	return int(r.Right - r.Left), int(r.Bottom - r.Top)
}

// Invalidate marks the whole client area as needing a repaint.
func Invalidate(hwnd uintptr) {
	procInvalidateRect.Call(hwnd, 0, 0)
}

// InvalidateRect marks a rectangle as needing a repaint.
func InvalidateArea(hwnd uintptr, x, y, w, h int32) {
	r := Rect{Left: x, Top: y, Right: x + w, Bottom: y + h}
	procInvalidateRect.Call(hwnd, uintptr(unsafe.Pointer(&r)), 0)
}

// BeginPaint starts a paint cycle.
func BeginPaint(hwnd uintptr) (uintptr, *paintStruct) {
	var ps paintStruct
	dc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	return dc, &ps
}

// EndPaint finishes a paint cycle.
func EndPaint(hwnd uintptr, ps *paintStruct) {
	procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(ps)))
}

// SetFocus moves keyboard focus to the window.
func SetFocus(hwnd uintptr) { procSetFocus.Call(hwnd) }

// Focus returns the focused window.
func Focus() uintptr {
	r, _, _ := procGetFocus.Call()
	return r
}

// SetCapture routes all mouse input to the window until ReleaseCapture.
func SetCapture(hwnd uintptr) { procSetCapture.Call(hwnd) }

// ReleaseCapture releases the mouse capture.
func ReleaseCapture() { procReleaseCapture.Call() }

// PostMessage posts a message to the window queue.
func PostMessage(hwnd uintptr, msg uint32, wparam, lparam uintptr) bool {
	r, _, _ := procPostMessageW.Call(hwnd, uintptr(msg), wparam, lparam)
	return r != 0
}

// MessageBox displays a modal message box owned by hwnd (0 for none).
func MessageBox(hwnd uintptr, title, text string, flags uint32) {
	titlePtr, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	textPtr, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	procMessageBoxW.Call(hwnd, uintptr(unsafe.Pointer(textPtr)), uintptr(unsafe.Pointer(titlePtr)), uintptr(flags))
}

// SendMessage sends a message and waits for the result.
func SendMessage(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	r, _, _ := procSendMessageW.Call(hwnd, uintptr(msg), wparam, lparam)
	return r
}

// SetTimer installs a timer that posts WM_TIMER.
func SetTimer(hwnd uintptr, id uintptr, millis uint32) {
	procSetTimer.Call(hwnd, id, uintptr(millis), 0)
}

// KillTimer removes a timer.
func KillTimer(hwnd uintptr, id uintptr) { procKillTimer.Call(hwnd, id) }

// CursorPos returns the cursor position in screen coordinates.
func CursorPos() Point {
	var p Point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p
}

// ScreenToClient converts screen coordinates to client coordinates.
func ScreenToClient(hwnd uintptr, p Point) Point {
	procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&p)))
	return p
}

// ClientToScreen converts client coordinates to screen coordinates.
func ClientToScreen(hwnd uintptr, p Point) Point {
	procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&p)))
	return p
}

// TrackLeave asks for a WM_MOUSELEAVE notification.
func TrackLeave(hwnd uintptr) {
	tme := trackMouseEvent{Flags: TME_LEAVE, Track: hwnd}
	tme.Size = uint32(unsafe.Sizeof(tme))
	procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
}

// SetCursor sets the mouse cursor shape.
func SetCursor(id int32) {
	cursor, _, _ := procLoadCursorW.Call(0, uintptr(id))
	procSetCursor.Call(cursor)
}

// DPIForWindow returns the effective DPI of a window.
func DPIForWindow(hwnd uintptr) int {
	r, _, _ := procGetDpiForWindow.Call(hwnd)
	if r == 0 {
		return 96
	}
	return int(r)
}

// SystemDPI returns the system DPI used before any window exists.
func SystemDPI() int {
	r, _, _ := procGetDpiForSystem.Call()
	if r == 0 {
		return 96
	}
	return int(r)
}

func u16(v string) []uint16 { return utf16.Encode([]rune(v)) }

// ScreenMetrics returns SM_CXSCREEN (index 0) or SM_CYSCREEN (index 1).
func ScreenMetrics(index int) int {
	ids := []uintptr{0, 1}
	if index < 0 || index >= len(ids) {
		return 0
	}
	r, _, _ := procGetSystemMetrics.Call(ids[index])
	return int(int32(r))
}

// IsZoomed reports whether the window is maximized.
func IsZoomed(hwnd uintptr) bool {
	r, _, _ := procIsZoomed.Call(hwnd)
	return r != 0
}

// IsIconic reports whether the window is minimized.
func IsIconic(hwnd uintptr) bool {
	r, _, _ := procIsIconic.Call(hwnd)
	return r != 0
}

// RegisterWindowMessage registers a message that every process can share.
func RegisterWindowMessage(name string) uint32 {
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0
	}
	r, _, _ := procRegisterWindowMessage.Call(uintptr(unsafe.Pointer(ptr)))
	return uint32(r)
}

// VisibleWindowWithTitlePrefix 报告是否存在标题以 prefix 开头的可见、非最小化
// 顶层窗口。它用来判断激活请求是否真的把已有窗口带了回来。
func VisibleWindowWithTitlePrefix(prefix string) bool {
	found := false
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if visible, _, _ := procIsWindowVisible.Call(hwnd); visible == 0 {
			return 1
		}
		if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
			return 1
		}
		var buf [256]uint16
		n, _, _ := procGetWindowTextW.Call(hwnd,
			uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n == 0 {
			return 1
		}
		if strings.HasPrefix(string(utf16.Decode(buf[:int(n)])), prefix) {
			found = true
			return 0
		}
		return 1
	})
	procEnumWindows.Call(callback, 0)
	return found
}

// TickCount returns milliseconds since system start.
func TickCount() uint64 {
	r, _, _ := procGetTickCount64.Call()
	return uint64(r)
}

// GdiFlush forces pending GDI operations to complete.
func GdiFlush() { procGdiFlush.Call() }

// DrawText draws a single line of UTF-16 text inside r.
func DrawText(dc uintptr, text string, r Rect, flags uint32) {
	ptr, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	procDrawTextW.Call(dc, uintptr(unsafe.Pointer(ptr)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), uintptr(flags))
}

// TextWidth measures a string in the given device context and font.
func TextWidth(dc uintptr, text string) int {
	ptr, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return 0
	}
	var size Size
	units := utf16.Encode([]rune(text))
	if len(units) == 0 {
		return 0
	}
	if ok, _, _ := procGetTextExtentPoint32W.Call(dc, uintptr(unsafe.Pointer(ptr)), uintptr(len(units)), uintptr(unsafe.Pointer(&size))); ok == 0 {
		return 0
	}
	return int(size.CX)
}

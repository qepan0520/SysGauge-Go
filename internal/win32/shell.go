package win32

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Ptr converts a message parameter into a pointer. The value is an address
// supplied by Windows, never a Go heap pointer.
func Ptr(v uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&v))
}

// Msg mirrors the Win32 MSG structure.
type Msg struct {
	HWnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       Point
	LPrivate uint32
}

// MinMaxInfo mirrors MINMAXINFO.
type MinMaxInfo struct {
	PtReserved     Point
	PtMaxSize      Point
	PtMaxPosition  Point
	PtMinTrackSize Point
	PtMaxTrackSize Point
}

type iconInfo struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

var (
	procGetMessageW        = user32.NewProc("GetMessageW")
	procTranslateMessage   = user32.NewProc("TranslateMessage")
	procDispatchMessageW   = user32.NewProc("DispatchMessageW")
	procPostQuitMessage    = user32.NewProc("PostQuitMessage")
	procSetWindowTextW     = user32.NewProc("SetWindowTextW")
	procSetForegroundW     = user32.NewProc("SetForegroundWindow")
	procEnableWindow       = user32.NewProc("EnableWindow")
	procGetWindowRect      = user32.NewProc("GetWindowRect")
	procUpdateWindow       = user32.NewProc("UpdateWindow")
	procCreateIconIndirect = user32.NewProc("CreateIconIndirect")
	procDestroyIcon        = user32.NewProc("DestroyIcon")
	procCreatePopupMenu    = user32.NewProc("CreatePopupMenu")
	procAppendMenuW        = user32.NewProc("AppendMenuW")
	procTrackPopupMenuEx   = user32.NewProc("TrackPopupMenuEx")
	procDestroyMenu        = user32.NewProc("DestroyMenu")
	procEnableMenuItem     = user32.NewProc("EnableMenuItem")
	procSetMenuDefaultItem = user32.NewProc("SetMenuDefaultItem")
	procCreateBitmap       = gdi32.NewProc("CreateBitmap")

	procShellNotifyIcon     = windows.NewLazySystemDLL("shell32.dll").NewProc("Shell_NotifyIconW")
	procShellExecuteW       = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteW")
	procSHBrowseForFolder   = windows.NewLazySystemDLL("shell32.dll").NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDList = windows.NewLazySystemDLL("shell32.dll").NewProc("SHGetPathFromIDListW")
	procGetOpenFileNameW    = windows.NewLazySystemDLL("comdlg32.dll").NewProc("GetOpenFileNameW")
	procDwmSetAttribute     = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
	procOleInitialize       = windows.NewLazySystemDLL("ole32.dll").NewProc("OleInitialize")
	procCoTaskMemFree       = windows.NewLazySystemDLL("ole32.dll").NewProc("CoTaskMemFree")
)

// RunMessageLoop pumps messages until WM_QUIT.
func RunMessageLoop() int {
	var msg Msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) == -1 || r == 0 {
			return int(int32(msg.WParam))
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// PostQuitMessage ends the message loop.
func PostQuitMessage(code int) { procPostQuitMessage.Call(uintptr(code)) }

// SetWindowText updates a window title.
func SetWindowText(hwnd uintptr, text string) {
	ptr, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(ptr)))
}

// SetForegroundWindow activates a window.
func SetForegroundWindow(hwnd uintptr) { procSetForegroundW.Call(hwnd) }

// EnableWindow enables or disables input for a window.
func EnableWindow(hwnd uintptr, enable bool) {
	value := uintptr(0)
	if enable {
		value = 1
	}
	procEnableWindow.Call(hwnd, value)
}

// WindowRect returns the outer window rectangle in screen coordinates.
func WindowRect(hwnd uintptr) (x, y, w, h int) {
	var r Rect
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return int(r.Left), int(r.Top), int(r.Right - r.Left), int(r.Bottom - r.Top)
}

// Update repaints immediately.
func Update(hwnd uintptr) { procUpdateWindow.Call(hwnd) }

// CenterWindow centers a window of the given size on the primary screen.
func CenterWindow(w, h int) (int, int) {
	sw, sh := ScreenMetrics(0), ScreenMetrics(1)
	if sw <= 0 || sh <= 0 {
		return 100, 100
	}
	x := (sw - w) / 2
	y := (sh - h) / 3
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}

// SetDarkTitleBar switches the non-client area to dark mode where supported.
func SetDarkTitleBar(hwnd uintptr, dark bool) {
	if procDwmSetAttribute.Find() != nil {
		return
	}
	value := int32(0)
	if dark {
		value = 1
	}
	const dwmwaUseImmersiveDarkMode = 20
	procDwmSetAttribute.Call(hwnd, dwmwaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&value)), 4)
}

// OleInit initializes COM for the shell dialogs.
func OleInit() {
	if procOleInitialize.Find() != nil {
		return
	}
	procOleInitialize.Call(0)
}

// IconFromImage converts an RGBA image into an HICON.
func IconFromImage(width, height int, pixels []uint32) uintptr {
	if width <= 0 || height <= 0 || len(pixels) < width*height {
		return 0
	}
	dc, _, _ := procCreateCompatibleDC.Call(0)
	if dc == 0 {
		return 0
	}
	defer procDeleteDC.Call(dc)

	info := bitmapInfo{}
	info.Header.Size = uint32(unsafe.Sizeof(info.Header))
	info.Header.Width = int32(width)
	info.Header.Height = -int32(height)
	info.Header.Planes = 1
	info.Header.BitCount = 32
	info.Header.Compression = 0

	var bits unsafe.Pointer
	bmp, _, _ := procCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&info)), 0,
		uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		return 0
	}
	defer procDeleteObject.Call(bmp)

	// Windows expects premultiplied alpha in icon bitmaps.
	target := unsafe.Slice((*uint32)(bits), width*height)
	for i := 0; i < width*height; i++ {
		px := pixels[i]
		a := uint32(px >> 24)
		r := (px >> 16) & 0xFF
		g := (px >> 8) & 0xFF
		b := px & 0xFF
		if a == 0 {
			target[i] = 0
			continue
		}
		if a < 0xFF {
			r = r * a / 0xFF
			g = g * a / 0xFF
			b = b * a / 0xFF
		}
		target[i] = a<<24 | r<<16 | g<<8 | b
	}

	zeros := make([]byte, ((width+15)/16)*2*height)
	mask, _, _ := procCreateBitmap.Call(uintptr(width), uintptr(height), 1, 1,
		uintptr(unsafe.Pointer(&zeros[0])))
	if mask == 0 {
		return 0
	}
	defer procDeleteObject.Call(mask)

	ii := iconInfo{FIcon: 1, HbmMask: mask, HbmColor: bmp}
	icon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	return icon
}

// DestroyIconHandle releases an HICON.
func DestroyIconHandle(hIcon uintptr) {
	if hIcon != 0 {
		procDestroyIcon.Call(hIcon)
	}
}

// ---- Menus ----------------------------------------------------------------

// MenuID is a command identifier used with TrackPopupMenu.
type MenuID uint32

// Menu wraps a popup menu handle.
type Menu struct {
	handle uintptr
}

// NewMenu creates an empty popup menu.
func NewMenu() *Menu {
	handle, _, _ := procCreatePopupMenu.Call()
	if handle == 0 {
		return nil
	}
	return &Menu{handle: handle}
}

// AddString appends a normal item.
func (m *Menu) AddString(id MenuID, text string) {
	ptr, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	const mfString = 0x0000
	procAppendMenuW.Call(m.handle, mfString, uintptr(id), uintptr(unsafe.Pointer(ptr)))
}

// AddSeparator appends a divider.
func (m *Menu) AddSeparator() {
	const mfSeparator = 0x0800
	procAppendMenuW.Call(m.handle, mfSeparator, 0, 0)
}

// SetEnabled enables or greys an item.
func (m *Menu) SetEnabled(id MenuID, enabled bool) {
	const mfByCommand = 0x0000
	const mfEnabled = 0x0000
	const mfGrayed = 0x0001
	flags := uintptr(mfByCommand | mfGrayed)
	if enabled {
		flags = mfByCommand | mfEnabled
	}
	procEnableMenuItem.Call(m.handle, uintptr(id), flags)
}

// Destroy releases the menu.
func (m *Menu) Destroy() {
	if m != nil && m.handle != 0 {
		procDestroyMenu.Call(m.handle)
		m.handle = 0
	}
}

// TrackPopup displays the menu and returns the chosen command (0 if cancelled).
func (m *Menu) TrackPopup(owner uintptr, x, y int) MenuID {
	const tpmLeftAlign = 0x0000
	const tpmRightButton = 0x0002
	const tpmReturnCmd = 0x0100
	if m == nil || m.handle == 0 {
		return 0
	}
	if owner != 0 {
		SetForegroundWindow(owner)
	}
	r, _, _ := procTrackPopupMenuEx.Call(m.handle,
		uintptr(tpmRightButton|tpmReturnCmd|tpmLeftAlign),
		uintptr(int32(x)), uintptr(int32(y)), owner, 0)
	if owner != 0 {
		PostMessage(owner, 0, 0, 0)
	}
	return MenuID(r)
}

// ---- Shell dialogs --------------------------------------------------------

type openFileName struct {
	StructSize      uint32
	Owner           uintptr
	Instance        uintptr
	Filter          *uint16
	CustomFilter    *uint16
	MaxCustomFilter uint32
	FilterIndex     uint32
	File            *uint16
	MaxFile         uint32
	FileTitle       *uint16
	MaxFileTitle    uint32
	InitialDir      *uint16
	Title           *uint16
	Flags           uint32
	FileOffset      uint16
	FileExtension   uint16
	DefExt          *uint16
	CustData        uintptr
	FnHook          uintptr
	TemplateName    *uint16
	ReservedPtr     uintptr
	Reserved        uint32
	FlagsEx         uint32
}

// OpenFileDialog shows the standard file picker and returns the selected path.
func OpenFileDialog(owner uintptr, title, filter, initialDir string) (string, bool) {
	buffer := make([]uint16, 1024)
	filterText := buildFilter(filter)
	filterPtr, _ := windows.UTF16PtrFromString(filterText)
	titlePtr, _ := windows.UTF16PtrFromString(title)
	dirPtr, _ := windows.UTF16PtrFromString(initialDir)

	ofn := openFileName{
		Owner:       owner,
		Filter:      filterPtr,
		FilterIndex: 1,
		File:        &buffer[0],
		MaxFile:     uint32(len(buffer)),
		InitialDir:  dirPtr,
		Title:       titlePtr,
		Flags:       0x00000008 | 0x00080000 | 0x00001000 | 0x00000800, // NOCHANGEDIR|EXPLORER|FILEMUSTEXIST|PATHMUSTEXIST
	}
	ofn.StructSize = uint32(unsafe.Sizeof(ofn))
	r, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return "", false
	}
	return windows.UTF16ToString(buffer), true
}

func buildFilter(filter string) string {
	// The translated filter uses Qt's "desc (*.ext);;desc2 (*.*)" form.
	parts := splitFilter(filter)
	out := ""
	for _, part := range parts {
		pattern := patternOf(part)
		out += part + "\x00" + pattern + "\x00"
	}
	if out == "" {
		out = "All files (*.*)\x00*.*\x00"
	}
	return out + "\x00"
}

func splitFilter(filter string) []string {
	var parts []string
	current := ""
	for i := 0; i < len(filter); i++ {
		if filter[i] == ';' && i+1 < len(filter) && filter[i+1] == ';' {
			if current != "" {
				parts = append(parts, current)
			}
			current = ""
			i++
			continue
		}
		current += string(filter[i])
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func patternOf(desc string) string {
	start := -1
	for i := 0; i < len(desc); i++ {
		if desc[i] == '(' {
			start = i + 1
		}
		if desc[i] == ')' && start > 0 {
			return desc[start:i]
		}
	}
	return "*.*"
}

type browseInfo struct {
	Owner       uintptr
	Root        uintptr
	DisplayName *uint16
	Title       *uint16
	Flags       uint32
	Callback    uintptr
	LParam      uintptr
	Image       int32
}

// BrowseFolder shows the shell folder picker.
func BrowseFolder(owner uintptr, title, initialDir string) (string, bool) {
	titlePtr, _ := windows.UTF16PtrFromString(title)
	_ = initialDir

	const bifReturnOnlyFSDirs = 0x0001
	const bifEditBox = 0x0010
	const bifNewDialogStyle = 0x0040
	bi := browseInfo{
		Owner: owner,
		Title: titlePtr,
		Flags: bifReturnOnlyFSDirs | bifEditBox | bifNewDialogStyle,
	}
	pidl, _, _ := procSHBrowseForFolder.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return "", false
	}
	defer procCoTaskMemFree.Call(pidl)

	buffer := make([]uint16, 1024)
	if ok, _, _ := procSHGetPathFromIDList.Call(pidl, uintptr(unsafe.Pointer(&buffer[0]))); ok == 0 {
		return "", false
	}
	return windows.UTF16ToString(buffer), true
}

// ShellOpen opens a path with the default handler.
func ShellOpen(path string) bool {
	verb, _ := windows.UTF16PtrFromString("open")
	target, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	r, _, _ := procShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(target)),
		0, 0, 1) // SW_SHOWNORMAL
	return r > 32
}

// ---- Tray icon ------------------------------------------------------------

// TrayIcon wraps a Shell_NotifyIcon entry.
type TrayIcon struct {
	hwnd     uintptr
	id       uint32
	callback uint32
	icon     uintptr
	tip      string
	visible  bool
}

const (
	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetVersion = 4

	notifyIconVersion4 = 4

	nifMessage = 0x0001
	nifIcon    = 0x0002
	nifTip     = 0x0004
	nifInfo    = 0x0010

	niifInfo    = 0x00000001
	niifWarning = 0x00000002
	niifError   = 0x00000003

	ninSelect       = 0x0400
	ninKeySelect    = 0x0401
	ninBalloonShow  = 0x0402
	ninBalloonHide  = 0x0403
	ninBalloonClick = 0x0405
)

type notifyIconData struct {
	CbSize            uint32
	HWnd              uintptr
	UID               uint32
	UFlags            uint32
	UCallbackMessage  uint32
	HIcon             uintptr
	SzTip             [128]uint16
	DwState           uint32
	DwStateMask       uint32
	SzInfo            [256]uint16
	UTimeoutOrVersion uint32
	SzInfoTitle       [64]uint16
	DwInfoFlags       uint32
	GuidItem          windows.GUID
	HBalloonIcon      uintptr
}

// NewTrayIcon registers a notification-area icon. id should be unique per
// process so a leftover icon from a force-killed instance cannot collide.
func NewTrayIcon(hwnd uintptr, id uint32, callbackMessage uint32, tip string, icon uintptr) (*TrayIcon, bool) {
	t := &TrayIcon{hwnd: hwnd, id: id, callback: callbackMessage, tip: tip, icon: icon}
	if !t.modify(nifMessage | nifIcon | nifTip) {
		return nil, false
	}
	t.visible = true
	t.setVersion()
	return t, true
}

// setVersion opts into NOTIFYICON_VERSION_4 message semantics.
func (t *TrayIcon) setVersion() {
	var data notifyIconData
	data.CbSize = uint32(unsafe.Sizeof(data))
	data.HWnd = t.hwnd
	data.UID = t.id
	data.UTimeoutOrVersion = notifyIconVersion4
	procShellNotifyIcon.Call(nimSetVersion, uintptr(unsafe.Pointer(&data)))
}

// ParseCallback extracts the notification message and icon id from a
// version-4 callback.
func ParseCallback(lparam uintptr) (message uint32, id uint32) {
	return uint32(lparam & 0xFFFF), uint32((lparam >> 16) & 0xFFFF)
}

// CallbackPoint extracts the cursor position from a version-4 callback.
func CallbackPoint(wparam uintptr) (int, int) {
	return int(int16(wparam & 0xFFFF)), int(int16((wparam >> 16) & 0xFFFF))
}

func (t *TrayIcon) modify(flags uint32) bool {
	var data notifyIconData
	data.CbSize = uint32(unsafe.Sizeof(data))
	data.HWnd = t.hwnd
	data.UID = t.id
	data.UFlags = flags
	data.UCallbackMessage = t.callback
	data.HIcon = t.icon
	copyString(data.SzTip[:], t.tip)
	action := uintptr(nimModify)
	if !t.visible {
		action = nimAdd
	}
	r, _, _ := procShellNotifyIcon.Call(action, uintptr(unsafe.Pointer(&data)))
	if r == 0 && action == nimAdd {
		return false
	}
	if action == nimAdd {
		t.visible = true
	}
	return r != 0
}

// SetIcon updates the tray icon.
func (t *TrayIcon) SetIcon(icon uintptr) {
	t.icon = icon
	t.modify(nifIcon)
}

// SetToolTip updates the hover text.
func (t *TrayIcon) SetToolTip(text string) {
	t.tip = text
	t.modify(nifTip)
}

// ShowBalloon displays a notification balloon.
func (t *TrayIcon) ShowBalloon(title, message string, kind uint32) {
	var data notifyIconData
	data.CbSize = uint32(unsafe.Sizeof(data))
	data.HWnd = t.hwnd
	data.UID = t.id
	data.UFlags = nifInfo
	copyString(data.SzInfoTitle[:], title)
	copyString(data.SzInfo[:], message)
	data.DwInfoFlags = kind
	data.UTimeoutOrVersion = 4000
	procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&data)))
}

// Remove deletes the icon from the notification area.
func (t *TrayIcon) Remove() {
	var data notifyIconData
	data.CbSize = uint32(unsafe.Sizeof(data))
	data.HWnd = t.hwnd
	data.UID = t.id
	procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
	t.visible = false
}

func copyString(dst []uint16, value string) {
	units := u16(value)
	if len(units) >= len(dst) {
		units = units[:len(dst)-1]
	}
	copy(dst, units)
}

// ---- Window helpers -------------------------------------------------------

// CreateTopLevel creates an overlapped window.
func CreateTopLevel(class, title string, x, y, w, h int, owner uintptr, exStyle, style uint32) (uintptr, error) {
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
		uintptr(int32(x)), uintptr(int32(y)), uintptr(int32(w)), uintptr(int32(h)),
		owner, 0, instance, 0,
	)
	if hwnd == 0 {
		return 0, callErr
	}
	return hwnd, nil
}

// SetIcon applies a window icon to the title bar and task switcher.
func SetIcon(hwnd, icon uintptr) {
	const wmSetIcon = 0x0080
	const iconSmall = 0
	const iconBig = 1
	SendMessage(hwnd, wmSetIcon, iconBig, icon)
	SendMessage(hwnd, wmSetIcon, iconSmall, icon)
}

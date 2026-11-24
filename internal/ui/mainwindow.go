package ui

import (
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/qepan0520/SysGauge-Go/internal/config"
	"github.com/qepan0520/SysGauge-Go/internal/i18n"
	"github.com/qepan0520/SysGauge-Go/internal/metrics"
	"github.com/qepan0520/SysGauge-Go/internal/sampler"
	"github.com/qepan0520/SysGauge-Go/internal/win32"
)

// AppName and Version are shown in the title bar and notifications.
const (
	AppName = "SysGauge"
	Version = "2.0.1"

	// WindowTitlePrefix 是主窗口标题的固定前缀（两版一致），供单实例探测
	// 判断已有窗口是否已经显示。
	WindowTitlePrefix = AppName + " v"
)

const (
	trayMessage = win32.WM_APP + 10
	// initTrayMessage defers Shell_NotifyIcon until the message loop runs.
	initTrayMessage = win32.WM_APP + 11
	// showMessage reveals the window once the message loop is running.
	showMessage = win32.WM_APP + 12

	cmdShow = 1000 + iota
	cmdStart
	cmdStop
	cmdOpenDir
	cmdHistory
	cmdQuit
)

var (
	mainClassOnce sync.Once
	mainClassName string
	mainByHWND    sync.Map

	// taskbarCreatedMessage 由 Explorer 重启后广播，用于重建托盘图标。
	taskbarCreatedMessage uint32
)

// MainWindow hosts the custom-drawn dashboard inside a native frame.
type MainWindow struct {
	hwnd   uintptr
	canvas uintptr
	dash   *Dash

	cfg     *config.Config
	sampler *sampler.Sampler
	events  chan sampler.Event

	tray       *win32.TrayIcon
	trayID     uint32
	icons      map[string]uintptr
	windowIcon uintptr

	history  *historyWindow
	quitting bool
	scale    float64
}

// NewMainWindow builds the frame, canvas and tray icon.
func NewMainWindow(cfg *config.Config, smp *sampler.Sampler) (*MainWindow, error) {
	mainClassOnce.Do(func() {
		name, err := win32.RegisterClass("SysGaugeMain", mainWindowProc)
		if err != nil {
			log.Printf("注册主窗口类失败: %v", err)
			return
		}
		mainClassName = name
	})
	if mainClassName == "" {
		return nil, fmt.Errorf("cannot register main window class")
	}

	taskbarCreatedMessage = win32.RegisterWindowMessage("TaskbarCreated")

	win32.OleInit()
	theme := LoadTheme()
	scale := float64(win32.SystemDPI()) / 96.0
	width := int(1020 * scale)
	height := int(700 * scale)
	x, y := win32.CenterWindow(width, height)
	if p := cfg.Placement; p != nil && p.W > 300 && p.H > 300 {
		x, y, width, height = p.X, p.Y, p.W, p.H
	}

	hwnd, err := win32.CreateTopLevel(mainClassName,
		i18n.Tr("window.title", "app", AppName, "version", Version),
		x, y, width, height, 0, 0, win32.WS_OVERLAPPEDWINDOW|win32.WS_CLIPCHILDREN)
	if err != nil {
		return nil, err
	}

	m := &MainWindow{
		hwnd:    hwnd,
		cfg:     cfg,
		sampler: smp,
		events:  make(chan sampler.Event, 2048),
		icons:   map[string]uintptr{},
		scale:   scale,
	}
	mainByHWND.Store(hwnd, m)

	win32.SetDarkTitleBar(hwnd, theme.Dark)
	win32.SetIcon(hwnd, m.appIcon())

	hooks := Hooks{
		Notify:           m.notify,
		Error:            m.showError,
		ChooseDir:        m.chooseDir,
		OpenDir:          m.openDataDir,
		OpenHistory:      m.openHistory,
		LanguageChanged:  m.retranslate,
		RecordingChanged: m.syncTrayActions,
		Sample:           m.updateTray,
		Activate:         m.ShowAndRaise,
	}
	canvas, dash, err := CreateCanvas(hwnd, cfg, smp, m.events, hooks)
	if err != nil {
		win32.DestroyWindow(hwnd)
		return nil, err
	}
	m.canvas = canvas
	m.dash = dash

	smp.SetEventHandler(func(event sampler.Event) {
		select {
		case m.events <- event:
		default:
		}
		if hwnd := m.canvas; hwnd != 0 {
			win32.PostMessage(hwnd, MessageSample, 0, 0)
		}
	})

	m.createTrayDeferred()
	// Reveal the window with the asynchronous variant: showing it inline can
	// deadlock against a shell that is still busy with a previous instance.
	win32.ShowWindowAsync(hwnd, win32.SW_SHOW)
	if p := cfg.Placement; p != nil && p.Maximized {
		win32.ShowWindowAsync(hwnd, win32.SW_MAXIMIZE)
	}
	log.Printf("主窗口已创建: hwnd=%d canvas=%d", hwnd, canvas)
	return m, nil
}

// Run pumps the message loop until the window closes.
func (m *MainWindow) Run() int {
	win32.PostMessage(m.hwnd, showMessage, 0, 0)
	code := win32.RunMessageLoop()
	log.Printf("消息循环结束: %d", code)
	return code
}

// ShowAndRaise restores the window from the tray.
func (m *MainWindow) ShowAndRaise() {
	win32.ShowWindow(m.hwnd, win32.SW_SHOW)
	win32.ShowWindow(m.hwnd, win32.SW_SHOW)
	if win32.IsIconic(m.hwnd) {
		win32.ShowWindow(m.hwnd, win32.SW_RESTORE)
	}
	win32.SetForegroundWindow(m.hwnd)
	win32.SetFocus(m.canvas)
}

// Quit ends the application.
func (m *MainWindow) Quit() {
	if m.quitting {
		return
	}
	m.quitting = true
	m.savePlacement()
	if m.tray != nil {
		m.tray.Remove()
		m.tray = nil
	}
	win32.DestroyWindow(m.hwnd)
}

// PrepareShutdown releases tray resources after the loop exits.
func (m *MainWindow) PrepareShutdown() {
	if m.tray != nil {
		m.tray.Remove()
		m.tray = nil
	}
}

// Dash exposes the dashboard (used for tray commands).
func (m *MainWindow) Dash() *Dash { return m.dash }

// Handle returns the native window handle.
func (m *MainWindow) Handle() uintptr { return m.hwnd }

// OpenHistoryFile opens the history viewer for a CSV passed on the command line.
func (m *MainWindow) OpenHistoryFile(path string) { m.openHistoryFile(path) }

// PostActivate asks the UI thread to bring the window to the front.
func (m *MainWindow) PostActivate() {
	if m.canvas != 0 {
		win32.PostMessage(m.canvas, MessageActivate, 0, 0)
	}
}

func mainWindowProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	value, ok := mainByHWND.Load(hwnd)
	if !ok {
		return win32.DefWindowProc(hwnd, msg, wparam, lparam)
	}
	m := value.(*MainWindow)

	// Explorer 重启后系统会广播 TaskbarCreated，旧的托盘图标已随之消失，
	// 必须重新添加，否则进程会变成一个没有任何入口的幽灵。
	if taskbarCreatedMessage != 0 && msg == taskbarCreatedMessage {
		m.tray = nil
		m.createTray()
		return 0
	}

	switch msg {
	case win32.WM_SIZE:
		m.resizeCanvas()
		return 0
	case win32.WM_GETMINMAXINFO:
		info := (*win32.MinMaxInfo)(win32.Ptr(lparam))
		info.PtMinTrackSize.X = int32(900 * m.scale)
		info.PtMinTrackSize.Y = int32(600 * m.scale)
		return 0
	case win32.WM_CLOSE:
		m.onClose()
		return 0
	case win32.WM_DESTROY:
		mainByHWND.Delete(hwnd)
		win32.PostQuitMessage(0)
		return 0
	case trayMessage:
		message, id := win32.ParseCallback(lparam)
		if id == m.trayID {
			m.onTrayMessage(message)
		}
		return 0
	case initTrayMessage:
		m.createTray()
		return 0
	case win32.WM_SETTINGCHANGE, win32.WM_THEMECHANGED:
		m.refreshTheme()
		return 0
	case showMessage:
		win32.ShowWindow(hwnd, win32.SW_SHOW)
		return 0
	}
	return win32.DefWindowProc(hwnd, msg, wparam, lparam)
}

// refreshTheme re-reads the Windows app theme so the window follows a live
// switch between dark and light mode.
func (m *MainWindow) refreshTheme() {
	theme := LoadTheme()
	if m.dash == nil || theme.Dark == m.dash.theme.Dark {
		return
	}
	m.dash.SetTheme(theme)
	win32.SetDarkTitleBar(m.hwnd, theme.Dark)
	win32.RedrawFrame(m.hwnd)
	if m.history != nil && m.history.hwnd != 0 {
		m.history.setTheme(theme)
	}
}

func (m *MainWindow) resizeCanvas() {
	w, h := win32.ClientRect(m.hwnd)
	if w <= 0 || h <= 0 {
		return
	}
	m.scale = float64(win32.DPIForWindow(m.hwnd)) / 96.0
	win32.SetWindowPos(m.canvas, 0, 0, int32(w), int32(h))
	if m.dash != nil {
		m.dash.UpdateDPI(win32.DPIForWindow(m.hwnd))
	}
}

func (m *MainWindow) onClose() {
	if !m.quitting && m.cfg.MinimizeToTray && m.tray != nil {
		win32.ShowWindow(m.hwnd, win32.SW_HIDE)
		m.notify(i18n.Tr("notify.background"), i18n.Tr("notify.background.body"))
		return
	}
	m.savePlacement()
	win32.DestroyWindow(m.hwnd)
}

func (m *MainWindow) savePlacement() {
	x, y, w, h := win32.WindowRect(m.hwnd)
	if w <= 0 || h <= 0 {
		return
	}
	m.cfg.Placement = &config.Placement{X: x, Y: y, W: w, H: h, Maximized: win32.IsZoomed(m.hwnd)}
	_ = m.cfg.Save()
}

func (m *MainWindow) createTrayDeferred() {
	win32.PostMessage(m.hwnd, initTrayMessage, 0, 0)
}

func (m *MainWindow) createTray() {
	icon := m.trayIcon("idle")
	m.trayID = uint32(os.Getpid())
	tray, ok := win32.NewTrayIcon(m.hwnd, m.trayID, trayMessage,
		i18n.Tr("tray.tooltip_idle", "app", AppName), icon)
	if !ok {
		log.Printf("系统托盘不可用")
		m.dash.SetTrayAvailable(false)
		return
	}
	m.tray = tray
	m.updateTray(nil)
	m.dash.SetTrayAvailable(true)
	log.Printf("托盘图标已创建: id=%d", m.trayID)
}

func (m *MainWindow) onTrayMessage(event uint32) {
	switch event {
	case win32.WM_LBUTTONUP, win32.WM_LBUTTONDBLCLK:
		m.ShowAndRaise()
	case win32.WM_RBUTTONUP, win32.WM_CONTEXTMENU:
		m.showTrayMenu()
	}
}
func (m *MainWindow) showTrayMenu() {
	menu := win32.NewMenu()
	if menu == nil {
		return
	}
	defer menu.Destroy()

	menu.AddString(cmdShow, i18n.Tr("tray.show"))
	menu.AddSeparator()
	menu.AddString(cmdStart, i18n.Tr("btn.start"))
	menu.AddString(cmdStop, i18n.Tr("btn.stop"))
	menu.AddSeparator()
	menu.AddString(cmdOpenDir, i18n.Tr("tray.open_dir"))
	menu.AddString(cmdHistory, i18n.Tr("tray.history"))
	menu.AddSeparator()
	menu.AddString(cmdQuit, i18n.Tr("tray.quit"))

	recording := m.dash.Recording()
	menu.SetEnabled(cmdStart, !recording)
	menu.SetEnabled(cmdStop, recording)

	pos := win32.CursorPos()
	command := menu.TrackPopup(m.hwnd, int(pos.X), int(pos.Y))
	switch int(command) {
	case cmdShow:
		m.ShowAndRaise()
	case cmdStart:
		m.dash.StartRecording()
		m.syncTrayActions()
	case cmdStop:
		m.dash.StopRecording()
		m.syncTrayActions()
	case cmdOpenDir:
		m.openDataDir()
	case cmdHistory:
		m.openHistory()
	case cmdQuit:
		m.Quit()
	}
}

func (m *MainWindow) syncTrayActions() {
	// The tray menu is rebuilt on every open, so only the icon colour needs to
	// follow the recording state here.
	if m.tray != nil {
		level := "idle"
		if m.dash.Recording() {
			level = "low"
		}
		m.tray.SetIcon(m.trayIcon(level))
	}
}

func (m *MainWindow) updateTray(sample *metrics.Sample) {
	if m.tray == nil {
		return
	}
	level := "idle"
	if sample != nil && sample.CPUPercent != nil {
		switch cpu := *sample.CPUPercent; {
		case cpu < 50:
			level = "low"
		case cpu < 80:
			level = "mid"
		default:
			level = "high"
		}
	}
	m.tray.SetIcon(m.trayIcon(level))

	cpuText := "--"
	memText := "--"
	state := i18n.Tr("tray.state_idle")
	if sample != nil {
		if sample.CPUPercent != nil {
			cpuText = fmt.Sprintf("%.0f%%", *sample.CPUPercent)
			state = i18n.Tr("tray.state_recording")
		}
		if sample.MemPercent != nil {
			memText = fmt.Sprintf("%.0f%%", *sample.MemPercent)
		}
	}
	m.tray.SetToolTip(i18n.Tr("tray.status", "app", AppName, "cpu", cpuText, "mem", memText, "state", state))
}

func (m *MainWindow) retranslate() {
	win32.SetWindowText(m.hwnd, i18n.Tr("window.title", "app", AppName, "version", Version))
	m.dash.Invalidate()
	if m.tray != nil {
		m.tray.SetToolTip(i18n.Tr("tray.status", "app", AppName, "cpu", "--", "mem", "--", "state", i18n.Tr("tray.state_idle")))
	}
}

func (m *MainWindow) notify(title, message string) {
	if m.tray != nil {
		m.tray.ShowBalloon(title, message, 0x00000001)
	}
}

func (m *MainWindow) showError(title, message string) {
	win32.MessageBox(m.hwnd, title, message, 0x00000010)
}

func (m *MainWindow) chooseDir() (string, bool) {
	return win32.BrowseFolder(m.hwnd, i18n.Tr("dialog.choose_dir"), m.cfg.SaveDir)
}

func (m *MainWindow) openDataDir() {
	path := m.cfg.SaveDir
	if err := os.MkdirAll(path, 0o755); err != nil {
		m.showError(i18n.Tr("error.dir_unavailable"), err.Error())
		return
	}
	if !win32.ShellOpen(path) {
		m.showError(i18n.Tr("error.dir_unavailable"), path)
	}
}

func (m *MainWindow) openHistory() {
	m.openHistoryFile("")
}

// openHistoryFile shows the history viewer, optionally preloading a CSV.
func (m *MainWindow) openHistoryFile(path string) {
	if m.history != nil && m.history.hwnd != 0 {
		win32.ShowWindow(m.history.hwnd, win32.SW_SHOW)
		if win32.IsIconic(m.history.hwnd) {
			win32.ShowWindow(m.history.hwnd, win32.SW_RESTORE)
		}
		win32.SetForegroundWindow(m.history.hwnd)
		return
	}
	m.history = newHistoryWindow(m.hwnd, m.cfg, m.dash.theme, m.appIcon(), path, func() {
		m.history = nil
	})
}

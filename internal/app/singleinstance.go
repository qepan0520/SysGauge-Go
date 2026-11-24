package app

import (
	"log"
	"os"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"
)

const (
	mutexName = `Local\SysGauge_SingleInstance_9f2c`
	eventName = `Local\SysGauge_Activate_9f2c`
	pipeName  = `\\.\pipe\SysGauge_SingleInstance_9f2c`

	// activationTimeout 限定新进程等待已有实例响应激活请求的最长时间。
	activationTimeout = 2 * time.Second
)

// Guard keeps the application single-instance and wakes the running window.
type Guard struct {
	mutex   windows.Handle
	event   windows.Handle
	stopped atomic.Bool

	// TookOver 为真表示启动时存在已有实例但它在 activationTimeout 内没有
	// 响应激活请求，本进程接管了启动。
	TookOver bool
}

// Acquire 获取单实例互斥体。
//
// 它也会探测 Python 版使用的命名管道，使 Go 版在旧版运行时去激活旧窗口，
// 而不是再启动一个记录器。
//
// isVisible 用于判断已有实例是否真的响应了激活请求（例如窗口重新显示）。
// 若在 activationTimeout 内始终没有响应，Acquire 会接管启动并标记
// TookOver，避免残留进程把所有后续启动都无声地吞掉。
func Acquire(isVisible func() bool) (*Guard, bool) {
	tookOver := false
	if signalLegacyInstance() {
		log.Printf("检测到 Python 版实例，已请求其激活窗口")
		if waitForWindow(isVisible) {
			log.Printf("Python 版实例已响应，本进程退出")
			return nil, false
		}
		log.Printf("Python 版实例未响应，继续启动本实例")
		tookOver = true
	}

	mutexPtr, _ := windows.UTF16PtrFromString(mutexName)
	handle, err := windows.CreateMutex(nil, false, mutexPtr)
	if err == windows.ERROR_ALREADY_EXISTS {
		signalEvent()
		if waitForWindow(isVisible) {
			windows.CloseHandle(handle)
			log.Printf("检测到已有实例，已激活其窗口，本进程退出")
			return nil, false
		}
		log.Printf("已有实例未响应，接管启动")
		return &Guard{mutex: handle, event: createEvent(), TookOver: true}, true
	}
	if err != nil {
		log.Printf("单实例互斥体创建失败，继续启动: %v", err)
		return &Guard{}, true
	}

	return &Guard{mutex: handle, event: createEvent(), TookOver: tookOver}, true
}

func createEvent() windows.Handle {
	eventPtr, _ := windows.UTF16PtrFromString(eventName)
	event, err := windows.CreateEvent(nil, 0, 0, eventPtr)
	if err != nil {
		log.Printf("激活事件创建失败: %v", err)
	}
	return event
}

// waitForWindow 轮询 isVisible，直到已有实例响应或超时。
// isVisible 为空时按“已响应”处理，保持原有的保守行为。
func waitForWindow(isVisible func() bool) bool {
	if isVisible == nil {
		return true
	}
	deadline := time.Now().Add(activationTimeout)
	for {
		if isVisible() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// WaitForActivation calls onActivate whenever another instance asks to show
// the window.
func (g *Guard) WaitForActivation(onActivate func()) {
	if g == nil || g.event == 0 {
		return
	}
	go func() {
		for {
			if _, err := windows.WaitForSingleObject(g.event, windows.INFINITE); err != nil {
				return
			}
			if g.stopped.Load() {
				return
			}
			onActivate()
		}
	}()
}

// Stop releases the mutex and unblocks the waiter.
func (g *Guard) Stop() {
	if g == nil {
		return
	}
	g.stopped.Store(true)
	if g.event != 0 {
		_ = windows.SetEvent(g.event)
		windows.CloseHandle(g.event)
		g.event = 0
	}
	if g.mutex != 0 {
		windows.CloseHandle(g.mutex)
		g.mutex = 0
	}
}

func signalLegacyInstance() bool {
	name, err := windows.UTF16PtrFromString(pipeName)
	if err != nil {
		return false
	}
	handle, err := windows.CreateFile(name,
		windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	file := os.NewFile(uintptr(handle), pipeName)
	if file != nil {
		_, _ = file.Write([]byte("ACTIVATE"))
	}
	return true
}

func signalEvent() {
	name, err := windows.UTF16PtrFromString(eventName)
	if err != nil {
		return
	}
	event, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		return
	}
	defer windows.CloseHandle(event)
	_ = windows.SetEvent(event)
}

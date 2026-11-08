package app

import (
	"log"
	"os"
	"sync/atomic"

	"golang.org/x/sys/windows"
)

const (
	mutexName = `Local\SysGauge_SingleInstance_9f2c`
	eventName = `Local\SysGauge_Activate_9f2c`
	pipeName  = `\\.\pipe\SysGauge_SingleInstance_9f2c`
)

// Guard keeps the application single-instance and wakes the running window.
type Guard struct {
	mutex   windows.Handle
	event   windows.Handle
	stopped atomic.Bool
}

// Acquire takes the single-instance mutex.
//
// It also tries the named pipe used by the Python edition, so launching the Go
// build while the old build is running activates the old window instead of
// starting a second recorder.
func Acquire() (*Guard, bool) {
	if signalLegacyInstance() {
		log.Printf("检测到 Python 版实例，已请求其激活窗口")
		return nil, false
	}

	mutexPtr, _ := windows.UTF16PtrFromString(mutexName)
	handle, err := windows.CreateMutex(nil, false, mutexPtr)
	if err == windows.ERROR_ALREADY_EXISTS {
		windows.CloseHandle(handle)
		signalEvent()
		return nil, false
	}
	if err != nil {
		log.Printf("单实例互斥体创建失败，继续启动: %v", err)
		return &Guard{}, true
	}

	eventPtr, _ := windows.UTF16PtrFromString(eventName)
	event, err := windows.CreateEvent(nil, 0, 0, eventPtr)
	if err != nil {
		log.Printf("激活事件创建失败: %v", err)
	}

	return &Guard{mutex: handle, event: event}, true
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

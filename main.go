// SysGauge is a lightweight CPU / memory recorder with a live chart and a
// crash-safe CSV log. This is the Go rewrite of the Python edition.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/qepan0520/SysGauge-Go/internal/app"
	"github.com/qepan0520/SysGauge-Go/internal/config"
	"github.com/qepan0520/SysGauge-Go/internal/i18n"
	"github.com/qepan0520/SysGauge-Go/internal/recorder"
	"github.com/qepan0520/SysGauge-Go/internal/sampler"
	"github.com/qepan0520/SysGauge-Go/internal/ui"
	"github.com/qepan0520/SysGauge-Go/internal/win32"
)

func main() {
	win32.EnableDPIAwareness()

	logPath := app.SetupLogging()
	defer app.CloseLogging()
	log.Printf("SysGauge %s 启动，日志: %s", ui.Version, logPath)

	guard, acquired := app.Acquire(func() bool {
		return win32.VisibleWindowWithTitlePrefix(ui.WindowTitlePrefix)
	})
	if !acquired {
		return
	}
	defer guard.Stop()
	if guard.TookOver {
		win32.MessageBox(0, ui.AppName, "检测到已有 SysGauge 实例但没有响应（可能残留在后台），已启动新实例。", 0x00000030)
	}

	cfg := config.Load()
	if cfg.Language == "" {
		cfg.Language = i18n.DetectSystemLanguage()
		_ = cfg.Save()
	}
	i18n.SetLanguage(cfg.Language)
	cfg.EnsureSaveDir()
	_ = cfg.Save()

	rec := recorder.New(cfg.SaveDir, cfg.RotateMB)
	smp := sampler.New(rec, cfg.IntervalSec)
	smp.SetFlags(cfg.EnableCores, cfg.EnableFreq, cfg.EnableSwap)

	window, err := ui.NewMainWindow(cfg, smp)
	if err != nil {
		log.Printf("创建主窗口失败: %v", err)
		win32.MessageBox(0, ui.AppName, err.Error(), 0x00000010)
		return
	}
	guard.WaitForActivation(window.PostActivate)
	if len(os.Args) > 1 {
		window.OpenHistoryFile(os.Args[1])
	}

	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("未捕获异常: %v", r)
				win32.MessageBox(window.Handle(), ui.AppName, fmt.Sprint(r), 0x00000010)
			}
		}()
		window.Run()
	}()

	smp.Stop()
	rec.Stop()
	window.PrepareShutdown()
	log.Printf("SysGauge 已退出")
}

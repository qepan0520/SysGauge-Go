// Package app contains process-level plumbing: logging and single instance.
package app

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/qepan0520/SysGauge-Go/internal/config"
)

const (
	maxLogBytes = 1024 * 1024
	keepLogs    = 3
)

type rotatingWriter struct {
	mu   sync.Mutex
	path string
	file *os.File
	size int64
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.size+int64(len(p)) > maxLogBytes {
		w.rotate()
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) open() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	w.file = file
	w.size = info.Size()
	return nil
}

func (w *rotatingWriter) rotate() {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	for i := keepLogs - 1; i >= 1; i-- {
		old := fmt.Sprintf("%s.%d", w.path, i)
		next := fmt.Sprintf("%s.%d", w.path, i+1)
		_ = os.Rename(old, next)
	}
	_ = os.Rename(w.path, w.path+".1")
	_ = w.open()
}

var logWriter *rotatingWriter

// SetupLogging installs a rotating log file under %APPDATA%\SysGauge\logs.
func SetupLogging() string {
	dir := config.LogDir()
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "sysgauge.log")

	logWriter = &rotatingWriter{path: path}
	log.SetOutput(logWriter)
	log.SetFlags(log.LstdFlags)
	return path
}

// CloseLogging flushes and closes the log file.
func CloseLogging() {
	if logWriter == nil {
		return
	}
	logWriter.mu.Lock()
	defer logWriter.mu.Unlock()
	if logWriter.file != nil {
		_ = logWriter.file.Close()
		logWriter.file = nil
	}
}

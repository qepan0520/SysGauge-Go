// Package recorder appends samples to a CSV file, flushing and fsyncing every
// row so a crash or power loss can only damage the last line.
package recorder

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/qepan0520/SysGauge-Go/internal/metrics"
)

// BytesPerMB is used for the rotation threshold.
const BytesPerMB = 1024 * 1024

// ErrNotOpen is returned when appending before Start.
var ErrNotOpen = errors.New("recorder is not open")

// Recorder writes MetricSample rows to disk.
type Recorder struct {
	saveDir    string
	rotateMB   int
	file       *os.File
	path       string
	part       int
	rowCount   int
	bytes      int64
	sessionTag string
}

// New creates a recorder; Start does the actual file work.
func New(saveDir string, rotateMB int) *Recorder {
	if rotateMB < 0 {
		rotateMB = 0
	}
	return &Recorder{saveDir: saveDir, rotateMB: rotateMB}
}

// SetRotateMB updates the rotation threshold (0 disables rotation).
func (r *Recorder) SetRotateMB(mb int) {
	if mb < 0 {
		mb = 0
	}
	r.rotateMB = mb
}

// SetSaveDir updates the folder used by the next Start call.
func (r *Recorder) SetSaveDir(dir string) {
	if dir != "" {
		r.saveDir = dir
	}
}

// CurrentPath returns the active file, or "" when closed.
func (r *Recorder) CurrentPath() string { return r.path }

// RowCount returns how many rows were written in this session.
func (r *Recorder) RowCount() int { return r.rowCount }

// BytesWritten returns the size of the active file.
func (r *Recorder) BytesWritten() int64 { return r.bytes }

// Start opens a new session file and writes the header when needed.
func (r *Recorder) Start(saveDir string) (string, error) {
	if r.file != nil {
		r.Stop()
	}
	if saveDir != "" {
		r.saveDir = saveDir
	}
	if err := os.MkdirAll(r.saveDir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create data folder %s: %w", r.saveDir, err)
	}
	r.sessionTag = time.Now().Format("20060102_150405")
	r.part = 1
	r.rowCount = 0
	r.bytes = 0
	if err := r.openFile(); err != nil {
		return "", err
	}
	return r.path, nil
}

// Stop flushes and closes the file, returning the last path used.
func (r *Recorder) Stop() string {
	path := r.path
	if r.file != nil {
		_ = r.file.Sync()
		_ = r.file.Close()
		r.file = nil
	}
	return path
}

func (r *Recorder) openFile() error {
	name := fmt.Sprintf("sysgauge_%s.csv", r.sessionTag)
	if r.part > 1 {
		name = fmt.Sprintf("sysgauge_%s_part%d.csv", r.sessionTag, r.part)
	}
	path := filepath.Join(r.saveDir, name)

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("cannot write file %s: %w", path, err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return fmt.Errorf("cannot stat file %s: %w", path, err)
	}
	r.file = file
	r.path = path
	r.bytes = info.Size()

	if info.Size() == 0 {
		line := strings.Join(metrics.Columns, ",") + "\n"
		if _, err := file.WriteString(line); err != nil {
			file.Close()
			r.file = nil
			return fmt.Errorf("cannot write header %s: %w", path, err)
		}
		if err := file.Sync(); err != nil {
			file.Close()
			r.file = nil
			return fmt.Errorf("cannot flush header %s: %w", path, err)
		}
		r.bytes = int64(len(line))
	}
	return nil
}

// Append writes a row and immediately fsyncs it.
func (r *Recorder) Append(sample *metrics.Sample) error {
	if r.file == nil {
		return ErrNotOpen
	}
	if r.rotateMB > 0 && r.bytes >= int64(r.rotateMB)*BytesPerMB {
		_ = r.file.Sync()
		_ = r.file.Close()
		r.file = nil
		r.part++
		if err := r.openFile(); err != nil {
			return err
		}
	}

	line := strings.Join(sample.Row(), ",") + "\n"
	if _, err := r.file.WriteString(line); err != nil {
		return fmt.Errorf("write failed (disk full?): %s: %w", r.path, err)
	}
	if err := r.file.Sync(); err != nil {
		return fmt.Errorf("flush failed (disk full?): %s: %w", r.path, err)
	}
	r.bytes += int64(len(line))
	r.rowCount++
	return nil
}

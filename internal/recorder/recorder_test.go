package recorder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qepan0520/SysGauge-Go/internal/metrics"
)

func sample(cpu, mem float64) *metrics.Sample {
	used := uint64(8 * 1024 * 1024 * 1024)
	total := uint64(16 * 1024 * 1024 * 1024)
	return &metrics.Sample{
		TimeISO:    "2026-11-08T20:00:00.000",
		TimeUnixMs: 1794140000000,
		CPUPercent: &cpu,
		MemPercent: &mem,
		MemUsed:    &used,
		MemTotal:   &total,
	}
}

func TestRecorderWritesHeaderAndRows(t *testing.T) {
	dir := t.TempDir()
	rec := New(dir, 100)
	path, err := rec.Start(dir)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("file %q is not inside %q", path, dir)
	}
	if !strings.HasSuffix(path, ".csv") {
		t.Fatalf("file %q does not end with .csv", path)
	}

	if err := rec.Append(sample(12.34, 56.78)); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := rec.Append(sample(50.0, 60.0)); err != nil {
		t.Fatalf("append: %v", err)
	}
	if rec.RowCount() != 2 {
		t.Fatalf("row count = %d, want 2", rec.RowCount())
	}
	if rec.BytesWritten() == 0 {
		t.Fatal("byte counter was not updated")
	}

	stopped := rec.Stop()
	if stopped != path {
		t.Fatalf("stop returned %q, want %q", stopped, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("file has %d lines, want 3", len(lines))
	}
	if lines[0] != strings.Join(metrics.Columns, ",") {
		t.Fatalf("unexpected header: %q", lines[0])
	}
	want := "2026-11-08T20:00:00.000,1794140000000,12.3,,,56.8,8589934592,,17179869184,,,"
	if lines[1] != want {
		t.Fatalf("row = %q, want %q", lines[1], want)
	}
}

func TestRecorderRejectsAppendWithoutStart(t *testing.T) {
	rec := New(t.TempDir(), 100)
	if err := rec.Append(sample(1, 2)); err != ErrNotOpen {
		t.Fatalf("err = %v, want ErrNotOpen", err)
	}
}

func TestRecorderRotatesFiles(t *testing.T) {
	dir := t.TempDir()
	rec := New(dir, 1)
	path, err := rec.Start(dir)
	if err != nil {
		t.Fatal(err)
	}

	// A row with many cores is large enough to reach the 1 MB threshold quickly.
	cores := make([]float64, 512)
	for i := range cores {
		cores[i] = 100
	}
	big := sample(99.9, 50)
	big.Cores = cores

	rotated := false
	for i := 0; i < 400; i++ {
		if err := rec.Append(big); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		if rec.CurrentPath() != path {
			rotated = true
			break
		}
	}
	if !rotated {
		t.Fatal("file was never rotated")
	}
	if !strings.Contains(rec.CurrentPath(), "_part2") {
		t.Fatalf("rotated file %q lacks the part suffix", rec.CurrentPath())
	}
	rec.Stop()

	matches, err := filepath.Glob(filepath.Join(dir, "sysgauge_*_part2.csv"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("rotated file missing on disk: %v %v", matches, err)
	}
}

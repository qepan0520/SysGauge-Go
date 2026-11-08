package history

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

const header = "timestamp_iso,timestamp_unix_ms,cpu_percent,cpu_percent_cores,cpu_freq_mhz,mem_percent,mem_used_bytes,mem_available_bytes,mem_total_bytes,swap_percent,swap_used_bytes,swap_total_bytes"

func writeCSV(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data.csv")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseSkipsBrokenRows(t *testing.T) {
	body := header + "\n" +
		"2026-11-08T20:00:00.000,1794140000000,10.0,,2000.0,50.0,1024,2048,4096,,,\n" +
		"2026-11-08T20:00:01.000,1794140001000,20.5,,2100.0,51.5,1100,2048,4096,,,\n" +
		"2026-11-08T20:00:02.000,1794140002000,,,2200.0,,1200,2048,4096,,,\n" +
		"2026-11-08T20:00:03.000,1794140003000,30.0\n" + // truncated last line
		"not-a-timestamp,xx,1.0,,1.0,1.0,1,1,1,,,\n" +
		"\n"

	data, err := Parse(writeCSV(t, body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if data.Count != 3 {
		t.Fatalf("count = %d, want 3", data.Count)
	}
	if data.Skipped != 2 {
		t.Fatalf("skipped = %d, want 2", data.Skipped)
	}
	if data.CPUCount != 2 {
		t.Fatalf("cpu count = %d, want 2", data.CPUCount)
	}
	if math.Abs(data.CPUMax-20.5) > 1e-9 || math.Abs(data.CPUMin-10) > 1e-9 {
		t.Fatalf("cpu min/max = %v/%v", data.CPUMin, data.CPUMax)
	}
	if math.Abs(data.CPUMean-15.25) > 1e-9 {
		t.Fatalf("cpu mean = %v", data.CPUMean)
	}
	if data.MemCount != 2 {
		t.Fatalf("mem count = %d, want 2", data.MemCount)
	}
	if math.IsNaN(data.Mem[2]) == false {
		t.Fatal("missing memory cell should be NaN")
	}
	if math.IsNaN(data.CPU[2]) == false {
		t.Fatal("missing cpu cell should be NaN")
	}
	if data.MemTotalGB <= 0 {
		t.Fatalf("mem total GB = %v", data.MemTotalGB)
	}
	if data.Duration.Seconds() != 2 {
		t.Fatalf("duration = %v, want 2s", data.Duration)
	}
	if data.Start.IsZero() || data.End.IsZero() {
		t.Fatal("start/end time were not set")
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse(writeCSV(t, "")); err != ErrEmpty {
		t.Fatalf("empty file err = %v, want ErrEmpty", err)
	}
	if _, err := Parse(writeCSV(t, "a,b,c\n1,2,3\n")); err != ErrNoTimestamp {
		t.Fatalf("missing timestamp err = %v, want ErrNoTimestamp", err)
	}
	if _, err := Parse(filepath.Join(t.TempDir(), "missing.csv")); err == nil {
		t.Fatal("missing file must return an error")
	}
}

func TestParseAcceptsBOMAndISOFallback(t *testing.T) {
	body := "\ufefftimestamp_iso,cpu_percent,mem_percent,mem_used_bytes,mem_total_bytes\n" +
		"2026-11-08T20:00:00.000,10.0,20.0,100,200\n" +
		"2026-11-08T20:00:01.500,12.0,21.0,110,200\n"
	data, err := Parse(writeCSV(t, body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if data.Count != 2 {
		t.Fatalf("count = %d, want 2", data.Count)
	}
	if math.Abs(data.Times[1]-data.Times[0]-1.5) > 1e-6 {
		t.Fatalf("timestamp delta = %v, want 1.5", data.Times[1]-data.Times[0])
	}
}

package metrics

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

var stampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}$`)

func TestCollectAllMetrics(t *testing.T) {
	collector := NewCollector()
	collector.WarmUp()
	time.Sleep(120 * time.Millisecond)

	sample := collector.Collect(true, true, true)
	if sample == nil {
		t.Fatal("sample is nil")
	}
	if !stampPattern.MatchString(sample.TimeISO) {
		t.Fatalf("timestamp %q does not match yyyy-mm-ddThh:mm:ss.mmm", sample.TimeISO)
	}
	if sample.TimeUnixMs <= 0 {
		t.Fatalf("unix ms = %d", sample.TimeUnixMs)
	}
	if sample.CPUPercent == nil || *sample.CPUPercent < 0 || *sample.CPUPercent > 100 {
		t.Fatalf("cpu percent = %v", sample.CPUPercent)
	}
	if len(sample.Cores) == 0 {
		t.Fatal("per-core values are missing")
	}
	for _, value := range sample.Cores {
		if value < 0 || value > 100 {
			t.Fatalf("core value out of range: %v", value)
		}
	}
	if sample.MemPercent == nil || sample.MemTotal == nil || *sample.MemTotal == 0 {
		t.Fatalf("memory values missing: %+v", sample)
	}
	if *sample.MemUsed > *sample.MemTotal {
		t.Fatal("used memory exceeds total memory")
	}
	if sample.CPUFreqMHz != nil && *sample.CPUFreqMHz <= 0 {
		t.Fatalf("cpu frequency = %v", *sample.CPUFreqMHz)
	}

	row := sample.Row()
	if len(row) != len(Columns) {
		t.Fatalf("row has %d columns, want %d", len(row), len(Columns))
	}
	if row[0] != sample.TimeISO {
		t.Fatal("first column must be the ISO timestamp")
	}
	if row[1] != strconv.FormatInt(sample.TimeUnixMs, 10) {
		t.Fatal("second column must be the unix timestamp")
	}
	if !strings.Contains(row[3], CoresSeparator) {
		t.Fatalf("core column %q is not separated by %q", row[3], CoresSeparator)
	}
}

func TestCollectOptionalFieldsEmpty(t *testing.T) {
	collector := NewCollector()
	collector.WarmUp()
	time.Sleep(60 * time.Millisecond)

	sample := collector.Collect(false, false, false)
	row := sample.Row()
	if row[3] != "" {
		t.Fatalf("core column = %q, want empty", row[3])
	}
	if row[4] != "" {
		t.Fatalf("frequency column = %q, want empty", row[4])
	}
	if row[9] != "" || row[10] != "" || row[11] != "" {
		t.Fatalf("swap columns must be empty: %v", row[9:])
	}
}

func TestSamplesAreUsableImmediately(t *testing.T) {
	collector := NewCollector()
	sample := collector.Collect(false, false, false)
	if sample == nil || sample.TimeUnixMs == 0 {
		t.Fatal("first sample must already carry a timestamp")
	}
	if sample.CPUPercent != nil && math.IsNaN(*sample.CPUPercent) {
		t.Fatal("cpu percent must not be NaN")
	}
}

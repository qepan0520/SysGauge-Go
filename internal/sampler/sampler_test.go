package sampler

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qepan0520/SysGauge-Go/internal/recorder"
)

func TestSamplerWritesRowsAndEmitsEvents(t *testing.T) {
	dir := t.TempDir()
	rec := recorder.New(dir, 100)
	smp := New(rec, 0.1)
	smp.SetFlags(false, true, false)

	var mu sync.Mutex
	kinds := map[EventKind]int{}
	smp.SetEventHandler(func(event Event) {
		mu.Lock()
		kinds[event.Kind]++
		mu.Unlock()
	})

	path, err := smp.Start(dir)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !smp.Recording() {
		t.Fatal("sampler should be recording")
	}
	time.Sleep(450 * time.Millisecond)
	smp.Stop()

	mu.Lock()
	samples := kinds[EventSample]
	started := kinds[EventStarted]
	stopped := kinds[EventStopped]
	mu.Unlock()

	if started != 1 || stopped != 1 {
		t.Fatalf("started=%d stopped=%d, want 1/1", started, stopped)
	}
	if samples < 3 {
		t.Fatalf("collected %d samples in 450ms at 100ms interval", samples)
	}
	if smp.RowCount() != samples {
		t.Fatalf("row count %d != sample events %d", smp.RowCount(), samples)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines)-1 != samples {
		t.Fatalf("file has %d rows, want %d", len(lines)-1, samples)
	}
}

func TestSamplerIntervalIsClamped(t *testing.T) {
	rec := recorder.New(t.TempDir(), 0)
	smp := New(rec, 5)
	smp.SetInterval(0)
	if smp.Interval() != 0.1 {
		t.Fatalf("interval = %v, want 0.1", smp.Interval())
	}
	smp.SetInterval(120)
	if smp.Interval() != 60 {
		t.Fatalf("interval = %v, want 60", smp.Interval())
	}
}

func TestStopWithoutStartIsSafe(t *testing.T) {
	rec := recorder.New(t.TempDir(), 0)
	smp := New(rec, 1)
	if path := smp.Stop(); path != "" {
		t.Fatalf("stop returned %q, want empty", path)
	}
}

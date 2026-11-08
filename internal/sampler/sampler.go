// Package sampler drives the recording loop.
//
// The loop schedules against absolute deadlines (next += interval) instead of
// sleeping for a fixed duration, so a long session never accumulates drift.
package sampler

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qepan0520/SysGauge-Go/internal/metrics"
	"github.com/qepan0520/SysGauge-Go/internal/recorder"
)

// EventKind identifies what happened inside the sampler.
type EventKind int

const (
	// EventSample fires after a row has been written.
	EventSample EventKind = iota
	// EventStarted fires when a session begins.
	EventStarted
	// EventStopped fires after the sampler has stopped.
	EventStopped
	// EventRotated fires when the CSV file is rotated.
	EventRotated
	// EventWriteError fires when writing fails and recording stops.
	EventWriteError
)

// Event is delivered on the sampler goroutine.
type Event struct {
	Kind   EventKind
	Sample *metrics.Sample
	Path   string
	Rows   int
	Err    error
}

// Sampler owns the recording goroutine and the CSV file.
type Sampler struct {
	collector *metrics.Collector
	recorder  *recorder.Recorder

	intervalBits atomic.Uint64
	enableCores  atomic.Bool
	enableFreq   atomic.Bool
	enableSwap   atomic.Bool

	mu      sync.Mutex
	active  bool
	stopCh  chan struct{}
	doneCh  chan struct{}
	onEvent func(Event)
}

// New creates a sampler bound to the given recorder.
func New(rec *recorder.Recorder, interval float64) *Sampler {
	s := &Sampler{
		collector: metrics.NewCollector(),
		recorder:  rec,
	}
	s.SetInterval(interval)
	s.enableFreq.Store(true)
	return s
}

// SetEventHandler installs the callback invoked from the sampler goroutine.
func (s *Sampler) SetEventHandler(handler func(Event)) { s.onEvent = handler }

// SetInterval updates the sampling interval; it applies to the next tick.
func (s *Sampler) SetInterval(seconds float64) {
	if seconds < 0.1 {
		seconds = 0.1
	}
	if seconds > 60 {
		seconds = 60
	}
	s.intervalBits.Store(math.Float64bits(seconds))
}

// Interval returns the active interval in seconds.
func (s *Sampler) Interval() float64 { return math.Float64frombits(s.intervalBits.Load()) }

// SetFlags selects which optional metrics are collected.
func (s *Sampler) SetFlags(cores, freq, swap bool) {
	s.enableCores.Store(cores)
	s.enableFreq.Store(freq)
	s.enableSwap.Store(swap)
}

// Recording reports whether a session is active.
func (s *Sampler) Recording() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// RowCount returns the number of rows written in the current session.
func (s *Sampler) RowCount() int { return s.recorder.RowCount() }

// DataPath returns the active CSV file.
func (s *Sampler) DataPath() string { return s.recorder.CurrentPath() }

// Start opens a new file and begins sampling.
func (s *Sampler) Start(saveDir string) (string, error) {
	s.mu.Lock()
	if s.active {
		path := s.recorder.CurrentPath()
		s.mu.Unlock()
		return path, nil
	}
	s.mu.Unlock()

	path, err := s.recorder.Start(saveDir)
	if err != nil {
		return "", err
	}
	s.collector.WarmUp()

	stop := make(chan struct{})
	done := make(chan struct{})
	s.mu.Lock()
	s.stopCh = stop
	s.doneCh = done
	s.active = true
	s.mu.Unlock()

	go s.loop(stop, done)
	s.emit(Event{Kind: EventStarted, Path: path})
	return path, nil
}

// Stop ends the session and waits for the file to be flushed.
func (s *Sampler) Stop() string {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return s.recorder.CurrentPath()
	}
	stop := s.stopCh
	done := s.doneCh
	s.active = false
	s.mu.Unlock()

	if stop != nil {
		close(stop)
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
	path := s.recorder.Stop()
	s.emit(Event{Kind: EventStopped, Path: path, Rows: s.recorder.RowCount()})
	return path
}

func (s *Sampler) loop(stop chan struct{}, done chan struct{}) {
	defer close(done)
	lastPath := s.recorder.CurrentPath()
	next := time.Now()

	for {
		select {
		case <-stop:
			return
		default:
		}

		sample := s.collector.Collect(
			s.enableCores.Load(),
			s.enableFreq.Load(),
			s.enableSwap.Load(),
		)
		if err := s.recorder.Append(sample); err != nil {
			s.emit(Event{Kind: EventWriteError, Err: err, Path: s.recorder.CurrentPath()})
			return
		}
		if current := s.recorder.CurrentPath(); current != lastPath {
			lastPath = current
			s.emit(Event{Kind: EventRotated, Path: current})
		}
		s.emit(Event{Kind: EventSample, Sample: sample, Rows: s.recorder.RowCount()})

		interval := s.Interval()
		next = next.Add(time.Duration(interval * float64(time.Second)))
		wait := time.Until(next)
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-stop:
				timer.Stop()
				return
			case <-timer.C:
			}
			continue
		}
		// Far behind schedule (sleep/resume, blocked disk): resync instead of
		// burst-catching up.
		if wait < -2*time.Duration(interval*float64(time.Second)) {
			next = time.Now()
		}
	}
}

func (s *Sampler) emit(event Event) {
	if s.onEvent != nil {
		s.onEvent(event)
	}
}

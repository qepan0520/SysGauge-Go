package ui

import (
	"math"
	"testing"
	"time"
)

func f64(v float64) *float64 { return &v }

func TestAppendFirstPointDoesNotPanicAndFollows(t *testing.T) {
	chart := NewChart(false)
	chart.SetWindow(60)
	chart.Append(1000, f64(10), f64(20))

	if len(chart.X) != 1 || chart.X[0] != 0 {
		t.Fatalf("X = %v, want [0]", chart.X)
	}
	if chart.ViewMax != 0 || chart.ViewMin != 0 {
		t.Fatalf("first view range = %v..%v, want 0..0", chart.ViewMin, chart.ViewMax)
	}

	chart.Append(1030, f64(30), nil)
	if chart.X[1] != 30 {
		t.Fatalf("second X = %v, want 30", chart.X[1])
	}
	if !math.IsNaN(chart.Mem[1]) {
		t.Fatal("nil memory must become NaN")
	}
	if chart.ViewMax != 30 {
		t.Fatalf("ViewMax = %v, want 30", chart.ViewMax)
	}
	if chart.ViewMin < 0 {
		t.Fatalf("ViewMin = %v, must not be negative while following", chart.ViewMin)
	}
}

func TestClearingResetsTheTimeline(t *testing.T) {
	chart := NewChart(false)
	chart.Append(5000, f64(1), f64(2))
	chart.Clear()
	chart.Append(9000, f64(3), f64(4))
	if chart.X[0] != 0 {
		t.Fatalf("first point after clear = %v, want 0", chart.X[0])
	}
}

func TestZoomAndPanKeepFollowingState(t *testing.T) {
	chart := NewChart(false)
	chart.SetWindow(60)
	for i := 0; i < 120; i++ {
		chart.Append(1000+float64(i), f64(float64(i%50)), f64(40))
	}
	last := chart.X[len(chart.X)-1]

	span := chart.ViewMax - chart.ViewMin
	pivot := chart.ViewMin + span/2
	chart.Zoom(0.5, pivot)
	if newSpan := chart.ViewMax - chart.ViewMin; newSpan >= span {
		t.Fatalf("zoom in did not narrow the range: %v -> %v", span, newSpan)
	}
	if chart.Follow {
		t.Fatal("zooming into the middle must stop following the right edge")
	}

	before := chart.ViewMin
	chart.PanPixels(200, 600)
	if chart.ViewMin == before {
		t.Fatal("pan had no effect")
	}

	chart.ResetView()
	if !chart.Follow {
		t.Fatal("reset must resume following")
	}
	if chart.ViewMax != last {
		t.Fatalf("reset ViewMax = %v, want %v", chart.ViewMax, last)
	}
}

func TestSetHoverFindsNearestPoint(t *testing.T) {
	chart := NewChart(false)
	for i := 0; i < 10; i++ {
		chart.Append(1000+float64(i), f64(float64(i)), f64(float64(i)))
	}
	if !chart.SetHover(4.4) {
		t.Fatal("hover should find a point")
	}
	if chart.HoverIdx != 4 {
		t.Fatalf("hover index = %d, want 4", chart.HoverIdx)
	}
	if chart.SetHover(-10) {
		t.Fatal("hover outside the data range must return false")
	}
	if chart.Hovering {
		t.Fatal("Hovering must be cleared outside the data range")
	}
}

func TestBuildPathsHandlesGapsAndDecimation(t *testing.T) {
	x := make([]float64, 0, 4000)
	cpu := make([]float64, 0, 4000)
	for i := 0; i < 4000; i++ {
		x = append(x, float64(i))
		if i >= 1000 && i < 1100 {
			cpu = append(cpu, math.NaN())
			continue
		}
		cpu = append(cpu, float64(i%100))
	}
	plot := Rect{X: 0, Y: 0, W: 400, H: 200}

	paths := buildPaths(x, cpu, 0, 3999, plot, 0, 100)
	if len(paths) < 2 {
		t.Fatalf("expected the NaN gap to split the line, got %d paths", len(paths))
	}
	points := 0
	for _, path := range paths {
		points += len(path)
		if len(path) > plot.W*8 {
			t.Fatalf("decimation kept too many points: %d", len(path))
		}
	}
	if points == 0 {
		t.Fatal("no points were produced")
	}
}

func TestAutoScaleRangeCoversVisibleData(t *testing.T) {
	chart := NewChart(false)
	chart.SetWindow(10)
	chart.SetAutoScale(true)
	for i := 0; i < 30; i++ {
		chart.Append(1000+float64(i), f64(float64(40+i%10)), f64(50))
	}
	chart.updateRange(Rect{X: 0, Y: 0, W: 400, H: 200})
	if !chart.showLowHigh {
		t.Fatal("auto scale markers are missing")
	}
	if chart.yMin >= chart.lowVal || chart.yMax <= chart.highVal {
		t.Fatalf("range %v..%v does not contain %v..%v", chart.yMin, chart.yMax, chart.lowVal, chart.highVal)
	}
	if chart.yMin < 0 || chart.yMax > 100 {
		t.Fatalf("auto range escaped 0..100: %v..%v", chart.yMin, chart.yMax)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		duration time.Duration
		want     string
	}{
		{5 * time.Second, "5 s"},
		{65 * time.Second, "1 min 5 s"},
		{3665 * time.Second, "1 h 1 min 5 s"},
	}
	for _, item := range cases {
		if got := formatDuration(item.duration); got != item.want {
			t.Fatalf("formatDuration(%v) = %q, want %q", item.duration, got, item.want)
		}
	}
}

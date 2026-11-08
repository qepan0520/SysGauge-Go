package win32

import "testing"

func TestBuildFilterConvertsQtStyle(t *testing.T) {
	filter := "CSV files (*.csv);;All files (*)"
	got := buildFilter(filter)
	want := "CSV files (*.csv)\x00*.csv\x00All files (*)\x00*\x00\x00"
	if got != want {
		t.Fatalf("buildFilter = %q, want %q", got, want)
	}
}

func TestBuildFilterFallback(t *testing.T) {
	got := buildFilter("")
	if got != "All files (*.*)\x00*.*\x00\x00" {
		t.Fatalf("fallback = %q", got)
	}
}

func TestSplitFilter(t *testing.T) {
	parts := splitFilter("CSV (*.csv);;Every file (*)")
	if len(parts) != 2 {
		t.Fatalf("parts = %v", parts)
	}
	if parts[0] != "CSV (*.csv)" || parts[1] != "Every file (*)" {
		t.Fatalf("unexpected parts: %v", parts)
	}
}

func TestPatternOf(t *testing.T) {
	cases := map[string]string{
		"CSV files (*.csv)": "*.csv",
		"All files (*)":     "*",
		"no pattern":        "*.*",
	}
	for input, want := range cases {
		if got := patternOf(input); got != want {
			t.Fatalf("patternOf(%q) = %q, want %q", input, got, want)
		}
	}
}

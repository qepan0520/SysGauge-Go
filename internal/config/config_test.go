package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCreatesDefaults(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	cfg := Load()
	if cfg.IntervalSec != 1.0 {
		t.Fatalf("interval = %v, want 1.0", cfg.IntervalSec)
	}
	if !cfg.EnableFreq || cfg.EnableCores || cfg.EnableSwap {
		t.Fatalf("unexpected metric defaults: %+v", cfg)
	}
	if cfg.RotateMB != 100 {
		t.Fatalf("rotate = %d, want 100", cfg.RotateMB)
	}
	if _, err := os.Stat(ConfigPath()); err != nil {
		t.Fatalf("config file was not created: %v", err)
	}
}

func TestLoadClampsAndKeepsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	if err := os.MkdirAll(filepath.Join(dir, AppDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{
	  "interval_sec": 999,
	  "rotate_mb": -8,
	  "enable_cores": true,
	  "save_dir": "  ",
	  "language": "zh-CN",
	  "window_geometry": "cHl0aG9uLWdlb21ldHJ5",
	  "legacy_flag": 42
	}`
	if err := os.WriteFile(ConfigPath(), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Load()
	if cfg.IntervalSec != MaxInterval {
		t.Fatalf("interval = %v, want %v", cfg.IntervalSec, MaxInterval)
	}
	if cfg.RotateMB != MinRotateMB {
		t.Fatalf("rotate = %d, want %d", cfg.RotateMB, MinRotateMB)
	}
	if !cfg.EnableCores {
		t.Fatal("enable_cores was not read")
	}
	if cfg.SaveDir != DefaultSaveDir() {
		t.Fatalf("save dir = %q, want default", cfg.SaveDir)
	}
	if cfg.Language != "zh-CN" {
		t.Fatalf("language = %q, want unchanged raw value", cfg.Language)
	}

	cfg.Placement = &Placement{X: 10, Y: 20, W: 900, H: 600}
	if err := cfg.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]json.RawMessage
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if string(saved["window_geometry"]) != `"cHl0aG9uLWdlb21ldHJ5"` {
		t.Fatalf("window_geometry was not preserved: %s", saved["window_geometry"])
	}
	if string(saved["legacy_flag"]) != "42" {
		t.Fatalf("unknown key was not preserved: %s", saved["legacy_flag"])
	}
	if _, ok := saved["window_placement"]; !ok {
		t.Fatal("window_placement was not written")
	}
}

func TestIsWritableDir(t *testing.T) {
	dir := t.TempDir()
	if !IsWritableDir(filepath.Join(dir, "data")) {
		t.Fatal("expected writable dir")
	}
	if IsWritableDir("") {
		t.Fatal("empty path must not be writable")
	}
}

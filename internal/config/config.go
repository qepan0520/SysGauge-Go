// Package config reads and writes %APPDATA%\SysGauge\config.json.
//
// The JSON layout is byte-compatible with the Python edition: the same keys,
// the same clamping rules, and unknown keys are preserved so switching between
// the two implementations never loses settings.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	// AppDirName is the folder used under %APPDATA% and Documents.
	AppDirName = "SysGauge"

	// MinInterval / MaxInterval bound the sampling interval in seconds.
	MinInterval = 0.1
	MaxInterval = 60.0

	// MinRotateMB / MaxRotateMB bound the CSV rotation threshold.
	MinRotateMB = 0
	MaxRotateMB = 10240
)

// Placement remembers the main window position.
type Placement struct {
	X         int  `json:"x"`
	Y         int  `json:"y"`
	W         int  `json:"w"`
	H         int  `json:"h"`
	Maximized bool `json:"maximized"`
}

// Config mirrors the Python AppConfig dataclass.
type Config struct {
	IntervalSec    float64
	EnableCores    bool
	EnableFreq     bool
	EnableSwap     bool
	SaveDir        string
	RotateMB       int
	MinimizeToTray bool
	AutoScale      bool
	Language       string
	Placement      *Placement

	raw map[string]json.RawMessage
}

// AppDataDir returns %APPDATA%\SysGauge.
func AppDataDir() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		base, _ = os.UserHomeDir()
	}
	return filepath.Join(base, AppDirName)
}

// ConfigPath returns the config file path.
func ConfigPath() string { return filepath.Join(AppDataDir(), "config.json") }

// LogDir returns the log folder.
func LogDir() string { return filepath.Join(AppDataDir(), "logs") }

// DefaultSaveDir returns %USERPROFILE%\Documents\SysGauge.
func DefaultSaveDir() string {
	home, _ := os.UserHomeDir()
	docs := filepath.Join(home, "Documents")
	if info, err := os.Stat(docs); err != nil || !info.IsDir() {
		docs = home
	}
	return filepath.Join(docs, AppDirName)
}

// Default returns a config with all documented defaults.
func Default() *Config {
	return &Config{
		IntervalSec:    1.0,
		EnableCores:    false,
		EnableFreq:     true,
		EnableSwap:     false,
		SaveDir:        DefaultSaveDir(),
		RotateMB:       100,
		MinimizeToTray: true,
		AutoScale:      false,
		Language:       "",
	}
}

// Load reads the config file, falling back to defaults on any error.
func Load() *Config {
	cfg := Default()
	path := ConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg.save()
		}
		return cfg
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return cfg
	}
	cfg.raw = raw

	cfg.IntervalSec = number(raw["interval_sec"], cfg.IntervalSec)
	cfg.RotateMB = int(number(raw["rotate_mb"], float64(cfg.RotateMB)))
	cfg.SaveDir = text(raw["save_dir"], cfg.SaveDir)
	cfg.Language = text(raw["language"], cfg.Language)
	cfg.EnableCores = boolean(raw["enable_cores"], cfg.EnableCores)
	cfg.EnableFreq = boolean(raw["enable_freq"], cfg.EnableFreq)
	cfg.EnableSwap = boolean(raw["enable_swap"], cfg.EnableSwap)
	cfg.MinimizeToTray = boolean(raw["minimize_to_tray"], cfg.MinimizeToTray)
	cfg.AutoScale = boolean(raw["auto_scale"], cfg.AutoScale)
	if p, ok := raw["window_placement"]; ok {
		var placement Placement
		if json.Unmarshal(p, &placement) == nil {
			cfg.Placement = &placement
		}
	}
	cfg.normalize()
	return cfg
}

// normalize clamps out-of-range values exactly like the Python edition.
func (c *Config) normalize() {
	if c.IntervalSec < MinInterval || c.IntervalSec > MaxInterval {
		c.IntervalSec = clamp(c.IntervalSec, MinInterval, MaxInterval)
	}
	if c.RotateMB < MinRotateMB || c.RotateMB > MaxRotateMB {
		c.RotateMB = int(clamp(float64(c.RotateMB), MinRotateMB, MaxRotateMB))
	}
	if strings.TrimSpace(c.SaveDir) == "" {
		c.SaveDir = DefaultSaveDir()
	}
	c.Language = strings.TrimSpace(c.Language)
}

// Save writes the config atomically; errors are returned to the caller.
func (c *Config) Save() error { return c.save() }

func (c *Config) save() error {
	values := map[string]any{
		"interval_sec":     c.IntervalSec,
		"enable_cores":     c.EnableCores,
		"enable_freq":      c.EnableFreq,
		"enable_swap":      c.EnableSwap,
		"save_dir":         c.SaveDir,
		"rotate_mb":        c.RotateMB,
		"minimize_to_tray": c.MinimizeToTray,
		"auto_scale":       c.AutoScale,
		"language":         c.Language,
	}
	for key, raw := range c.raw {
		if _, known := values[key]; known {
			continue
		}
		var value any
		if json.Unmarshal(raw, &value) == nil {
			values[key] = value
		}
	}
	if c.Placement != nil {
		values["window_placement"] = c.Placement
	}

	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		os.Remove(tmp)
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// EnsureSaveDir verifies the save folder, falling back to the default one.
func (c *Config) EnsureSaveDir() bool {
	if IsWritableDir(c.SaveDir) {
		return true
	}
	c.SaveDir = DefaultSaveDir()
	return IsWritableDir(c.SaveDir)
}

// IsWritableDir creates the folder if needed and probes it with a temp file.
func IsWritableDir(path string) bool {
	if path == "" {
		return false
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return false
	}
	probe := filepath.Join(path, ".sysgauge_write_test")
	file, err := os.OpenFile(probe, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return false
	}
	_, _ = file.WriteString("ok")
	_ = file.Close()
	_ = os.Remove(probe)
	return true
}

func clamp(v, low, high float64) float64 {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

func number(raw json.RawMessage, fallback float64) float64 {
	if len(raw) == 0 {
		return fallback
	}
	var f float64
	if json.Unmarshal(raw, &f) != nil {
		return fallback
	}
	return f
}

func boolean(raw json.RawMessage, fallback bool) bool {
	if len(raw) == 0 {
		return fallback
	}
	var b bool
	if json.Unmarshal(raw, &b) != nil {
		return fallback
	}
	return b
}

func text(raw json.RawMessage, fallback string) string {
	if len(raw) == 0 {
		return fallback
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return fallback
	}
	return s
}

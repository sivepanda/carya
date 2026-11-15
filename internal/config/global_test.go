package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveGlobalConfigPersistsDefaultFlushIntervals(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := SaveGlobalConfig(GlobalConfig{DeviceID: "laptop"}); err != nil {
		t.Fatalf("save global config: %v", err)
	}

	path, err := GlobalConfigPath()
	if err != nil {
		t.Fatalf("global config path: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}

	text := string(data)
	if !strings.Contains(text, `"hot_flush_interval": "2m0s"`) {
		t.Fatalf("expected hot flush interval default in file, got %s", text)
	}
	if !strings.Contains(text, `"cold_flush_interval": "30m0s"`) {
		t.Fatalf("expected cold flush interval default in file, got %s", text)
	}
}

func TestGlobalConfigFlushIntervalsNormalizesInvalidValues(t *testing.T) {
	cfg := GlobalConfig{
		HotFlushInterval:  "bogus",
		ColdFlushInterval: "1m",
	}

	hot, cold := cfg.FlushIntervals()
	if hot != DefaultHotFlushInterval {
		t.Fatalf("expected default hot interval %s, got %s", DefaultHotFlushInterval, hot)
	}
	if cold != hot {
		t.Fatalf("expected cold interval to be clamped to hot %s, got %s", hot, cold)
	}

	cfg = GlobalConfig{HotFlushInterval: "10m", ColdFlushInterval: "5m"}
	hot, cold = cfg.FlushIntervals()
	if hot != 10*time.Minute {
		t.Fatalf("expected hot interval 10m, got %s", hot)
	}
	if cold != 10*time.Minute {
		t.Fatalf("expected cold interval to be clamped to hot, got %s", cold)
	}
}

func TestGlobalConfigPathUsesConfigDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	path, err := GlobalConfigPath()
	if err != nil {
		t.Fatalf("global config path: %v", err)
	}

	if filepath.Base(path) != "carya.json" {
		t.Fatalf("expected carya.json file name, got %s", path)
	}
}

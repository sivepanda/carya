package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultHotFlushInterval  = 2 * time.Minute
	DefaultColdFlushInterval = 30 * time.Minute
)

// GlobalConfig stores machine-level Carya settings shared by all projects.
type GlobalConfig struct {
	DeviceID          string `json:"device_id,omitempty"`
	HotFlushInterval  string `json:"hot_flush_interval,omitempty"`
	ColdFlushInterval string `json:"cold_flush_interval,omitempty"`
}

func LoadGlobalConfig() (GlobalConfig, error) {
	path, err := GlobalConfigPath()
	if err != nil {
		return GlobalConfig{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return GlobalConfig{}, nil
		}
		return GlobalConfig{}, err
	}

	var cfg GlobalConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return GlobalConfig{}, err
	}

	cfg.normalize()
	return cfg, nil
}

func SaveGlobalConfig(cfg GlobalConfig) error {
	path, err := GlobalConfigPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	cfg.normalize()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func GlobalConfigExists() bool {
	path, err := GlobalConfigPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func GlobalConfigPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve user config dir: %w", err)
	}
	return filepath.Join(base, "carya", "carya.json"), nil
}

// FlushIntervals returns normalized hot and cold flush intervals.
func (cfg GlobalConfig) FlushIntervals() (hot time.Duration, cold time.Duration) {
	cfg.normalize()
	hot, _ = time.ParseDuration(cfg.HotFlushInterval)
	cold, _ = time.ParseDuration(cfg.ColdFlushInterval)
	return hot, cold
}

func (cfg *GlobalConfig) normalize() {
	cfg.DeviceID = strings.TrimSpace(cfg.DeviceID)

	hot := parseDurationOrDefault(strings.TrimSpace(cfg.HotFlushInterval), DefaultHotFlushInterval)
	cold := parseDurationOrDefault(strings.TrimSpace(cfg.ColdFlushInterval), DefaultColdFlushInterval)

	if cold < hot {
		cold = hot
	}

	cfg.HotFlushInterval = hot.String()
	cfg.ColdFlushInterval = cold.String()
}

func parseDurationOrDefault(value string, fallback time.Duration) time.Duration {
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}

	return parsed
}

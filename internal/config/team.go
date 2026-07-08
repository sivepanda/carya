package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type TeamConfig struct {
	AutoPublish bool `json:"auto_publish"`
	AutoFetch   bool `json:"auto_fetch"`
	AutoPush    bool `json:"auto_push"`
}

func LoadTeamConfig(caryaPath string) TeamConfig {
	data, err := os.ReadFile(teamConfigPath(caryaPath))
	if err != nil {
		return TeamConfig{}
	}
	var cfg TeamConfig
	json.Unmarshal(data, &cfg)
	return cfg
}

func SaveTeamConfig(caryaPath string, cfg TeamConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(teamConfigPath(caryaPath), data, 0644)
}

func teamConfigPath(caryaPath string) string {
	return filepath.Join(caryaPath, "team.json")
}

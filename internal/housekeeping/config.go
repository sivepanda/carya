package housekeeping

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Command struct {
	Command     string `json:"command"`
	WorkingDir  string `json:"working_dir"`
	Description string `json:"description"`
}

type Config struct {
	Version            string    `json:"version"`
	AutoApprovePostPull     bool      `json:"auto_approve_post_pull,omitempty"`
	AutoApprovePostCheckout bool      `json:"auto_approve_post_checkout,omitempty"`
	PostPull           []Command `json:"post-pull"`
	PostCheckout       []Command `json:"post-checkout"`
}

const (
	ConfigVersion = "1.0"
	ConfigFile    = "housekeeping.json"
)

func NewConfig() *Config {
	return &Config{
		Version:      ConfigVersion,
		PostPull:     []Command{},
		PostCheckout: []Command{},
	}
}

func GetConfigPath() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	caryaDir := filepath.Join(wd, ".carya")
	if _, err := os.Stat(caryaDir); os.IsNotExist(err) {
		return "", fmt.Errorf(".carya directory not found - run 'carya init' first")
	}

	return filepath.Join(caryaDir, ConfigFile), nil
}

func LoadConfig() (*Config, error) {
	configPath, err := GetConfigPath()
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return NewConfig(), nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

func (c *Config) Save() error {
	configPath, err := GetConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func (c *Config) AddCommand(category, command, workingDir, description string) error {
	cmd := Command{
		Command:     command,
		WorkingDir:  workingDir,
		Description: description,
	}

	switch category {
	case "post-pull":
		c.PostPull = append(c.PostPull, cmd)
	case "post-checkout":
		c.PostCheckout = append(c.PostCheckout, cmd)
	default:
		return fmt.Errorf("unknown category: %s", category)
	}

	return nil
}

func (c *Config) GetCommands(category string) ([]Command, error) {
	switch category {
	case "post-pull":
		return c.PostPull, nil
	case "post-checkout":
		return c.PostCheckout, nil
	default:
		return nil, fmt.Errorf("unknown category: %s", category)
	}
}

func (c *Config) IsAutoApprove(category string) bool {
	switch category {
	case "post-pull":
		return c.AutoApprovePostPull
	case "post-checkout":
		return c.AutoApprovePostCheckout
	default:
		return false
	}
}

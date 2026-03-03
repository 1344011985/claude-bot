package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ModelConfig holds pricing and configuration for a specific Claude model.
type ModelConfig struct {
	Name            string  `json:"name"`
	InputPriceMTok  float64 `json:"input_price_mtok"`
	OutputPriceMTok float64 `json:"output_price_mtok"`
	CacheWriteMTok  float64 `json:"cache_write_mtok"`
	CacheReadMTok   float64 `json:"cache_read_mtok"`
}

// Config holds all application configuration.
type Config struct {
	Channel string `json:"channel"`
	Feishu  struct {
		AppID     string `json:"app_id"`
		AppSecret string `json:"app_secret"`
	} `json:"feishu"`
	Claude struct {
		BinPath           string                 `json:"bin_path"`
		TimeoutSeconds    int                    `json:"timeout_seconds"`
		MaxTimeoutSeconds int                    `json:"max_timeout_seconds"`
		Models            map[string]ModelConfig `json:"models"`
		AutoSelect        bool                   `json:"auto_select"`
		DefaultModel      string                 `json:"default_model"`
	} `json:"claude"`
	Memory struct {
		DBPath string `json:"db_path"`
	} `json:"memory"`
	Images struct {
		CacheDir  string `json:"cache_dir"`
		MaxSizeMB int    `json:"max_size_mb"`
	} `json:"images"`
	Allowlist    []string `json:"allowlist"`
	LogLevel     string   `json:"log_level"`
	SystemPrompt string   `json:"system_prompt"`

	// ConfigDir is the resolved directory where claude-bot.json was loaded from.
	// Not serialised — set at runtime by Load.
	ConfigDir string `json:"-"`
}

// ConfigPath returns the platform-appropriate path to claude-bot.json.
//
//	Windows : C:\Users\<user>\.claude-bot\claude-bot.json
//	macOS   : /Users/<user>/.claude-bot/claude-bot.json
//	Linux   : /root/.claude-bot/claude-bot.json  (or ~/.claude-bot/)
func ConfigPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "claude-bot.json"), nil
}

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".claude-bot"), nil
}

// Platform returns a short description of the current OS for logging.
func Platform() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "macos"
	default:
		return "linux"
	}
}

// Load reads configuration from the platform-default claude-bot.json.
// Missing file is not an error — defaults are returned.
func Load() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	return LoadFrom(path)
}

// LoadFrom reads configuration from an explicit file path.
func LoadFrom(path string) (*Config, error) {
	cfg := defaultConfig()
	cfg.ConfigDir = filepath.Dir(path)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config file: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	applyDefaults(cfg)
	cfg.ConfigDir = filepath.Dir(path)
	return cfg, nil
}

// Validate checks that all required fields are present and values are within bounds.
func (c *Config) Validate() error {
	switch c.Channel {
	case "feishu":
		if c.Feishu.AppID == "" {
			return fmt.Errorf("feishu.app_id is required")
		}
		if c.Feishu.AppSecret == "" {
			return fmt.Errorf("feishu.app_secret is required")
		}
	case "":
		return fmt.Errorf("channel is required (set \"channel\": \"feishu\" in claude-bot.json or use -channel flag)")
	default:
		return fmt.Errorf("unknown channel: %s (supported: feishu)", c.Channel)
	}

	if c.Claude.MaxTimeoutSeconds > 0 && c.Claude.TimeoutSeconds > c.Claude.MaxTimeoutSeconds {
		return fmt.Errorf("claude.timeout_seconds (%d) exceeds max_timeout_seconds (%d)",
			c.Claude.TimeoutSeconds, c.Claude.MaxTimeoutSeconds)
	}

	return nil
}

func defaultConfig() *Config {
	cfg := &Config{}
	cfg.Channel = "feishu"
	cfg.Claude.BinPath = "claude"
	cfg.Claude.AutoSelect = true
	cfg.Claude.DefaultModel = "haiku"
	cfg.Claude.Models = map[string]ModelConfig{
		"haiku": {
			Name:            "claude-haiku-4-5-20251001",
			InputPriceMTok:  1.0,
			OutputPriceMTok: 5.0,
			CacheWriteMTok:  1.25,
			CacheReadMTok:   0.1,
		},
		"sonnet": {
			Name:            "claude-sonnet-4-6",
			InputPriceMTok:  15.0,
			OutputPriceMTok: 75.0,
			CacheWriteMTok:  18.75,
			CacheReadMTok:   1.5,
		},
		"opus": {
			Name:            "claude-opus-4-6",
			InputPriceMTok:  30.0,
			OutputPriceMTok: 150.0,
			CacheWriteMTok:  37.5,
			CacheReadMTok:   3.0,
		},
	}
	cfg.LogLevel = "info"
	cfg.Images.MaxSizeMB = 10
	return cfg
}

func applyDefaults(cfg *Config) {
	if cfg.Channel == "" {
		cfg.Channel = "feishu"
	}
	if cfg.Claude.BinPath == "" {
		cfg.Claude.BinPath = "claude"
	}
	if cfg.Claude.Models == nil || len(cfg.Claude.Models) == 0 {
		cfg.Claude.Models = defaultConfig().Claude.Models
	}
	if cfg.Claude.DefaultModel == "" {
		cfg.Claude.DefaultModel = "haiku"
	}
	if cfg.Memory.DBPath == "" {
		// Default: store DB alongside config file
		if cfg.ConfigDir != "" {
			cfg.Memory.DBPath = filepath.Join(cfg.ConfigDir, "data", "bot.db")
		} else {
			cfg.Memory.DBPath = "data/bot.db"
		}
	}
	if cfg.Images.MaxSizeMB == 0 {
		cfg.Images.MaxSizeMB = 10
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
}

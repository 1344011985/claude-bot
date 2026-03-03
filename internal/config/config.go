package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ModelConfig holds pricing and configuration for a specific Claude model.
type ModelConfig struct {
	Name            string  `yaml:"name"`
	InputPriceMTok  float64 `yaml:"input_price_mtok"`
	OutputPriceMTok float64 `yaml:"output_price_mtok"`
	CacheWriteMTok  float64 `yaml:"cache_write_mtok"`
	CacheReadMTok   float64 `yaml:"cache_read_mtok"`
}

// Config holds all application configuration.
type Config struct {
	QQ struct {
		AppID     string `yaml:"app_id"`
		AppSecret string `yaml:"app_secret"`
	} `yaml:"qq"`
	Feishu struct {
		AppID     string `yaml:"app_id"`
		AppSecret string `yaml:"app_secret"`
	} `yaml:"feishu"`
	Claude struct {
		BinPath           string                 `yaml:"bin_path"`
		TimeoutSeconds    int                    `yaml:"timeout_seconds"`
		MaxTimeoutSeconds int                    `yaml:"max_timeout_seconds"`
		Models            map[string]ModelConfig `yaml:"models"`
		AutoSelect        bool                   `yaml:"auto_select"`
		DefaultModel      string                 `yaml:"default_model"`
	} `yaml:"claude"`
	Memory struct {
		DBPath string `yaml:"db_path"`
	} `yaml:"memory"`
	Images struct {
		CacheDir  string `yaml:"cache_dir"`
		MaxSizeMB int    `yaml:"max_size_mb"`
	} `yaml:"images"`
	Allowlist    []string `yaml:"allowlist"`
	LogLevel     string   `yaml:"log_level"`
	SystemPrompt string   `yaml:"system_prompt"`
}

// Load reads configuration from the given YAML file path.
// If the file is not found, it falls back to environment variables
// QQ_APP_ID and QQ_APP_SECRET.
func Load(path string) (*Config, error) {
	cfg := defaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Fallback to environment variables
			cfg.QQ.AppID = os.Getenv("QQ_APP_ID")
			cfg.QQ.AppSecret = os.Getenv("QQ_APP_SECRET")
			return cfg, nil
		}
		return nil, fmt.Errorf("read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	// Apply defaults for zero values after unmarshal
	applyDefaults(cfg)
	return cfg, nil
}

// Validate checks that all required fields are present and values are within bounds.
// channel can be "qq" or "feishu" to validate the corresponding section.
func (c *Config) Validate(channels ...string) error {
	channel := "qq"
	if len(channels) > 0 && channels[0] != "" {
		channel = channels[0]
	}

	switch channel {
	case "qq":
		if c.QQ.AppID == "" {
			return fmt.Errorf("qq.app_id is required (or set QQ_APP_ID env var)")
		}
		if c.QQ.AppSecret == "" {
			return fmt.Errorf("qq.app_secret is required (or set QQ_APP_SECRET env var)")
		}
	case "feishu":
		if c.Feishu.AppID == "" {
			return fmt.Errorf("feishu.app_id is required")
		}
		if c.Feishu.AppSecret == "" {
			return fmt.Errorf("feishu.app_secret is required")
		}
	default:
		return fmt.Errorf("unknown channel: %s (supported: qq, feishu)", channel)
	}

	// timeout_seconds=0 means unlimited; only enforce the bound when max is set
	if c.Claude.MaxTimeoutSeconds > 0 && c.Claude.TimeoutSeconds > c.Claude.MaxTimeoutSeconds {
		return fmt.Errorf("claude.timeout_seconds (%d) exceeds max_timeout_seconds (%d)",
			c.Claude.TimeoutSeconds, c.Claude.MaxTimeoutSeconds)
	}

	return nil
}

func defaultConfig() *Config {
	cfg := &Config{}
	cfg.Claude.BinPath = "claude"
	cfg.Claude.TimeoutSeconds = 0
	cfg.Claude.MaxTimeoutSeconds = 0
	cfg.Claude.AutoSelect = true
	cfg.Claude.DefaultModel = "haiku"
	cfg.Claude.Models = map[string]ModelConfig{
		"haiku": {
			Name:            "claude-haiku-4.5",
			InputPriceMTok:  1.0,
			OutputPriceMTok: 5.0,
			CacheWriteMTok:  1.25,
			CacheReadMTok:   0.1,
		},
		"sonnet": {
			Name:            "claude-sonnet-4.6",
			InputPriceMTok:  15.0,
			OutputPriceMTok: 75.0,
			CacheWriteMTok:  18.75,
			CacheReadMTok:   1.5,
		},
		"opus": {
			Name:            "claude-opus-4.6",
			InputPriceMTok:  30.0,
			OutputPriceMTok: 150.0,
			CacheWriteMTok:  37.5,
			CacheReadMTok:   3.0,
		},
	}
	cfg.Memory.DBPath = "data/bot.db"
	cfg.LogLevel = "info"
	return cfg
}

func applyDefaults(cfg *Config) {
	if cfg.Claude.BinPath == "" {
		cfg.Claude.BinPath = "claude"
	}
	// timeout_seconds=0 means no timeout (unlimited)
	if cfg.Claude.Models == nil || len(cfg.Claude.Models) == 0 {
		cfg.Claude.Models = defaultConfig().Claude.Models
	}
	if cfg.Claude.DefaultModel == "" {
		cfg.Claude.DefaultModel = "haiku"
	}
	if cfg.Memory.DBPath == "" {
		cfg.Memory.DBPath = "data/bot.db"
	}
	if cfg.Images.MaxSizeMB == 0 {
		cfg.Images.MaxSizeMB = 10
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
}

package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Collector CollectorConfig `yaml:"collector"`
	Auth      AuthConfig      `yaml:"auth"`
	Analysis  AnalysisConfig  `yaml:"analysis"`
}

type CollectorConfig struct {
	Address string `yaml:"address"`
	Insecure bool   `yaml:"insecure"`
	Timeout  int    `yaml:"timeout"`
}

type AuthConfig struct {
	Type       string `yaml:"type"`
	APIKey     string `yaml:"api_key"`
	Token      string `yaml:"token"`
	Username   string `yaml:"username"`
	Password   string `yaml:"password"`
	TLSCert    string `yaml:"tls_cert"`
	TLSKey     string `yaml:"tls_key"`
	TLSCA      string `yaml:"tls_ca"`
}

type AnalysisConfig struct {
	SlowQueryThresholdMs int64 `yaml:"slow_query_threshold_ms"`
	MaxResults           int   `yaml:"max_results"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	cfg.setDefaults()

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) setDefaults() {
	if c.Collector.Timeout == 0 {
		c.Collector.Timeout = 30
	}
	if c.Analysis.SlowQueryThresholdMs == 0 {
		c.Analysis.SlowQueryThresholdMs = 100
	}
	if c.Analysis.MaxResults == 0 {
		c.Analysis.MaxResults = 50
	}
}

func (c *Config) validate() error {
	if c.Collector.Address == "" {
		return fmt.Errorf("collector address is required")
	}
	if c.Analysis.SlowQueryThresholdMs < 0 {
		return fmt.Errorf("slow query threshold must be non-negative")
	}
	return nil
}

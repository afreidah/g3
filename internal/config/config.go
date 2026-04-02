// -------------------------------------------------------------------------------
// Configuration - Root Config Loader and Validator
//
// Author: Alex Freidah
//
// Loads YAML configuration with environment variable expansion, applies
// defaults, and validates all fields. Returns collected errors rather than
// failing on the first issue, enabling operators to fix all problems in a
// single pass.
// -------------------------------------------------------------------------------

package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// -------------------------------------------------------------------------
// TYPES
// -------------------------------------------------------------------------

// Config is the root configuration for g3.
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Gmail     GmailConfig     `yaml:"gmail"`
	Database  DatabaseConfig  `yaml:"database"`
	Buckets   []BucketConfig  `yaml:"buckets"`
	Telemetry TelemetryConfig `yaml:"telemetry"`
}

// -------------------------------------------------------------------------
// LOADING
// -------------------------------------------------------------------------

// LoadConfig reads and parses a YAML config file with environment variable
// expansion. Variables are referenced as ${VAR_NAME} in the YAML file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	expanded := os.Expand(string(data), os.Getenv)

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.SetDefaultsAndValidate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// -------------------------------------------------------------------------
// VALIDATION
// -------------------------------------------------------------------------

// SetDefaultsAndValidate applies defaults and validates the entire config
// tree. Returns an error listing all validation failures.
func (c *Config) SetDefaultsAndValidate() error {
	var errs []string

	errs = append(errs, c.Server.setDefaultsAndValidate()...)
	errs = append(errs, c.Gmail.setDefaultsAndValidate()...)
	errs = append(errs, c.Database.setDefaultsAndValidate()...)
	errs = append(errs, c.Telemetry.setDefaultsAndValidate()...)

	if len(c.Buckets) == 0 {
		errs = append(errs, "at least one bucket must be configured")
	}
	for i := range c.Buckets {
		errs = append(errs, c.Buckets[i].setDefaultsAndValidate()...)
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation failed:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}

// -------------------------------------------------------------------------
// HELPERS
// -------------------------------------------------------------------------

// ParseLogLevel converts a log level string to slog.Level.
func ParseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

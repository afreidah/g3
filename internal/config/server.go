// -------------------------------------------------------------------------------
// Server Configuration
//
// Author: Alex Freidah
//
// HTTP server settings including listen address, timeouts, and log level.
// -------------------------------------------------------------------------------

package config

import (
	"time"
)

// -------------------------------------------------------------------------
// TYPES
// -------------------------------------------------------------------------

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	ListenAddr      string        `yaml:"listen_addr"`
	LogLevel        string        `yaml:"log_level"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

// -------------------------------------------------------------------------
// VALIDATION
// -------------------------------------------------------------------------

// setDefaultsAndValidate applies defaults and returns any validation errors.
func (c *ServerConfig) setDefaultsAndValidate() []string {
	var errs []string

	if c.ListenAddr == "" {
		c.ListenAddr = "0.0.0.0:9000"
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = 5 * time.Minute
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 5 * time.Minute
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = 30 * time.Second
	}

	return errs
}

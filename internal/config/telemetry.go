// -------------------------------------------------------------------------------
// Telemetry Configuration
//
// Author: Alex Freidah
//
// Settings for Prometheus metrics and OpenTelemetry tracing endpoints.
// -------------------------------------------------------------------------------

package config

import (
	"github.com/afreidah/g3/internal/telemetry"
)

// -------------------------------------------------------------------------
// TYPES
// -------------------------------------------------------------------------

// TelemetryConfig holds metrics and tracing configuration.
type TelemetryConfig struct {
	Metrics MetricsConfig          `yaml:"metrics"`
	Tracing telemetry.TracingConfig `yaml:"tracing"`
}

// MetricsConfig controls the Prometheus metrics endpoint.
type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"`
}

// -------------------------------------------------------------------------
// VALIDATION
// -------------------------------------------------------------------------

// setDefaultsAndValidate applies defaults and returns any validation errors.
func (c *TelemetryConfig) setDefaultsAndValidate() []string {
	if c.Metrics.Path == "" {
		c.Metrics.Path = "/metrics"
	}
	if c.Tracing.SampleRate == 0 && c.Tracing.Enabled {
		c.Tracing.SampleRate = 1.0
	}
	return nil
}

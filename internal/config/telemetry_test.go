// -------------------------------------------------------------------------------
// Telemetry Config Tests
//
// Author: Alex Freidah
//
// Verifies default application for the metrics path and the tracing sample
// rate, including the branch that only defaults the sample rate when tracing
// is enabled.
// -------------------------------------------------------------------------------

package config

import (
	"testing"

	"github.com/afreidah/g3/internal/telemetry"
)

func TestTelemetryConfig_Defaults(t *testing.T) {
	tests := []struct {
		name           string
		in             TelemetryConfig
		wantPath       string
		wantSampleRate float64
	}{
		{
			name:           "empty applies metrics path default only",
			in:             TelemetryConfig{},
			wantPath:       "/metrics",
			wantSampleRate: 0, // tracing disabled → sample rate left at zero
		},
		{
			name:           "tracing enabled defaults sample rate to 1.0",
			in:             TelemetryConfig{Tracing: telemetry.TracingConfig{Enabled: true}},
			wantPath:       "/metrics",
			wantSampleRate: 1.0,
		},
		{
			name:           "explicit values preserved",
			in:             TelemetryConfig{Metrics: MetricsConfig{Path: "/custom"}, Tracing: telemetry.TracingConfig{Enabled: true, SampleRate: 0.25}},
			wantPath:       "/custom",
			wantSampleRate: 0.25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.in
			if errs := cfg.setDefaultsAndValidate(); errs != nil {
				t.Fatalf("setDefaultsAndValidate returned errors: %v", errs)
			}
			if cfg.Metrics.Path != tt.wantPath {
				t.Errorf("Metrics.Path = %q, want %q", cfg.Metrics.Path, tt.wantPath)
			}
			if cfg.Tracing.SampleRate != tt.wantSampleRate {
				t.Errorf("Tracing.SampleRate = %v, want %v", cfg.Tracing.SampleRate, tt.wantSampleRate)
			}
		})
	}
}

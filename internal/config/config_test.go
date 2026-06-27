// -------------------------------------------------------------------------------
// Configuration Tests
//
// Author: Alex Freidah
//
// Unit tests for config loading, defaults, and validation. Covers required
// field enforcement, default value application, and cross-field validation.
// -------------------------------------------------------------------------------

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// -------------------------------------------------------------------------
// LOAD CONFIG
// -------------------------------------------------------------------------

func TestLoadConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
server:
  listen_addr: "0.0.0.0:8080"
gmail:
  client_id: "test-client-id"
  client_secret: "test-client-secret"
  refresh_token: "test-refresh-token"
buckets:
  - name: "test"
    credentials:
      - access_key_id: "key1"
        secret_access_key: "secret1"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Server.ListenAddr != "0.0.0.0:8080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.Server.ListenAddr, "0.0.0.0:8080")
	}
	if cfg.Gmail.User != "me" {
		t.Errorf("User = %q, want %q", cfg.Gmail.User, "me")
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := LoadConfig("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("{{invalid"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

// -------------------------------------------------------------------------
// VALIDATION
// -------------------------------------------------------------------------

func TestValidation_MissingBuckets(t *testing.T) {
	cfg := &Config{
		Gmail: GmailConfig{
			ClientID:     "test-id",
			ClientSecret: "test-secret",
			RefreshToken: "test-token",
		},
	}
	err := cfg.SetDefaultsAndValidate()
	if err == nil {
		t.Fatal("expected validation error for missing buckets")
	}
}

func TestValidation_MissingGmailCredentials(t *testing.T) {
	cfg := &Config{
		Buckets: []BucketConfig{{
			Name:        "test",
			Credentials: []CredentialConfig{{AccessKeyID: "k", SecretAccessKey: "s"}},
		}},
	}
	err := cfg.SetDefaultsAndValidate()
	if err == nil {
		t.Fatal("expected validation error for missing gmail credentials")
	}
}

func TestBucketValidation_Errors(t *testing.T) {
	tests := []struct {
		name    string
		bucket  BucketConfig
		wantErr int
	}{
		{
			name:    "missing name",
			bucket:  BucketConfig{Credentials: []CredentialConfig{{AccessKeyID: "k", SecretAccessKey: "s"}}},
			wantErr: 1,
		},
		{
			name:    "no credentials",
			bucket:  BucketConfig{Name: "b"},
			wantErr: 1,
		},
		{
			name:    "missing access key id",
			bucket:  BucketConfig{Name: "b", Credentials: []CredentialConfig{{SecretAccessKey: "s"}}},
			wantErr: 1,
		},
		{
			name:    "missing secret access key",
			bucket:  BucketConfig{Name: "b", Credentials: []CredentialConfig{{AccessKeyID: "k"}}},
			wantErr: 1,
		},
		{
			name:    "valid",
			bucket:  BucketConfig{Name: "b", Credentials: []CredentialConfig{{AccessKeyID: "k", SecretAccessKey: "s"}}},
			wantErr: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.bucket.setDefaultsAndValidate()
			if len(errs) != tt.wantErr {
				t.Errorf("errs = %v, want %d error(s)", errs, tt.wantErr)
			}
		})
	}
}

func TestValidation_ChunkSizeTooLarge(t *testing.T) {
	cfg := &Config{
		Gmail: GmailConfig{
			ClientID:           "test-id",
			ClientSecret:       "test-secret",
			RefreshToken:       "test-token",
			MaxAttachmentBytes: 25_000_000,
			ChunkSizeBytes:     30_000_000,
		},
		Buckets: []BucketConfig{{
			Name:        "test",
			Credentials: []CredentialConfig{{AccessKeyID: "k", SecretAccessKey: "s"}},
		}},
	}
	err := cfg.SetDefaultsAndValidate()
	if err == nil {
		t.Fatal("expected validation error for chunk_size >= max_attachment")
	}
}

// -------------------------------------------------------------------------
// DEFAULTS
// -------------------------------------------------------------------------

func TestDefaults_Server(t *testing.T) {
	cfg := ServerConfig{}
	cfg.setDefaultsAndValidate()

	if cfg.ListenAddr != "0.0.0.0:9000" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, "0.0.0.0:9000")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
}

func TestDefaults_Gmail(t *testing.T) {
	cfg := GmailConfig{ClientID: "id", ClientSecret: "secret", RefreshToken: "tok"}
	cfg.setDefaultsAndValidate()

	if cfg.User != "me" {
		t.Errorf("User = %q, want %q", cfg.User, "me")
	}
	if cfg.MaxAttachmentBytes != 25_000_000 {
		t.Errorf("MaxAttachmentBytes = %d, want %d", cfg.MaxAttachmentBytes, 25_000_000)
	}
	if cfg.LabelPrefix != "s3" {
		t.Errorf("LabelPrefix = %q, want %q", cfg.LabelPrefix, "s3")
	}
}

// -------------------------------------------------------------------------
// DATABASE DEFAULTS AND VALIDATION
// -------------------------------------------------------------------------

func TestDefaults_Database_SQLite(t *testing.T) {
	cfg := DatabaseConfig{}
	errs := cfg.setDefaultsAndValidate()
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if cfg.Driver != "sqlite" {
		t.Errorf("Driver = %q, want %q", cfg.Driver, "sqlite")
	}
	if cfg.Path != "g3-metadata.db" {
		t.Errorf("Path = %q, want %q", cfg.Path, "g3-metadata.db")
	}
}

func TestDefaults_Database_Postgres(t *testing.T) {
	cfg := DatabaseConfig{
		Driver:   "postgres",
		Host:     "localhost",
		Database: "g3",
		User:     "g3user",
	}
	errs := cfg.setDefaultsAndValidate()
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if cfg.Port != 5432 {
		t.Errorf("Port = %d, want 5432", cfg.Port)
	}
	if cfg.SSLMode != "prefer" {
		t.Errorf("SSLMode = %q, want %q", cfg.SSLMode, "prefer")
	}
	if cfg.MaxConns != 5 {
		t.Errorf("MaxConns = %d, want 5", cfg.MaxConns)
	}
}

func TestValidation_Database_PostgresMissingFields(t *testing.T) {
	cfg := DatabaseConfig{Driver: "postgres"}
	errs := cfg.setDefaultsAndValidate()
	if len(errs) != 3 {
		t.Errorf("expected 3 errors (host, database, user), got %d: %v", len(errs), errs)
	}
}

func TestValidation_Database_InvalidDriver(t *testing.T) {
	cfg := DatabaseConfig{Driver: "mysql"}
	errs := cfg.setDefaultsAndValidate()
	if len(errs) != 1 {
		t.Errorf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

// -------------------------------------------------------------------------
// LOG LEVEL PARSING
// -------------------------------------------------------------------------

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"debug", "DEBUG"},
		{"info", "INFO"},
		{"warn", "WARN"},
		{"warning", "WARN"},
		{"error", "ERROR"},
		{"unknown", "INFO"},
		{"", "INFO"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseLogLevel(tt.input).String()
			if got != tt.want {
				t.Errorf("ParseLogLevel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

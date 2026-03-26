// -------------------------------------------------------------------------------
// Gmail Configuration
//
// Author: Alex Freidah
//
// Gmail API connection settings including OAuth2 credentials, attachment size
// limits, and label naming conventions. Users provide their own Google Cloud
// project credentials and a refresh token obtained via `g3 auth`.
// -------------------------------------------------------------------------------

package config

// -------------------------------------------------------------------------
// TYPES
// -------------------------------------------------------------------------

// GmailConfig holds Gmail API connection and behavior settings.
type GmailConfig struct {
	ClientID           string `yaml:"client_id"`
	ClientSecret       string `yaml:"client_secret"`
	RefreshToken       string `yaml:"refresh_token"`
	User               string `yaml:"user"`
	MaxAttachmentBytes int64  `yaml:"max_attachment_bytes"`
	ChunkSizeBytes     int64  `yaml:"chunk_size_bytes"`
	LabelPrefix        string `yaml:"label_prefix"`
}

// -------------------------------------------------------------------------
// VALIDATION
// -------------------------------------------------------------------------

// setDefaultsAndValidate applies defaults and returns any validation errors.
func (c *GmailConfig) setDefaultsAndValidate() []string {
	var errs []string

	if c.ClientID == "" {
		errs = append(errs, "gmail.client_id is required")
	}
	if c.ClientSecret == "" {
		errs = append(errs, "gmail.client_secret is required")
	}
	if c.RefreshToken == "" {
		errs = append(errs, "gmail.refresh_token is required (run 'g3 auth' to obtain one)")
	}
	if c.User == "" {
		c.User = "me"
	}
	if c.MaxAttachmentBytes == 0 {
		c.MaxAttachmentBytes = 25_000_000
	}
	if c.ChunkSizeBytes == 0 {
		c.ChunkSizeBytes = 20_000_000
	}
	if c.ChunkSizeBytes >= c.MaxAttachmentBytes {
		errs = append(errs, "gmail.chunk_size_bytes must be less than gmail.max_attachment_bytes")
	}
	if c.LabelPrefix == "" {
		c.LabelPrefix = "s3"
	}

	return errs
}

// -------------------------------------------------------------------------------
// Gmail Configuration
//
// Author: Alex Freidah
//
// Gmail API connection settings including OAuth2 credentials, token storage,
// attachment size limits, and label naming conventions.
// -------------------------------------------------------------------------------

package config

// -------------------------------------------------------------------------
// TYPES
// -------------------------------------------------------------------------

// GmailConfig holds Gmail API connection and behavior settings.
type GmailConfig struct {
	CredentialsFile    string `yaml:"credentials_file"`
	TokenFile          string `yaml:"token_file"`
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

	if c.CredentialsFile == "" {
		errs = append(errs, "gmail.credentials_file is required")
	}
	if c.TokenFile == "" {
		errs = append(errs, "gmail.token_file is required")
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

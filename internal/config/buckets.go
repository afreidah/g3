// -------------------------------------------------------------------------------
// Bucket Configuration
//
// Author: Alex Freidah
//
// S3 bucket definitions with per-bucket credential sets for SigV4
// authentication. Each bucket maps to a Gmail label and can have multiple
// access key pairs.
// -------------------------------------------------------------------------------

package config

// -------------------------------------------------------------------------
// TYPES
// -------------------------------------------------------------------------

// BucketConfig defines an S3 bucket and its access credentials.
type BucketConfig struct {
	Name        string             `yaml:"name"`
	Credentials []CredentialConfig `yaml:"credentials"`
}

// CredentialConfig holds an S3-compatible access key pair.
type CredentialConfig struct {
	AccessKeyID     string `yaml:"access_key_id"`
	SecretAccessKey string `yaml:"secret_access_key"`
}

// -------------------------------------------------------------------------
// VALIDATION
// -------------------------------------------------------------------------

// setDefaultsAndValidate returns any validation errors for this bucket.
func (c *BucketConfig) setDefaultsAndValidate() []string {
	var errs []string

	if c.Name == "" {
		errs = append(errs, "bucket name is required")
	}
	if len(c.Credentials) == 0 {
		errs = append(errs, "bucket "+c.Name+": at least one credential is required")
	}
	for i, cred := range c.Credentials {
		if cred.AccessKeyID == "" {
			errs = append(errs, "bucket "+c.Name+": credential "+string(rune('0'+i))+": access_key_id is required")
		}
		if cred.SecretAccessKey == "" {
			errs = append(errs, "bucket "+c.Name+": credential "+string(rune('0'+i))+": secret_access_key is required")
		}
	}

	return errs
}

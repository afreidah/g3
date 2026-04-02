// -------------------------------------------------------------------------------
// Database Configuration
//
// Author: Alex Freidah
//
// SQLite metadata index settings. The database file path must be on a
// persistent volume to survive container restarts.
// -------------------------------------------------------------------------------

package config

// -------------------------------------------------------------------------
// TYPES
// -------------------------------------------------------------------------

// DatabaseConfig holds SQLite metadata index settings.
type DatabaseConfig struct {
	Path string `yaml:"path"`
}

// -------------------------------------------------------------------------
// VALIDATION
// -------------------------------------------------------------------------

// setDefaultsAndValidate applies defaults and returns any validation errors.
func (c *DatabaseConfig) setDefaultsAndValidate() []string {
	if c.Path == "" {
		c.Path = "g3-metadata.db"
	}
	return nil
}

// -------------------------------------------------------------------------------
// Database Configuration
//
// Author: Alex Freidah
//
// Metadata store settings supporting SQLite (local) and PostgreSQL (shared).
// The driver field selects the backend. SQLite requires a persistent volume;
// PostgreSQL allows the service to run on any node.
// -------------------------------------------------------------------------------

package config

// -------------------------------------------------------------------------
// TYPES
// -------------------------------------------------------------------------

// DatabaseConfig holds metadata store settings.
type DatabaseConfig struct {
	Driver   string `yaml:"driver"`
	Path     string `yaml:"path"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Database string `yaml:"database"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	SSLMode  string `yaml:"ssl_mode"`
	MaxConns int    `yaml:"max_conns"`
}

// -------------------------------------------------------------------------
// VALIDATION
// -------------------------------------------------------------------------

// setDefaultsAndValidate applies defaults and returns any validation errors.
func (c *DatabaseConfig) setDefaultsAndValidate() []string {
	if c.Driver == "" {
		c.Driver = "sqlite"
	}

	switch c.Driver {
	case "sqlite":
		if c.Path == "" {
			c.Path = "g3-metadata.db"
		}
		return nil
	case "postgres":
		return c.setPostgresDefaultsAndValidate()
	default:
		return []string{"database.driver must be 'sqlite' or 'postgres'"}
	}
}

// setPostgresDefaultsAndValidate applies PostgreSQL connection defaults and
// returns any validation errors for the postgres driver.
func (c *DatabaseConfig) setPostgresDefaultsAndValidate() []string {
	var errs []string

	if c.Host == "" {
		errs = append(errs, "database.host is required for postgres driver")
	}
	if c.Database == "" {
		errs = append(errs, "database.database is required for postgres driver")
	}
	if c.User == "" {
		errs = append(errs, "database.user is required for postgres driver")
	}
	if c.Port == 0 {
		c.Port = 5432
	}
	if c.SSLMode == "" {
		c.SSLMode = "prefer"
	}
	if c.MaxConns == 0 {
		c.MaxConns = 5
	}

	return errs
}

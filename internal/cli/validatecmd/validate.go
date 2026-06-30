// -------------------------------------------------------------------------------
// g3 - Validate Subcommand
//
// Author: Alex Freidah
//
// Loads and validates the configuration file without starting the server. Run
// returns an exit code and writes to the provided writers so it can be
// unit-tested without touching the process.
// -------------------------------------------------------------------------------

package validatecmd

import (
	"flag"
	"fmt"
	"io"

	"github.com/afreidah/g3/internal/cli"
	"github.com/afreidah/g3/internal/config"
)

// loadConfig is the injection seam for tests.
var loadConfig = config.LoadConfig

// Run validates the configuration file named by the -config flag and returns
// the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", cli.DefaultConfigPath, "path to config file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if _, err := loadConfig(*configPath); err != nil {
		fmt.Fprintf(stderr, "validation failed: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, "configuration is valid")
	return 0
}

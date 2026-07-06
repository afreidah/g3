// -------------------------------------------------------------------------------
// g3 - Version Subcommand
//
// Author: Alex Freidah
//
// Prints the binary version and Go runtime version. The version string is
// injected at build time via ldflags. Run returns an exit code and writes to
// the provided writer so it can be unit-tested.
// -------------------------------------------------------------------------------

package versioncmd

import (
	"fmt"
	"io"
	"runtime"

	"github.com/afreidah/g3/internal/telemetry"
)

// Run prints version information to stdout and returns the process exit code.
func Run(stdout io.Writer) int {
	fmt.Fprintf(stdout, "g3 %s (%s)\n", telemetry.Version, runtime.Version())
	return 0
}

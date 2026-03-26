// -------------------------------------------------------------------------------
// Version - Build Information Printer
//
// Author: Alex Freidah
//
// Prints the binary version and Go runtime version, then exits. The version
// string is injected at build time via ldflags.
// -------------------------------------------------------------------------------

package main

import (
	"fmt"
	"runtime"

	"github.com/afreidah/g3/internal/telemetry"
)

// runVersion prints version information and exits.
func runVersion() { // codecov:ignore -- os.Exit wrapper
	fmt.Printf("g3 %s (%s)\n", telemetry.Version, runtime.Version())
}

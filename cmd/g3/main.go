// -------------------------------------------------------------------------------
// g3 - S3-Compatible Gateway Backed by Gmail
//
// Author: Alex Freidah
//
// Entry point and subcommand dispatch. Each subcommand's logic lives under
// internal/cli/* so it stays unit-testable; this package is a thin shell that
// turns each command's returned exit code into a process exit.
// -------------------------------------------------------------------------------

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/afreidah/g3/internal/cli/authcmd"
	"github.com/afreidah/g3/internal/cli/servecmd"
	"github.com/afreidah/g3/internal/cli/synccmd"
	"github.com/afreidah/g3/internal/cli/validatecmd"
	"github.com/afreidah/g3/internal/cli/versioncmd"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "version":
			os.Exit(versioncmd.Run(os.Stdout))
		case "auth":
			os.Exit(authcmd.Run(args[1:], os.Stdout, os.Stderr))
		case "sync":
			os.Exit(synccmd.Run(context.Background(), args[1:], os.Stdout, os.Stderr))
		case "validate":
			os.Exit(validatecmd.Run(args[1:], os.Stdout, os.Stderr))
		case "serve":
			os.Exit(servecmd.Run(context.Background(), args[1:], os.Stdout, os.Stderr))
		case "help", "-h", "--help":
			printUsage()
			return
		}
	}

	// Default: start the server (supports `g3 -config ...`).
	os.Exit(servecmd.Run(context.Background(), args, os.Stdout, os.Stderr))
}

// printUsage prints available subcommands.
func printUsage() {
	fmt.Println(`Usage: g3 [command]

Commands:
  serve      Start the S3 gateway server (default)
  auth       Obtain a refresh token via OAuth2 browser flow
  sync       Rebuild the metadata index from Gmail
  validate   Validate configuration file
  version    Print version information
  help       Show this help message

Flags:
  -config string   Path to config file (default "config.yaml")`)
}

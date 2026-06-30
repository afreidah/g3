// -------------------------------------------------------------------------------
// g3 - Version Subcommand Tests
//
// Author: Alex Freidah
// -------------------------------------------------------------------------------

package versioncmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	var out bytes.Buffer
	code := Run(&out)

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.HasPrefix(out.String(), "g3 ") {
		t.Errorf("output = %q, want prefix %q", out.String(), "g3 ")
	}
	if !strings.Contains(out.String(), "go") {
		t.Errorf("output = %q missing Go version", out.String())
	}
}

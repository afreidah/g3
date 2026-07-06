// -------------------------------------------------------------------------------
// g3 - Validate Subcommand Tests
//
// Author: Alex Freidah
// -------------------------------------------------------------------------------

package validatecmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/afreidah/g3/internal/config"
)

func TestRun_Valid(t *testing.T) {
	loadConfig = func(string) (*config.Config, error) { return &config.Config{}, nil }
	t.Cleanup(func() { loadConfig = config.LoadConfig })

	var out, errOut bytes.Buffer
	code := Run([]string{"-config", "x.yaml"}, &out, &errOut)

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "configuration is valid") {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestRun_InvalidConfig(t *testing.T) {
	loadConfig = func(string) (*config.Config, error) { return nil, errors.New("bad config") }
	t.Cleanup(func() { loadConfig = config.LoadConfig })

	var out, errOut bytes.Buffer
	code := Run(nil, &out, &errOut)

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "validation failed") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestRun_BadFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run([]string{"-nonexistent"}, &out, &errOut)

	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

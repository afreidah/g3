// -------------------------------------------------------------------------------
// g3 - Auth CLI Injection Seam
//
// Author: Alex Freidah
//
// openBrowser is a package variable so tests can replace the real exec-based
// browser launcher with a stub, keeping the auth flow free of side effects.
// -------------------------------------------------------------------------------

package authcmd

import (
	"os/exec"
	"runtime"
)

// openBrowser attempts to open a URL in the default browser. It is a package
// variable so tests can swap in a stub.
var openBrowser = func(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

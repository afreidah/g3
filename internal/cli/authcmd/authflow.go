// -------------------------------------------------------------------------------
// g3 - Auth Flow Helpers
//
// Author: Alex Freidah
//
// Flag parsing and the OAuth redirect handler for the auth command, factored
// out of the live-flow orchestration so they are unit-testable.
// -------------------------------------------------------------------------------

package authcmd

import (
	"flag"
	"fmt"
	"io"
	"net/http"
)

// authFlags holds the parsed auth subcommand flags.
type authFlags struct {
	clientID     string
	clientSecret string
	port         int
}

// parseAuthFlags parses the auth subcommand arguments. ok is false when the
// flags fail to parse or required credentials are missing.
func parseAuthFlags(args []string, stderr io.Writer) (authFlags, bool) {
	fs := flag.NewFlagSet("auth", flag.ContinueOnError)
	fs.SetOutput(stderr)
	clientID := fs.String("client-id", "", "Google OAuth2 client ID")
	clientSecret := fs.String("client-secret", "", "Google OAuth2 client secret")
	port := fs.Int("port", 0, "localhost port for redirect (0 = auto)")
	if err := fs.Parse(args); err != nil {
		return authFlags{}, false
	}

	if *clientID == "" || *clientSecret == "" {
		fmt.Fprintln(stderr, "Usage: g3 auth --client-id <id> --client-secret <secret>")
		return authFlags{}, false
	}
	return authFlags{clientID: *clientID, clientSecret: *clientSecret, port: *port}, true
}

// newCallbackHandler returns the OAuth redirect handler. On success it sends the
// authorization code to codeCh; on failure it sends an error to errCh.
func newCallbackHandler(codeCh chan<- string, errCh chan<- error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			errMsg := r.URL.Query().Get("error")
			if errMsg == "" {
				errMsg = "no authorization code in response"
			}
			_, _ = fmt.Fprintf(w, "Authorization failed: %s\nYou can close this tab.", errMsg)
			errCh <- fmt.Errorf("authorization failed: %s", errMsg)
			return
		}
		_, _ = fmt.Fprint(w, "Authorization successful. You can close this tab.")
		codeCh <- code
	}
}

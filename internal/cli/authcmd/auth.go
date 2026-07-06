// -------------------------------------------------------------------------------
// g3 - Auth Subcommand
//
// Author: Alex Freidah
//
// Performs an OAuth2 authorization code flow with a temporary localhost server
// to capture the redirect, exchanges the code for a refresh token, and prints
// it to stdout. Flag parsing and the callback handler are split into pure
// functions so they are unit-testable; Run orchestrates the live OAuth flow.
// -------------------------------------------------------------------------------

package authcmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/gmail/v1"
)

// authTimeout bounds how long Run waits for the user to complete authorization.
var authTimeout = 5 * time.Minute

// Run performs the OAuth2 authorization code flow and prints the refresh token
// to stdout, returning the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	flags, ok := parseAuthFlags(args, stderr)
	if !ok {
		return 1
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", flags.port))
	if err != nil {
		fmt.Fprintf(stderr, "failed to start listener: %v\n", err)
		return 1
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/callback", actualPort)

	oauthCfg := &oauth2.Config{
		ClientID:     flags.clientID,
		ClientSecret: flags.clientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  redirectURL,
		Scopes:       []string{gmail.GmailModifyScope, drive.DriveFileScope},
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", newCallbackHandler(codeCh, errCh))
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("callback server: %w", err)
		}
	}()

	authURL := oauthCfg.AuthCodeURL("g3-auth", oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Fprintf(stderr, "\nOpening browser for Google authorization...\n\nIf the browser doesn't open, visit this URL:\n\n  %s\n\n", authURL)
	openBrowser(authURL)

	fmt.Fprintln(stderr, "Waiting for authorization...")
	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	case <-time.After(authTimeout):
		fmt.Fprintln(stderr, "authorization timed out")
		return 1
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)

	tok, err := oauthCfg.Exchange(context.Background(), code)
	if err != nil {
		fmt.Fprintf(stderr, "token exchange failed: %v\n", err)
		return 1
	}
	if tok.RefreshToken == "" {
		fmt.Fprintln(stderr, "no refresh token received — revoke access at https://myaccount.google.com/permissions and try again")
		return 1
	}

	fmt.Fprint(stdout, tok.RefreshToken)
	fmt.Fprintln(stderr, "\nRefresh token printed to stdout. Add it to your g3 config as gmail.refresh_token.")
	return 0
}

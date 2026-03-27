// -------------------------------------------------------------------------------
// Auth - OAuth2 Authorization Code Flow for Gmail API
//
// Author: Alex Freidah
//
// Implements the `g3 auth` subcommand that performs an OAuth2 authorization
// code flow with a temporary localhost HTTP server to handle the redirect.
// The user's browser opens the Google consent screen; after approval, Google
// redirects to localhost where g3 captures the authorization code, exchanges
// it for a refresh token, and prints the token to stdout.
// -------------------------------------------------------------------------------

package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
)

// runAuth performs the OAuth2 authorization code flow and prints the refresh
// token to stdout.
func runAuth() { // codecov:ignore -- interactive CLI flow
	fs := flag.NewFlagSet("auth", flag.ExitOnError)
	clientID := fs.String("client-id", "", "Google OAuth2 client ID")
	clientSecret := fs.String("client-secret", "", "Google OAuth2 client secret")
	port := fs.Int("port", 0, "localhost port for redirect (0 = auto)")
	_ = fs.Parse(os.Args[2:])

	if *clientID == "" || *clientSecret == "" {
		fmt.Fprintln(os.Stderr, "Usage: g3 auth --client-id <id> --client-secret <secret>")
		os.Exit(1)
	}

	// Bind a listener to get the actual port (handles port 0 = auto)
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start listener: %v\n", err)
		os.Exit(1)
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/callback", actualPort)

	oauthCfg := &oauth2.Config{
		ClientID:     *clientID,
		ClientSecret: *clientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  redirectURL,
		Scopes:       []string{gmail.GmailModifyScope},
	}

	// Channel to receive the authorization code from the callback
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
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
	})

	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("callback server: %w", err)
		}
	}()

	// Generate and open the authorization URL
	authURL := oauthCfg.AuthCodeURL("g3-auth", oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Fprintf(os.Stderr, "\nOpening browser for Google authorization...\n\nIf the browser doesn't open, visit this URL:\n\n  %s\n\n", authURL)
	openBrowser(authURL)

	// Wait for the callback
	fmt.Fprintln(os.Stderr, "Waiting for authorization...")
	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	case <-time.After(5 * time.Minute):
		fmt.Fprintln(os.Stderr, "authorization timed out after 5 minutes")
		os.Exit(1)
	}

	// Shut down the callback server
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)

	// Exchange the code for a token
	tok, err := oauthCfg.Exchange(context.Background(), code)
	if err != nil {
		fmt.Fprintf(os.Stderr, "token exchange failed: %v\n", err)
		os.Exit(1)
	}

	if tok.RefreshToken == "" {
		fmt.Fprintln(os.Stderr, "no refresh token received — if you previously authorized this app, revoke access at https://myaccount.google.com/permissions and try again")
		os.Exit(1)
	}

	// Print refresh token to stdout
	fmt.Print(tok.RefreshToken)
	fmt.Fprintln(os.Stderr, "\nRefresh token printed to stdout. Add it to your g3 config as gmail.refresh_token.")
}

// openBrowser attempts to open a URL in the default browser.
func openBrowser(url string) { // codecov:ignore -- platform-specific browser launch
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

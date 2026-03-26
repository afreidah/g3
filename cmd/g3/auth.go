// -------------------------------------------------------------------------------
// Auth - OAuth2 Device Code Flow for Gmail API
//
// Author: Alex Freidah
//
// Implements the `g3 auth` subcommand that performs an OAuth2 device code flow
// to obtain a refresh token for the Gmail API. The user visits a URL, enters
// a code, and approves access. The resulting refresh token is printed to
// stdout for the user to place in their config, environment, or secret store.
// -------------------------------------------------------------------------------

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

const (
	googleDeviceCodeURL = "https://oauth2.googleapis.com/device/code"
	googleTokenURL      = "https://oauth2.googleapis.com/token"
	gmailModifyScope    = "https://www.googleapis.com/auth/gmail.modify"
)

// deviceCodeResponse holds the response from Google's device code endpoint.
type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// tokenResponse holds the response from Google's token endpoint.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
}

// runAuth performs the OAuth2 device code flow and prints the refresh token.
func runAuth() { // codecov:ignore -- interactive CLI flow
	fs := flag.NewFlagSet("auth", flag.ExitOnError)
	clientID := fs.String("client-id", "", "Google OAuth2 client ID")
	clientSecret := fs.String("client-secret", "", "Google OAuth2 client secret")
	_ = fs.Parse(os.Args[2:])

	if *clientID == "" || *clientSecret == "" {
		fmt.Fprintln(os.Stderr, "Usage: g3 auth --client-id <id> --client-secret <secret>")
		os.Exit(1)
	}

	ctx := context.Background()

	// Request device code
	resp, err := http.PostForm(googleDeviceCodeURL, url.Values{
		"client_id": {*clientID},
		"scope":     {gmailModifyScope},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to request device code: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read response: %v\n", err)
		os.Exit(1)
	}

	var dcResp deviceCodeResponse
	if err := json.Unmarshal(body, &dcResp); err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse device code response: %v\n", err)
		os.Exit(1)
	}

	if dcResp.VerificationURL == "" {
		fmt.Fprintf(os.Stderr, "unexpected response from Google: %s\n", string(body))
		os.Exit(1)
	}

	// Prompt user
	fmt.Fprintf(os.Stderr, "\nVisit this URL on any device:\n\n  %s\n\nEnter code: %s\n\nWaiting for approval...\n", dcResp.VerificationURL, dcResp.UserCode)

	// Poll for token
	interval := time.Duration(dcResp.Interval) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(dcResp.ExpiresIn) * time.Second)

	for {
		if time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr, "authorization timed out")
			os.Exit(1)
		}

		select {
		case <-ctx.Done():
			os.Exit(1)
		case <-time.After(interval):
		}

		tokResp, err := pollToken(*clientID, *clientSecret, dcResp.DeviceCode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "token poll error: %v\n", err)
			os.Exit(1)
		}

		if tokResp.Error == "authorization_pending" {
			continue
		}
		if tokResp.Error == "slow_down" {
			interval += 5 * time.Second
			continue
		}
		if tokResp.Error != "" {
			fmt.Fprintf(os.Stderr, "authorization failed: %s\n", tokResp.Error)
			os.Exit(1)
		}

		if tokResp.RefreshToken == "" {
			fmt.Fprintln(os.Stderr, "no refresh token in response (this can happen if you previously authorized this app — revoke access at https://myaccount.google.com/permissions and try again)")
			os.Exit(1)
		}

		// Print refresh token to stdout
		fmt.Print(tokResp.RefreshToken)
		fmt.Fprintln(os.Stderr, "\nRefresh token printed to stdout. Add it to your g3 config as gmail.refresh_token.")
		return
	}
}

// pollToken exchanges the device code for a token.
func pollToken(clientID, clientSecret, deviceCode string) (*tokenResponse, error) {
	resp, err := http.PostForm(googleTokenURL, url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"device_code":   {deviceCode},
		"grant_type":    {"urn:ietf:params:oauth:grant-type:device_code"},
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var tokResp tokenResponse
	if err := json.Unmarshal(body, &tokResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	return &tokResp, nil
}

// -------------------------------------------------------------------------------
// g3 - Sync CLI Consumer Interfaces
//
// Author: Alex Freidah
//
// The sync command depends on a narrow gmailAPI interface rather than the full
// *gmail.Service, so the scan/index flow can be unit-tested with a stub instead
// of a live Gmail connection. newGmailClient and openStore are the injection
// seams: production builds the real client and store, tests override them.
// -------------------------------------------------------------------------------

package synccmd

import (
	"context"

	"github.com/afreidah/g3/internal/cli"
	"github.com/afreidah/g3/internal/config"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// gmailAPI is the subset of Gmail operations the sync command uses. The user is
// bound by the implementation, so callers pass only operation parameters.
type gmailAPI interface {
	ListLabels(ctx context.Context) ([]*gmail.Label, error)
	ListMessages(ctx context.Context, query, pageToken string, maxResults int64) (msgs []*gmail.Message, nextPageToken string, err error)
	GetMessage(ctx context.Context, id, format string) (*gmail.Message, error)
}

// gmailService adapts *gmail.Service to the gmailAPI interface for a fixed user.
type gmailService struct {
	svc  *gmail.Service
	user string
}

func (g *gmailService) ListLabels(ctx context.Context) ([]*gmail.Label, error) {
	resp, err := g.svc.Users.Labels.List(g.user).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	return resp.Labels, nil
}

func (g *gmailService) ListMessages(ctx context.Context, query, pageToken string, maxResults int64) ([]*gmail.Message, string, error) {
	req := g.svc.Users.Messages.List(g.user).Q(query).MaxResults(maxResults)
	if pageToken != "" {
		req = req.PageToken(pageToken)
	}
	resp, err := req.Context(ctx).Do()
	if err != nil {
		return nil, "", err
	}
	return resp.Messages, resp.NextPageToken, nil
}

func (g *gmailService) GetMessage(ctx context.Context, id, format string) (*gmail.Message, error) {
	return g.svc.Users.Messages.Get(g.user, id).Format(format).Context(ctx).Do()
}

// newGmailClient builds a read-only Gmail client from the configured OAuth
// credentials. It is a package variable so tests can swap in a stub.
var newGmailClient = func(ctx context.Context, cfg *config.GmailConfig) (gmailAPI, error) {
	oauthCfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       []string{gmail.GmailReadonlyScope, drive.DriveFileScope},
	}
	tok := &oauth2.Token{RefreshToken: cfg.RefreshToken}
	svc, err := gmail.NewService(ctx, option.WithHTTPClient(oauthCfg.Client(ctx, tok)))
	if err != nil {
		return nil, err
	}
	return &gmailService{svc: svc, user: cfg.User}, nil
}

// openStore initializes the metadata store. Package variable for test injection.
var openStore = cli.InitMetadataStore

// loadConfig is the injection seam for configuration loading in tests.
var loadConfig = config.LoadConfig

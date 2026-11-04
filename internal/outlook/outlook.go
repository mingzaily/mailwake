// Package outlook implements delegated Microsoft sign-in for Exchange IMAP.
package outlook

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"golang.org/x/oauth2"
)

const Host = "outlook.office365.com"
const authority = "https://login.microsoftonline.com/common/oauth2/v2.0/"

var config = oauth2.Config{
	ClientID: "f34b673e-1ec9-4d3e-9a95-4fa38341605f",
	Scopes:   []string{"https://outlook.office.com/IMAP.AccessAsUser.All", "offline_access"},
	Endpoint: oauth2.Endpoint{DeviceAuthURL: authority + "devicecode", TokenURL: authority + "token", AuthStyle: oauth2.AuthStyleInParams},
}

var client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func providerContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, client)
}

func Start(ctx context.Context) (*oauth2.DeviceAuthResponse, error) {
	d, err := config.DeviceAuth(providerContext(ctx))
	if err != nil || d.DeviceCode == "" || d.UserCode == "" || !strings.HasPrefix(d.VerificationURI, "https://") || !d.Expiry.After(time.Now()) || d.Interval < 0 {
		return nil, fault.New("outlook_unavailable")
	}
	return d, nil
}

func Authorize(ctx context.Context, d *oauth2.DeviceAuthResponse) (*oauth2.Token, error) {
	token, err := config.DeviceAccessToken(providerContext(ctx), d)
	if err != nil {
		return nil, providerError(err, "outlook_sign_in_interrupted")
	}
	if token.RefreshToken == "" {
		return nil, fault.New(mail.OutlookReauthorize)
	}
	return token, nil
}

func providerError(err error, fallback string) error {
	var response *oauth2.RetrieveError
	if errors.As(err, &response) {
		switch response.ErrorCode {
		case "invalid_grant", "interaction_required", "authorization_declined", "access_denied", "expired_token", "bad_verification_code":
			return fault.New(mail.OutlookReauthorize)
		}
	}

	return fault.New(fallback)
}

// Grant is shared by every connection to a saved mailbox. Only the rotating
// refresh credential survives a restart. Its storage binding is installed before
// the mailbox is published to workers.
type Grant struct {
	lock   chan struct{}
	token  oauth2.Token
	rotate func(context.Context, string) error
}

func NewGrant(token oauth2.Token) *Grant {
	return &Grant{lock: make(chan struct{}, 1), token: token}
}

func (g *Grant) RefreshToken() string {
	g.lock <- struct{}{}
	defer func() { <-g.lock }()
	return g.token.RefreshToken
}

func (g *Grant) Bind(rotate func(context.Context, string) error) { g.rotate = rotate }

func (g *Grant) AccessToken(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case g.lock <- struct{}{}:
	}
	defer func() { <-g.lock }()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if g.token.Valid() {
		return g.token.AccessToken, nil
	}
	token, err := config.TokenSource(providerContext(ctx), &g.token).Token()
	if err != nil {
		return "", providerError(err, "outlook_unavailable")
	}
	if g.rotate != nil && token.RefreshToken != g.token.RefreshToken {
		if err := g.rotate(ctx, token.RefreshToken); err != nil {
			return "", fault.From(err, "database_unavailable")
		}
	}
	g.token = *token
	return token.AccessToken, nil
}

package outlook

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"golang.org/x/oauth2"
)

func TestDeviceConsentAndConcurrentTokenRenewal(t *testing.T) {
	var polls, refreshes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "POST" || r.FormValue("client_id") != config.ClientID || r.FormValue("client_secret") != "" {
			t.Error("invalid public client request")
		}
		switch r.FormValue("grant_type") {
		case "":
			if r.URL.Path != "/devicecode" || r.FormValue("scope") != "https://outlook.office.com/IMAP.AccessAsUser.All offline_access" {
				t.Error("incorrect device authorization request")
			}
			fmt.Fprint(w, `{"device_code":"private-device","user_code":"DISPLAY","verification_uri":"https://microsoft.com/devicelogin","interval":1,"expires_in":90}`)
		case "urn:ietf:params:oauth:grant-type:device_code":
			if r.FormValue("device_code") != "private-device" {
				t.Error("wrong authorization")
			}
			switch polls.Add(1) {
			case 1:
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(w, `{"error":"temporarily_unavailable"}`)
				return
			case 2:
				w.WriteHeader(400)
				fmt.Fprint(w, `{"error":"authorization_pending"}`)
				return
			}
			fmt.Fprint(w, `{"access_token":"first","refresh_token":"refresh-a","token_type":"Bearer","expires_in":3600}`)
		case "refresh_token":
			refreshes.Add(1)
			if r.FormValue("refresh_token") != "refresh-a" {
				t.Error("incorrect refresh grant")
			}
			fmt.Fprint(w, `{"access_token":"renewed","refresh_token":"refresh-b","token_type":"Bearer","expires_in":3600}`)
		default:
			t.Error("unexpected grant type")
		}
	}))
	defer server.Close()
	original := config.Endpoint
	defer func() { config.Endpoint = original }()
	config.Endpoint.DeviceAuthURL = server.URL + "/devicecode"
	config.Endpoint.TokenURL = server.URL + "/token"
	d, err := Start(t.Context())
	if err != nil || d.UserCode != "DISPLAY" {
		t.Fatal("device start", err)
	}
	if _, err := Authorize(t.Context(), d); fault.From(err, "").Code != "outlook_sign_in_interrupted" {
		t.Fatal("interrupted sign-in", err)
	}
	d, err = Start(t.Context())
	if err != nil {
		t.Fatal("restart sign-in", err)
	}
	token, err := Authorize(t.Context(), d)
	if err != nil || token.AccessToken != "first" {
		t.Fatal("device consent", err)
	}
	token.Expiry = time.Now().Add(-time.Minute)
	grant := NewGrant(*token)
	var saved string
	grant.Bind(func(_ context.Context, replacement string) error { saved = replacement; return nil })
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			access, err := grant.AccessToken(t.Context())
			if err != nil || access != "renewed" {
				t.Error("renewal", err)
			}
		})
	}
	wg.Wait()
	if saved != "refresh-b" || grant.RefreshToken() != saved || refreshes.Load() != 1 {
		t.Fatal("rotated refresh token was not shared and saved")
	}
}

func TestTokenErrorsExposeOnlyActionableCodes(t *testing.T) {
	for _, tc := range []struct{ reason, want string }{{"invalid_grant", mail.OutlookReauthorize}, {"authorization_declined", mail.OutlookReauthorize}, {"temporarily_unavailable", "outlook_unavailable"}} {
		err := providerError(&oauth2.RetrieveError{ErrorCode: tc.reason, ErrorDescription: "private provider response"}, "outlook_unavailable")
		if fault.From(err, "").Code != tc.want || err.Error() != tc.want {
			t.Fatal(err)
		}
	}
}

func TestWaitingForTokenRenewalHonorsCallerDeadline(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"renewed","expires_in":3600}`)
	}))
	defer server.Close()
	original := config.Endpoint.TokenURL
	config.Endpoint.TokenURL = server.URL
	defer func() { config.Endpoint.TokenURL = original }()

	grant := NewGrant(oauth2.Token{RefreshToken: "fixture"})
	first := make(chan error, 1)
	go func() {
		_, err := grant.AccessToken(t.Context())
		first <- err
	}()
	<-started
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	waiter := make(chan error, 1)
	go func() {
		_, err := grant.AccessToken(ctx)
		waiter <- err
	}()
	select {
	case err := <-waiter:
		if err != context.DeadlineExceeded {
			t.Errorf("waiting caller returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("waiting caller is still blocked by another caller's renewal")
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal("active renewal failed", err)
	}
	if token, err := grant.AccessToken(t.Context()); err != nil || token != "renewed" {
		t.Fatal("renewed credential unavailable", err)
	}
	if _, err := grant.AccessToken(ctx); err != context.DeadlineExceeded {
		t.Errorf("expired caller received cached credential, error %v", err)
	}
}

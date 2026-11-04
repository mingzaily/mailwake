package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/outlook"
	"github.com/mingzaily/mailwake/internal/settings"
)

// Only Microsoft's device and token endpoints are reachable in this test.
type microsoftFixture struct{}

func (microsoftFixture) RoundTrip(req *http.Request) (*http.Response, error) {
	var body string
	switch req.URL.String() {
	case "https://login.microsoftonline.com/common/oauth2/v2.0/devicecode":
		body = `{"device_code":"private-device","user_code":"DISPLAY","verification_uri":"https://microsoft.com/devicelogin","interval":1,"expires_in":60}`
	case "https://login.microsoftonline.com/common/oauth2/v2.0/token":
		body = `{"access_token":"private-access","refresh_token":"private-refresh","expires_in":3600}`
	default:
		panic("unexpected network destination")
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

type outlookMailboxFixture struct{ testRuntime }

func (outlookMailboxFixture) CreateMailbox(_ context.Context, input settings.MailboxCreate) (settings.MailboxView, error) {
	m, err := settings.NewMailbox().Merge(input.MailboxUpdate)
	return m.View(0), err
}
func (outlookMailboxFixture) TestMailbox(_ context.Context, _ string, input settings.MailboxUpdate) (*mail.FolderDiscovery, error) {
	_, err := settings.NewMailbox().Merge(input)
	return &mail.FolderDiscovery{Folders: []string{"INBOX"}}, err
}

func TestMicrosoftAuthorizationIsScopedToTheConsoleSessionAndMailbox(t *testing.T) {
	f := newAppFixture(t, outlookMailboxFixture{})
	transport := http.DefaultTransport
	http.DefaultTransport = microsoftFixture{}
	defer func() { http.DefaultTransport = transport }()
	const start = "/api/v1/outlook/sign-in"
	const test = "/api/v1/mailboxes/test"
	const user = "owner@example.test"
	setup := map[string]string{"username": user}
	if w := f.do(t, "POST", start, setup, map[string]string{}); w.Code != 401 {
		t.Fatal("anonymous", w.Code)
	}
	w := f.do(t, "POST", start, setup, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "private-") {
		t.Fatal("unsafe device start", w.Code)
	}
	var device struct {
		ID   string `json:"authorization_id"`
		Code string `json:"user_code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &device); err != nil || device.ID == "" || device.Code != "DISPLAY" {
		t.Fatal("invalid device response", err)
	}
	draft := map[string]any{"host": outlook.Host, "port": 993, "username": user, "outlook_authorization": device.ID}
	check := func(path string, headers map[string]string, code int, reason string) {
		t.Helper()
		w := f.do(t, "POST", path, draft, headers)
		if w.Code != code || !strings.Contains(w.Body.String(), reason) || strings.Contains(w.Body.String(), "private-") {
			t.Fatalf("response %d, wanted %d and %s", w.Code, code, reason)
		}
	}
	check(test, nil, 409, "outlook_pending")
	draft["username"] = "other@example.test"
	check(test, nil, 400, "outlook_reauthorize")
	draft["username"] = user
	check("/api/v1/mailboxes/another/test", nil, 400, "outlook_reauthorize")
	login := f.do(t, "POST", "/api/v1/session", map[string]string{"username": "admin", "password": "test password long enough"}, map[string]string{})
	var other struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &other); err != nil || len(login.Result().Cookies()) != 1 {
		t.Fatal("second session", err)
	}
	check(test, map[string]string{"Origin": "http://example.com", "Cookie": login.Result().Cookies()[0].String(), "X-CSRF-Token": other.CSRF}, 400, "outlook_reauthorize")
	deadline := time.Now().Add(5 * time.Second)
	for {
		w = f.do(t, "POST", test, draft, nil)
		if w.Code == 200 && strings.Contains(w.Body.String(), "INBOX") {
			break
		}
		if w.Code != 409 || time.Now().After(deadline) {
			t.Fatal("authorized test", w.Code, w.Body.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	draft["host"] = "other.example.test"
	check(test, nil, 400, "outlook_account_invalid")
	draft["host"] = outlook.Host
	check("/api/v1/mailboxes", nil, 201, `"outlook":true`)
	check(test, nil, 400, "outlook_reauthorize")
}

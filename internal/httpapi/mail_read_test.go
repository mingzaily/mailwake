package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mailview"
)

type mailReadRuntime struct {
	testRuntime
	read func(mailview.Reader, mailview.MessageRequest) (mailview.Messages, error)
}

func (r mailReadRuntime) MailMessages(_ context.Context, reader mailview.Reader, input mailview.MessageRequest) (mailview.Messages, error) {
	return r.read(reader, input)
}

func TestMailCapabilitiesExposeCurrentContentPermission(t *testing.T) {
	f := newAppFixture(t)
	for _, scope := range []string{"content", "mailboxes", "diagnostics"} {
		credential, _ := f.invite(t, []string{scope})
		w := f.do(t, "GET", appPrefix+"/capabilities", nil, map[string]string{"Authorization": "Bearer " + credential})
		var result mailview.Capabilities
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Permissions.Content != (scope == "content") || result.MailHistory.Version != 1 || result.MailHealth.Version != 1 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("capabilities: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestMailReadValidatesRequestsAndMapsErrors(t *testing.T) {
	code := ""
	runtime := mailReadRuntime{read: func(reader mailview.Reader, input mailview.MessageRequest) (mailview.Messages, error) {
		if reader.ControllerID == "" || reader.DeviceID == "" || input.Limit != 50 || input.AccountID != "account" {
			t.Fatal("request context missing")
		}
		if code != "" {
			return mailview.Messages{}, fault.New(code)
		}
		return mailview.Messages{Page: mailview.Page[mailview.Message]{Items: []mailview.Message{}, Complete: true}}, nil
	}}
	f := newAppFixture(t, runtime)
	credential, _ := f.invite(t, []string{"content"})
	headers := map[string]string{"Authorization": "Bearer " + credential}
	for _, body := range []any{
		map[string]any{},
		map[string]any{"mailbox_id": "box", "folder": "INBOX"},
		map[string]any{"mailbox_id": "box", "account_id": "account", "folder": "INBOX", "limit": 101},
		map[string]any{"mailbox_id": "box", "account_id": "account", "folder": "INBOX", "limit": -1},
		map[string]any{"mailbox_id": "box", "account_id": "account", "folder": "INBOX", "limit": 0},
		map[string]any{"mailbox_id": "box", "account_id": "account", "folder": "INBOX", "since": "arbitrary"},
	} {
		if w := f.do(t, "POST", appPrefix+"/mail/messages", body, headers); w.Code != 400 {
			t.Fatalf("invalid request accepted: %d %s", w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/mail/folders", "/mail/health"} {
		for _, limit := range []string{"0", "-1", "101", "all", ""} {
			if w := f.do(t, "GET", appPrefix+path+"?limit="+limit, nil, headers); w.Code != 400 {
				t.Fatalf("invalid limit: %d %s", w.Code, w.Body.String())
			}
		}
	}
	body := map[string]any{"mailbox_id": "box", "account_id": "account", "folder": "INBOX"}
	for _, tc := range []struct {
		code   string
		status int
	}{{"", 200}, {"sync_cursor_invalid", 409}, {"sync_page_too_large", 413}, {"message_not_found", 404}, {"message_unavailable", 503}, {"message_timeout", 504}} {
		code = tc.code
		w := f.do(t, "POST", appPrefix+"/mail/messages", body, headers)
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("read: %d %s", w.Code, w.Body.String())
		}
		if code != "" && !strings.Contains(w.Body.String(), `"code":"`+code+`"`) {
			t.Fatalf("wrong code: %s", w.Body.String())
		}
	}
	if w := f.do(t, "DELETE", appPrefix+"/controller", nil, headers); w.Code != 204 {
		t.Fatal(w.Code)
	}
	for _, path := range []string{"/capabilities", "/mail/folders", "/mail/health"} {
		if w := f.do(t, "GET", appPrefix+path, nil, headers); w.Code != 401 {
			t.Fatal("revoked read accepted", path, w.Code)
		}
	}
	if w := f.do(t, "POST", appPrefix+"/mail/messages", body, headers); w.Code != 401 {
		t.Fatal("revoked message read accepted", w.Code)
	}
}

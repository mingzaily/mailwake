package pushover

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/event"
)

func TestPushoverRequestAndClassification(t *testing.T) {
	for _, tc := range []struct {
		name           string
		status         int
		body           string
		retry, success bool
	}{
		{"accepted", 200, `{"status":1,"request":"r"}`, false, true},
		{"invalid user", 400, `{"status":0,"errors":["user identifier secret-user-key is invalid"]}`, false, false},
		{"monthly limit", 429, `{"status":0}`, true, false},
		{"temporary failure", 503, `secret-app-token`, true, false},
		{"malformed", 200, `broken`, true, false},
		{"redirect", 302, ``, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get(delivery.HeaderEvent) != "test_fixture" {
					t.Error("outgoing test ID changed")
				}
				if err := r.ParseForm(); err != nil || r.Method != http.MethodPost {
					t.Errorf("request: %s %v", r.Method, err)
				}
				if r.PostForm.Get("token") != "secret-app-token" || r.PostForm.Get("user") != "secret-user-key" {
					t.Errorf("credentials: %v", r.PostForm)
				}
				if r.PostForm.Get("title") != "Work mailbox · Clients" || r.PostForm.Get("message") != "Hello" {
					t.Errorf("content: %v", r.PostForm)
				}
				if tc.status == 302 {
					http.Redirect(w, r, "https://example.org/", http.StatusFound)
					return
				}
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()
			s := New(server.URL, "secret-app-token", "secret-user-key", "en")
			s.client.Transport = server.Client().Transport
			err := s.Send(t.Context(), event.Notification{ID: "test_fixture", Account: "Work mailbox", MailboxID: "mbx_work", Folder: "Clients", Sender: "a@example.org", Subject: "Hello"})
			if tc.success {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var failure *delivery.Failure
			if !errors.As(err, &failure) || failure.Retryable != tc.retry {
				t.Fatalf("classification: %+v", err)
			}
			if !strings.HasPrefix(failure.Code, "pushover_") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("failure must be a safe pushover code: %v", err)
			}
		})
	}
}

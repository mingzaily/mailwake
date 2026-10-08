package webhook

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/event"
)

// A fixed low-entropy fixture; real deployments use a random 32-byte secret.
var secret = strings.Repeat("s", 32)

func TestWebhookSignsVersionedPayload(t *testing.T) {
	received := time.Date(2026, 9, 28, 10, 32, 0, 0, time.UTC)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get(HeaderSignature) != Sign(secret, r.Header.Get(HeaderTimestamp), body) {
			t.Error("signature does not verify")
		}
		if r.Header.Get(HeaderTimestamp) != "1790000000" || r.Header.Get(HeaderEvent) != "evt_1" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("headers: %v", r.Header)
		}
		var p Payload
		if err := json.Unmarshal(body, &p); err != nil {
			t.Fatal(err)
		}
		if p.Version != 1 || p.ID != "evt_1" || p.Folder != "Clients" || p.Sender != "a@example.org" || p.Subject != "Hello" || !p.ReceivedAt.Equal(received) || p.Message != "Hello" || p.Title != "Work mailbox · Clients" || p.Account != "Work mailbox" || p.MailboxID != "mbx_work" {
			t.Errorf("payload: %+v", p)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	s := New(server.URL+"/hooks/mail?token=route", secret, "en")
	s.client.Transport = server.Client().Transport
	s.now = func() time.Time { return time.Unix(1790000000, 0) }
	if err := s.Send(t.Context(), event.Notification{ID: "evt_1", Account: "Work mailbox", MailboxID: "mbx_work", Folder: "Clients", Sender: "a@example.org", Subject: "Hello", ReceivedAt: received}); err != nil {
		t.Fatal(err)
	}
}

func TestSignatureDependsOnSecretTimestampAndBody(t *testing.T) {
	base := Sign(secret, "1", []byte("{}"))
	for _, other := range []string{Sign("another-secret-another-secret-00", "1", []byte("{}")), Sign(secret, "2", []byte("{}")), Sign(secret, "1", []byte("{ }"))} {
		if other == base {
			t.Fatal("signature ignores an input")
		}
	}
	if !strings.HasPrefix(base, "sha256=") || len(base) != len("sha256=")+64 {
		t.Fatalf("format: %s", base)
	}
}

func TestWebhookClassificationAndRedirects(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	for _, tc := range []struct {
		name   string
		status int
		retry  bool
	}{
		{"rejected", 403, false},
		{"rate limited", 429, true},
		{"unavailable", 502, true},
		{"redirect", 307, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status == 307 {
					http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
					return
				}
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			s := New(server.URL, secret, "en")
			s.client.Transport = server.Client().Transport
			var failure *delivery.Failure
			if err := s.Send(t.Context(), event.Notification{ID: "evt"}); !errors.As(err, &failure) || failure.Retryable != tc.retry || failure.Code != "webhook_http_error" {
				t.Fatalf("classification: %+v", err)
			}
		})
	}
	if forwarded.Load() != 0 {
		t.Fatal("a redirect forwarded the signed event")
	}
}

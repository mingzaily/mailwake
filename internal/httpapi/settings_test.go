package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/runtime"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestSettingsHTTPRedactsAllCredentials(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v, err := settings.OpenVault(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	mailbox, err := v.Seal(storage.MailboxConfigurationName("mbx_primary"), []byte(`{"id":"mbx_primary","label":"user","host":"imap.example.test","port":993,"username":"user","password":"private-mail-password"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMailboxRecord(t.Context(), "mbx_primary", 0, mailbox, false); err != nil {
		t.Fatal(err)
	}
	for name, plain := range map[string]string{
		"delivery": `{"channel":"webhook","preview":"off","retry_count":0,"language":"en","bark":{"key":"private-bark"},"pushover":{"token":"private-app","user":"private-user"},"webhook":{"url":"https://hooks.test/private-route","secret":"private-signing-secret-long-enough"}}`,
	} {
		encrypted, err := v.Seal(name, []byte(plain))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SaveConfiguration(t.Context(), name, encrypted); err != nil {
			t.Fatal(err)
		}
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	configuration, err := runtime.New(t.Context(), s, v, log, "")
	if err != nil {
		t.Fatal(err)
	}
	defer configuration.Close()
	a, g := testAdministrator(t, s)
	r := New(a, configuration, s, log)
	for _, path := range []string{"mailboxes/mbx_primary", "settings/delivery"} {
		req := httptest.NewRequest("GET", "/api/v1/"+path, nil)
		authorizeSession(req, g)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 || strings.Contains(w.Body.String(), "private-") || !strings.Contains(w.Body.String(), `"configured":true`) {
			t.Fatal(w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		req = httptest.NewRequest("PUT", "/api/v1/"+path, strings.NewReader(`{"unknown":"private-secret"}`))
		authorizeSession(req, g)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 400 || strings.Contains(w.Body.String(), "private-secret") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestStatusReportsRecoverableConfiguration(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	vault, err := settings.OpenVault(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := vault.Seal("delivery", []byte(`{"channel":"invalid-private-value"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfiguration(t.Context(), "delivery", encrypted); err != nil {
		t.Fatal(err)
	}
	for name, plain := range map[string]string{
		"mailbox:mbx_broken": `{}`,
		"mailbox:mbx_full":   `{"id":"mbx_full","label":"Mailbox","host":"imap.invalid","port":993,"username":"user","password":"private-password","connection_limit":2}`,
	} {
		encrypted, err := vault.Seal(name, []byte(plain))
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SaveConfiguration(t.Context(), name, encrypted); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := seedSubscriptions(t.Context(), store, "mbx_full", []string{"one", "two"}); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	configuration, err := runtime.New(t.Context(), store, vault, log, "")
	if err != nil {
		t.Fatal(err)
	}
	defer configuration.Close()
	service, grant := testAdministrator(t, store)
	router := New(service, configuration, store, log)
	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	authorizeSession(req, grant)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "configuration_invalid") || strings.Contains(w.Body.String(), "invalid-private-value") {
		t.Fatal(w.Code, w.Body.String())
	}
	var status struct {
		Notices map[string]localizedError `json:"notices"`
		Folders []struct {
			MailboxID    string `json:"mailbox_id"`
			MailboxLabel string `json:"mailbox_label"`
		} `json:"folders"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if len(status.Notices) != 3 || status.Notices["mailbox:mbx_broken"].Code != "configuration_invalid" || status.Notices["subscriptions:mbx_full"].Code != "connection_budget_exceeded" || status.Notices["delivery"].Code != "configuration_invalid" {
		t.Fatal(status.Notices)
	}
	if len(status.Folders) != 2 || status.Folders[0].MailboxID != "mbx_full" || status.Folders[0].MailboxLabel != "Mailbox" {
		t.Fatal(status.Folders)
	}

}

func TestDeliveryDeleteUsesOwnerAuthenticationAndRevisionCAS(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	vault, err := settings.OpenVault(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := vault.Seal("delivery", []byte(`{"channel":"webhook","preview":"off","retry_count":0,"language":"en","webhook":{"url":"https://hooks.example.test/private","secret":"synthetic-owner-secret-at-least-thirty-two"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfiguration(t.Context(), "delivery", sealed); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager, err := runtime.New(t.Context(), store, vault, logger, "")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	service, grant := testAdministrator(t, store)
	router := New(service, manager, store, logger)
	for _, tc := range []struct {
		name, body          string
		authenticated, csrf bool
		status              int
	}{
		{"anonymous", `{"revision":0}`, false, false, 401},
		{"csrf", `{"revision":0}`, true, false, 403},
		{"unknown field", `{"revision":0,"channel":"webhook"}`, true, true, 400},
		{"missing revision", `{}`, true, true, 400},
		{"stale revision", `{"revision":0}`, true, true, 409},
		{"current revision", `{"revision":1}`, true, true, 204},
		{"repeated stale revision", `{"revision":1}`, true, true, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("DELETE", "/api/v1/settings/delivery", strings.NewReader(tc.body))
			if tc.authenticated {
				authorizeSession(req, grant)
			}
			if !tc.csrf {
				req.Header.Del("X-CSRF-Token")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "private") {
				t.Fatal("secret exposed")
			}
		})
	}
	if manager.Channel() != "" || manager.DeliveryView()["revision"] != int64(2) {
		t.Fatal("delivery was not cleared exactly once")
	}
}

package auth

import (
	"bytes"
	"encoding/json"
	"github.com/mingzaily/mailwake/internal/logging"
	"github.com/mingzaily/mailwake/internal/storage"
	"strings"
	"testing"
)

func TestAdministrativeLogsKeepSecretsPrivate(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var output bytes.Buffer
	log := logging.New(&output)
	s := New(store, log)
	code, err := s.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	password, next := "synthetic-old-password", "synthetic-new-password"
	grant, err := s.Setup(t.Context(), "private-source", code, "private-administrator", password)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Login(t.Context(), "private-source", "private-administrator", "synthetic-wrong-password")
	if err == nil {
		t.Fatal("invalid password accepted")
	}
	logged, err := s.Login(t.Context(), "private-source", "private-administrator", password)
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.CreateToken(t.Context(), "private-token-name")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeToken(t.Context(), token.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangePassword(t.Context(), "private-source", digest(grant.ID), password, next); err != nil {
		t.Fatal(err)
	}
	page := logging.Read(log, logging.Query{})
	data, _ := json.Marshal(page)
	for _, event := range []string{"Administrator authentication failed", "Administrator login succeeded", "API token created", "API token revoked", "Administrator password changed"} {
		if !strings.Contains(string(data), event) {
			t.Fatalf("missing audit event %s", event)
		}
	}
	for _, value := range []string{code, password, next, "synthetic-wrong-password", grant.ID, grant.CSRF, logged.ID, logged.CSRF, token.Value, "private-source", "private-administrator", "private-token-name"} {
		if strings.Contains(output.String(), value) || strings.Contains(string(data), value) {
			t.Fatal("administrative secret logged")
		}
	}
}

package auth

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/storage"
)

func TestAuthenticationFailureUsesInjectedLogger(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var output bytes.Buffer
	service := New(store, slog.New(slog.NewJSONHandler(&output, nil)))
	code, err := service.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Setup(t.Context(), "peer", code, "admin", "synthetic-password"); err != nil {
		t.Fatal(err)
	}
	password := strings.Repeat("x", 1025)
	if _, err := service.Login(t.Context(), "peer", "admin", password); err == nil {
		t.Fatal("invalid password accepted")
	}
	if !strings.Contains(output.String(), `"count":1`) {
		t.Fatal("injected logger received no failure")
	}
	if strings.Contains(output.String(), password) || strings.Contains(output.String(), code) {
		t.Fatal("credentials logged")
	}
}

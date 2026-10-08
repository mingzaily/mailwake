package auth

import (
	"github.com/mingzaily/mailwake/internal/storage"
	"log/slog"
	"strings"
	"testing"
)

func TestSetupCodeRotationAndSession(t *testing.T) {
	s, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := New(s, slog.Default())
	first, err := a.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.Prepare(t.Context())
	if err != nil || first == second || len(strings.ReplaceAll(second, "-", "")) != 12 {
		t.Fatal("code generation", err)
	}
	if _, err := a.Setup(t.Context(), "test-source", first, "admin", "long enough password"); err == nil {
		t.Fatal("old startup code accepted")
	}
	grant, err := a.Setup(t.Context(), "test-source", second, "admin", "long enough password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AuthenticateSession(t.Context(), grant.ID); err != nil {
		t.Fatal(err)
	}
	admin, err := s.Administrator(t.Context())
	if err != nil || !strings.HasPrefix(admin.PasswordHash, "$argon2id$") || strings.Contains(admin.PasswordHash, "long enough password") {
		t.Fatal("password storage", err)
	}
}

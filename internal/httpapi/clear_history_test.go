package httpapi

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/mingzaily/mailwake/internal/auth"
	"github.com/mingzaily/mailwake/internal/logging"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestHistoryDeletionRequiresAdministratorAuthenticationAndCSRF(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	log := logging.New(io.Discard)
	a := auth.New(store, log)
	code, err := a.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	grant, err := a.Setup(t.Context(), "peer", code, "admin", "synthetic-clear-password")
	if err != nil {
		t.Fatal(err)
	}
	token, err := a.CreateToken(t.Context(), "history cleanup")
	if err != nil {
		t.Fatal(err)
	}
	router := New(a, newTestRuntime("", nil, nil), store, log)
	for _, path := range []string{"/api/v1/logs", "/api/v1/deliveries"} {
		for _, mode := range []string{"anonymous", "missing-csrf", "session", "token"} {
			request := httptest.NewRequest("DELETE", path, nil)
			expected := 200
			switch mode {
			case "anonymous":
				expected = 401
			case "missing-csrf":
				authorizeSession(request, grant)
				request.Header.Del("X-CSRF-Token")
				expected = 403
			case "session":
				authorizeSession(request, grant)
			case "token":
				request.Header.Set("Authorization", "Bearer "+token.Value)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != expected {
				t.Fatalf("%s %s: %d %s", path, mode, response.Code, response.Body.String())
			}
		}
	}
	if page := logging.Read(log, logging.Query{}); len(page.Entries) != 0 {
		t.Fatal(page)
	}
}

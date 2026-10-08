package native

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mingzaily/mailwake/internal/delivery"
)

func TestExplicitLocalRelayTrustKeepsHostnameAndProductionBoundary(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Info{Audience: "relay.test", Environment: "sandbox", Version: 2})
	}))
	defer server.Close()
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	defaults, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = defaults.Info(t.Context()); err == nil {
		t.Fatal("unknown local CA accepted by production defaults")
	}
	configured, err := NewClientWithOptions(server.URL, ClientOptions{RootCAs: pool})
	if err != nil {
		t.Fatal(err)
	}
	if info, err := configured.Info(t.Context()); err != nil || info.Audience != "relay.test" {
		t.Fatal(info, err)
	}
	wrong, err := NewClientWithOptions(strings.Replace(server.URL, "127.0.0.1", "localhost", 1), ClientOptions{RootCAs: pool})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = wrong.Info(t.Context()); err == nil {
		t.Fatal("wrong certificate hostname accepted")
	}
	if _, err = NewClientWithOptions("https://relay.example.org", ClientOptions{RootCAs: pool}); err == nil {
		t.Fatal("local roots installed for public Relay")
	}
	if _, err = NewClientWithOptions("http://127.0.0.1:8787", ClientOptions{RootCAs: pool}); err == nil {
		t.Fatal("local TLS trust enabled on plaintext endpoint")
	}
}

func TestClientSignaturesNoncesAndRedirects(t *testing.T) {
	v := vector(t)
	identity := &Identity{Key: fixedKey(t, v.Core), PublicKey: v.Core.PublicKey, ID: v.Core.ID}
	var mu sync.Mutex
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/info":
			json.NewEncoder(w).Encode(Info{Audience: v.Audience, Environment: "sandbox", Version: 2})
		case "/redirect":
			w.Header().Set("Location", "/v1/info")
			w.WriteHeader(302)
		case "/unavailable":
			w.WriteHeader(503)
			io.WriteString(w, `{"error":{"code":"platform_unavailable","message":"private secret"}}`)
		default:
			b, _ := io.ReadAll(r.Body)
			h := r.Header
			text := requestText(v.Audience, "core", identity.ID, r.Method, r.URL.Path, h.Get("X-Relay-Timestamp"), h.Get("X-Relay-Nonce"), b, "")
			if err := verify(identity.PublicKey, h.Get("X-Relay-Signature"), text); err != nil {
				t.Error(err)
			}
			mu.Lock()
			defer mu.Unlock()
			nonce := h.Get("X-Relay-Nonce")
			if seen[nonce] {
				t.Error("nonce reused")
			}
			seen[nonce] = true
			w.WriteHeader(202)
			io.WriteString(w, `{"id":"accepted"}`)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	info, err := client.Info(context.Background())
	if err != nil || info.Audience != v.Audience {
		t.Fatal(info, err)
	}
	for range 2 {
		if err := client.call(context.Background(), identity, v.Audience, "POST", "/v1/push", map[string]string{"hello": "世界"}, nil, 202); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"/redirect", "/unavailable"} {
		err := client.call(context.Background(), identity, v.Audience, "GET", path, nil, nil, 200)
		var failure *delivery.Failure
		if !errors.As(err, &failure) {
			t.Fatal(err)
		}
		if failure.Retryable != (path == "/unavailable") {
			t.Fatal("HTTP retry policy mismatch")
		}
	}
}
func TestRelayURLBoundary(t *testing.T) {
	for _, raw := range []string{"http://example.com", "https://x.example/prefix", "https://a:b@x.example", "https://x.example?token=secret", "https://x.example#fragment"} {
		if _, err := NewClient(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, raw := range []string{"https://relay.example", "http://127.0.0.1:8787", "http://[::1]:8787"} {
		if _, err := NewClient(raw); err != nil {
			t.Fatal(err)
		}
	}
}

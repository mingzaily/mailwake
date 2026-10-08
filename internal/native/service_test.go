package native

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

func testService(t *testing.T, handler http.HandlerFunc) (*Service, *storage.Store) {
	t.Helper()
	return testServiceInDirectory(t, t.TempDir(), handler)
}
func testServiceInDirectory(t *testing.T, dir string, handler http.HandlerFunc) (*Service, *storage.Store) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	vault, err := settings.OpenVault(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	service, err := New(store, vault, server.URL, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return service, store
}
func TestPairingSelectsBeforeRevealingTokenAndResumes(t *testing.T) {
	ctx := context.Background()
	v := vector(t)
	var service *Service
	var store *storage.Store
	var claim Claim
	var expectedToken string
	posts := 0
	failOnce := true
	service, store = testService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/info" {
			json.NewEncoder(w).Encode(Info{Audience: v.Audience, Environment: "sandbox", Version: 2})
			return
		}
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]any{"claims": []Claim{{DeviceID: "forged"}, claim}})
			return
		}
		if r.Method == "DELETE" {
			w.Write([]byte(`{}`))
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		posts++
		saved, err := store.NativePairing(ctx, body["pairing_id"].(string))
		if err != nil {
			t.Error(err)
		}
		if saved.DeviceID != claim.DeviceID || saved.SigningKey != claim.SigningKey || saved.EncryptionKey != claim.EncryptionKey {
			t.Error("Token disclosed before device was durably locked")
		}
		if body["one_time_token"] != expectedToken {
			t.Error("Token changed")
		}
		if failOnce {
			failOnce = false
			w.WriteHeader(503)
			return
		}
		json.NewEncoder(w).Encode(relayPairing{saved.ID, "active", saved.DeviceID, saved.EncryptionKey})
	})
	created, err := service.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(created.URI)
	expectedToken = u.Query().Get("t")
	p, err := store.NativePairing(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := service.identity.load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	claim = Claim{v.Device.ID, v.Device.PublicKey, v.Encryption.PublicKey, "", "My phone", p.ExpiresAt}
	claim.Signature, err = sign(fixedKey(t, v.Device), consentText(v.Audience, p.ID, identity.ID, claim.DeviceID, v.Encryption.ID, p.TokenHash, strconv.FormatInt(p.ExpiresAt, 10)))
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	locked, err := store.NativePairing(ctx, p.ID)
	if err != nil || locked.State != "waiting" || locked.DeviceID != claim.DeviceID {
		t.Fatal(locked, err)
	}
	restarted, err := New(store, service.vault, service.client.url, service.log)
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	active, err := store.NativePairing(ctx, p.ID)
	if err != nil || active.State != "active" || len(active.EncryptedToken) != 0 || active.TokenHash != "" || active.DeviceSignature != "" {
		t.Fatal("terminal privacy or status", err)
	}
	if posts != 2 {
		t.Fatal(posts)
	}
	if err = restarted.Unlink(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	devices, err := restarted.Devices(ctx)
	if err != nil || len(devices) != 0 {
		t.Fatal(devices, err)
	}
}
func TestClaimValidationAndConcurrentPairingLimit(t *testing.T) {
	ctx := context.Background()
	v := vector(t)
	service, store := testService(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Info{Audience: v.Audience, Environment: "sandbox", Version: 2})
	})
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.Create(ctx)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				success++
			} else if safeCode(err) != "native_pairing_limit" {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if success != 3 {
		t.Fatal(success)
	}
	items, _ := store.NativePairings(ctx, "waiting")
	p := items[0]
	identity, _ := service.identity.load(ctx)
	info := Info{Audience: v.Audience, Environment: "sandbox", Version: 2}
	valid := Claim{v.Device.ID, v.Device.PublicKey, v.Encryption.PublicKey, "", "Phone", p.ExpiresAt}
	text := consentText(v.Audience, p.ID, identity.ID, valid.DeviceID, v.Encryption.ID, p.TokenHash, strconv.FormatInt(p.ExpiresAt, 10))
	valid.Signature, _ = sign(fixedKey(t, v.Device), text)
	if err := validateClaim(identity, info, p, valid); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"token", "core", "expired", "fingerprint", "encryption"} {
		t.Run(kind, func(t *testing.T) {
			c := valid
			q := p
			core := *identity
			switch kind {
			case "token":
				q.TokenHash = v.TokenHash
			case "core":
				core.ID = v.Device.ID
			case "expired":
				q.ExpiresAt = time.Now().Unix() - 1
				c.ExpiresAt = q.ExpiresAt
			case "fingerprint":
				c.DeviceID = v.Core.ID
			case "encryption":
				c.EncryptionKey = v.Core.PublicKey
			}
			if validateClaim(&core, info, q, c) == nil {
				t.Fatal("forged claim accepted")
			}
		})
	}
}
func TestExpiredPairingAndRelayBinding(t *testing.T) {
	ctx := context.Background()
	audience := "first"
	service, store := testService(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Info{Audience: audience, Environment: "sandbox", Version: 2})
	})
	created, err := service.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := store.NativePairing(ctx, created.ID)
	p.ExpiresAt = time.Now().Unix() - 1
	identity, _ := service.identity.load(ctx)
	if err = service.pollPairing(ctx, identity, Info{Audience: audience, Environment: "sandbox", Version: 2}, p); err != nil {
		t.Fatal(err)
	}
	saved, _ := store.NativePairing(ctx, p.ID)
	if saved.State != "expired" || len(saved.EncryptedToken) != 0 {
		t.Fatal("expired token retained")
	}
	audience = "other"
	if _, err = service.Create(ctx); safeCode(err) != "native_environment_mismatch" {
		t.Fatal(err)
	}
	disabled, _ := New(store, service.vault, "", service.log)
	if _, err = disabled.Create(ctx); safeCode(err) != "native_push_unavailable" {
		t.Fatal(err)
	}
}

func TestRelayKeySubstitutionEndsPairingAndClearsToken(t *testing.T) {
	ctx := context.Background()
	v := vector(t)
	var claim Claim
	var p storage.NativePairing
	service, store := testService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/info" {
			json.NewEncoder(w).Encode(Info{Audience: v.Audience, Environment: "sandbox", Version: 2})
			return
		}
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]any{"claims": []Claim{claim}})
			return
		}
		json.NewEncoder(w).Encode(relayPairing{p.ID, "active", v.Device.ID, v.Core.PublicKey})
	})
	created, err := service.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, _ = store.NativePairing(ctx, created.ID)
	identity, _ := service.identity.load(ctx)
	claim = Claim{v.Device.ID, v.Device.PublicKey, v.Encryption.PublicKey, "", "Phone", p.ExpiresAt}
	claim.Signature, _ = sign(fixedKey(t, v.Device), consentText(v.Audience, p.ID, identity.ID, claim.DeviceID, v.Encryption.ID, p.TokenHash, strconv.FormatInt(p.ExpiresAt, 10)))
	if err = service.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	saved, _ := store.NativePairing(ctx, p.ID)
	if saved.State != "failed" || saved.ErrorCode != "native_protocol_invalid" || len(saved.EncryptedToken) != 0 || saved.TokenHash != "" {
		t.Fatal("Relay key substitution was accepted or token survived terminal state")
	}
}
func TestExpiredTokenClearsDuringRelayOutage(t *testing.T) {
	ctx := context.Background()
	service, store := testService(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	p := storage.NativePairing{ID: "expired", ExpiresAt: time.Now().Unix() - 1, TokenHash: "fixture", EncryptedToken: []byte{1}}
	if err := store.CreateNativePairing(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := service.Poll(ctx); err == nil {
		t.Fatal("outage not reported")
	}
	saved, err := store.NativePairing(ctx, p.ID)
	if err != nil || saved.State != "expired" || len(saved.EncryptedToken) != 0 {
		t.Fatal("expired token survived outage", err)
	}
}

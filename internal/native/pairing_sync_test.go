package native

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/storage"
)

func TestPairingSyncWithoutDeliveries(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		code, state string
		revoked     bool
	}{
		{"revoked", 200, "", "revoked", true}, {"active", 200, "", "active", false},
		{"missing", 404, "not_found", "", true}, {"gone", 410, "pairing_revoked", "", true},
		{"proxy404", 404, "", "", false}, {"rateLimit", 429, "pairing_revoked", "", false},
		{"unavailable", 503, "pairing_revoked", "", false}, {"unauthorized", 401, "unauthorized", "", false},
		{"invalidStatus", 200, "", "unknown", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var pair storage.NativePairing
			svc, store := testService(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/info" {
					json.NewEncoder(w).Encode(Info{Audience: "test", Environment: "sandbox", Version: 2})
					return
				}
				w.WriteHeader(tc.status)
				if tc.status == 200 {
					json.NewEncoder(w).Encode(relayPairing{pair.ID, tc.state, pair.DeviceID, pair.EncryptionKey})
				} else {
					json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": tc.code}})
				}
			})
			id := addDevice(t, store, "phone", "key")
			pair, _ = store.NativePairing(t.Context(), id)
			_ = svc.PollActivePairings(t.Context())
			got, err := store.NativePairing(t.Context(), id)
			if err != nil || (got.State == "revoked") != tc.revoked {
				t.Fatalf("state=%s err=%v", got.State, err)
			}
		})
	}
}

func TestPairingSyncBackoffAndFairness(t *testing.T) {
	calls := map[string]int{}
	status := 429
	var store *storage.Store
	svc, store := testService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/info" {
			json.NewEncoder(w).Encode(Info{Audience: "test", Environment: "sandbox", Version: 2})
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/v1/pairings/")
		calls[id]++
		if status == 429 {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(429)
			return
		}
		p, _ := store.NativePairing(t.Context(), id)
		json.NewEncoder(w).Encode(relayPairing{p.ID, "active", p.DeviceID, p.EncryptionKey})
	})
	for _, id := range []string{"one", "two", "three"} {
		addDevice(t, store, id, "key")
	}
	now := time.Now()
	pollAt := func(at time.Time) error { return svc.pollActivePairings(t.Context(), func() time.Time { return at }) }
	_ = pollAt(now)
	_ = pollAt(now.Add(119 * time.Second))
	total := func() int {
		n := 0
		for _, v := range calls {
			n += v
		}
		return n
	}
	if total() != 1 {
		t.Fatal("rate limit backoff ignored", calls)
	}
	status = 200
	for i := 0; i < 3; i++ {
		if err := pollAt(now.Add(time.Duration(120+i*5) * time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if len(calls) != 3 {
		t.Fatal("pairings starved", calls)
	}
	if total() != 4 {
		t.Fatal("unbounded polling", calls)
	}
}

func TestPairingSyncTransportFailuresPreserveAuthorization(t *testing.T) {
	for _, mode := range []string{"timeout", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			svc, store := testService(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/info" {
					json.NewEncoder(w).Encode(Info{Audience: "test", Environment: "sandbox", Version: 2})
					return
				}
				if mode == "timeout" {
					<-r.Context().Done()
					return
				}
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					conn.Close()
				}
			})
			id := addDevice(t, store, "phone", "key")
			svc.client.http.Timeout = 50 * time.Millisecond
			if err := svc.PollActivePairings(t.Context()); err == nil {
				t.Fatal("expected transport failure")
			}
			p, err := store.NativePairing(t.Context(), id)
			if err != nil || p.State != "active" {
				t.Fatal(p, err)
			}
		})
	}
}

func TestPairingSyncWaitsAfterSlowFailureResponse(t *testing.T) {
	for _, tc := range []struct {
		name, retryAfter string
		status           int
		wait             time.Duration
	}{
		{"rate_limit", "120", 429, 120 * time.Second},
		{"temporary_failure", "", 503, 30 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			var elapsed atomic.Int64
			var calls atomic.Int32
			clock := func() time.Time { return start.Add(time.Duration(elapsed.Load())) }
			svc, store := testService(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/info" {
					json.NewEncoder(w).Encode(Info{Audience: "test", Environment: "sandbox", Version: 2})
					return
				}
				calls.Add(1)
				elapsed.Add(int64(10 * time.Second))
				if tc.retryAfter != "" {
					w.Header().Set("Retry-After", tc.retryAfter)
				}
				w.WriteHeader(tc.status)
			})
			addDevice(t, store, "one", "key")
			addDevice(t, store, "two", "key")
			if err := svc.pollActivePairings(t.Context(), clock); err == nil {
				t.Fatal("expected Relay failure")
			}
			elapsed.Store(int64(tc.wait + 9*time.Second))
			if err := svc.pollActivePairings(t.Context(), clock); err != nil {
				t.Fatal("retried before response-relative deadline", err)
			}
			if calls.Load() != 1 {
				t.Fatal("backoff ignored")
			}
			elapsed.Store(int64(tc.wait + 10*time.Second))
			if err := svc.pollActivePairings(t.Context(), clock); err == nil {
				t.Fatal("expected retry at deadline")
			}
			if calls.Load() != 2 {
				t.Fatal("polling did not resume")
			}
		})
	}
}

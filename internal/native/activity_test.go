package native

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/coreidentity"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

func queueActivity(t *testing.T, store *storage.Store, id, pairing string, expires time.Time) {
	t.Helper()
	if err := store.Enqueue(t.Context(), event.Notification{ID: id, ReceivedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveNativePlan(t.Context(), nil, storage.NativeActivity{EventID: id, PairingID: pairing, Request: `{"event_id":"` + id + `"}`, ExpiresAt: expires.Unix()}); err != nil {
		t.Fatal(err)
	}
}

func TestActivityRetryDeadlineSurvivesRestart(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		retry  string
		delay  time.Duration
	}{
		{"rate limit", 429, "120", 120 * time.Second},
		{"service unavailable", 503, "", 30 * time.Second},
		{"transport failure", 0, "", 30 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().Truncate(time.Millisecond)
			var sent atomic.Int32
			dir := t.TempDir()
			service, store := testServiceInDirectory(t, dir, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/info" {
					json.NewEncoder(w).Encode(Info{Audience: "test", Environment: "sandbox", Version: 2})
					return
				}
				if r.URL.Path != "/v1/activities" {
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				raw, _ := io.ReadAll(r.Body)
				if string(raw) != `{"event_id":"activity"}` {
					t.Errorf("request changed: %s", raw)
				}
				if sent.Add(1) == 1 {
					if tc.status == 0 {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						conn.Close()
						return
					}
					w.Header().Set("Retry-After", tc.retry)
					w.WriteHeader(tc.status)
					return
				}
				w.WriteHeader(202)
				w.Write([]byte(`{"id":"accepted"}`))
			})
			pairing := addDevice(t, store, "phone", "key")
			queueActivity(t, store, "activity", pairing, now.Add(10*time.Minute))
			poll := func() {
				t.Helper()
				if err := service.pollActivitiesAt(t.Context(), func() time.Time { return now }); err != nil {
					t.Fatal(err)
				}
			}
			poll()
			now = now.Add(tc.delay - time.Millisecond)
			poll()
			if sent.Load() != 1 {
				t.Fatal("activity sent before retry deadline")
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := storage.Open(t.Context(), dir)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			vault, err := settings.OpenVault(dir, true)
			if err != nil {
				t.Fatal(err)
			}
			service, err = New(reopened, vault, coreidentity.New(reopened, vault), service.client.url, service.log)
			if err != nil {
				t.Fatal(err)
			}
			poll()
			if sent.Load() != 1 {
				t.Fatal("restart bypassed retry deadline")
			}
			now = now.Add(time.Millisecond)
			poll()
			if sent.Load() != 2 {
				t.Fatal("activity was not delivered when due")
			}
			now = now.Add(time.Hour)
			poll()
			if sent.Load() != 2 {
				t.Fatal("accepted activity was retried")
			}
			// Activity processing leaves ordinary notification delivery untouched.
			pending, err := reopened.Due(t.Context(), time.Now())
			if err != nil || pending == nil || pending.Event.ID != "activity" {
				t.Fatalf("ordinary notification changed: %v, %v", pending, err)
			}
		})
	}
}

func TestActivityFailuresUseIncreasingBackoff(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	var sent atomic.Int32
	service, store := testService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/info" {
			json.NewEncoder(w).Encode(Info{Audience: "test", Environment: "sandbox", Version: 2})
			return
		}
		sent.Add(1)
		w.WriteHeader(503)
	})
	pairing := addDevice(t, store, "phone", "key")
	queueActivity(t, store, "activity", pairing, now.Add(time.Hour))
	for _, delay := range []time.Duration{30, 60, 120, 240, 300, 300} {
		before := sent.Load()
		if err := service.pollActivitiesAt(t.Context(), func() time.Time { return now }); err != nil {
			t.Fatal(err)
		}
		if sent.Load() != before+1 {
			t.Fatal("due activity was skipped")
		}
		now = now.Add(delay*time.Second - time.Millisecond)
		if err := service.pollActivitiesAt(t.Context(), func() time.Time { return now }); err != nil {
			t.Fatal(err)
		}
		if sent.Load() != before+1 {
			t.Fatal("backoff was shortened")
		}
		now = now.Add(time.Millisecond)
	}
}

func TestActivityDeadlineAndPermanentFailuresStopSending(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		retry   string
		expired bool
	}{
		{"expired", 202, "", true}, {"retry beyond lifetime", 429, "3600", false}, {"permanent failure", 403, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			var sent atomic.Int32
			service, store := testService(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/info" {
					json.NewEncoder(w).Encode(Info{Audience: "test", Environment: "sandbox", Version: 2})
					return
				}
				sent.Add(1)
				w.Header().Set("Retry-After", tc.retry)
				w.WriteHeader(tc.status)
			})
			pairing := addDevice(t, store, "phone", "key")
			expires := now.Add(time.Minute)
			if tc.expired {
				expires = now.Add(-time.Second)
			}
			queueActivity(t, store, "activity", pairing, expires)
			if err := service.pollActivitiesAt(t.Context(), func() time.Time { return now }); err != nil {
				t.Fatal(err)
			}
			due, err := store.DueNativeActivities(t.Context(), now.Add(24*time.Hour))
			if err != nil || len(due) != 0 {
				t.Fatalf("terminal activity remains queued: %v %v", due, err)
			}
			want := int32(1)
			if tc.expired {
				want = 0
			}
			if sent.Load() != want {
				t.Fatal("unexpected activity request")
			}
		})
	}
}

func TestActivityPreparationOutageAndCancellationPreserveRetryPolicy(t *testing.T) {
	now := time.Now()
	var infoCalls atomic.Int32
	service, store := testService(t, func(w http.ResponseWriter, r *http.Request) {
		infoCalls.Add(1)
		w.Header().Set("Retry-After", "90")
		w.WriteHeader(503)
	})
	pairing := addDevice(t, store, "phone", "key")
	queueActivity(t, store, "activity", pairing, now.Add(time.Minute*10))
	if err := service.pollActivitiesAt(t.Context(), func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	now = now.Add(89 * time.Second)
	if err := service.pollActivitiesAt(t.Context(), func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	if infoCalls.Load() != 1 {
		t.Fatal("Relay info retried during backoff")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	now = now.Add(time.Second)
	if err := service.pollActivitiesAt(ctx, func() time.Time { return now }); err == nil {
		t.Fatal("expected cancellation")
	}
	due, err := store.DueNativeActivities(t.Context(), now)
	if err != nil || len(due) != 1 {
		t.Fatalf("cancellation consumed activity: %v %v", due, err)
	}
}

package storage

import (
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/event"
)

func TestActivityRetryPreservesCiphertextAndTerminalState(t *testing.T) {
	for _, terminal := range []string{"accepted", "failed", "revoked"} {
		t.Run(terminal, func(t *testing.T) {
			s, err := Open(t.Context(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if _, err := s.db.Exec("INSERT INTO native_pairings(id,state,device_id,token_hash,expires_at,created_at) VALUES('p','active','phone','',0,0)"); err != nil {
				t.Fatal(err)
			}
			now := time.Now().Truncate(time.Millisecond)
			item := NativeActivity{EventID: "activity", PairingID: "p", Request: "encrypted-content", ExpiresAt: now.Add(10 * time.Minute).Unix()}
			if err := s.Enqueue(t.Context(), event.Notification{ID: item.EventID}); err != nil {
				t.Fatal(err)
			}
			if err := s.SaveNativePlan(t.Context(), nil, item); err != nil {
				t.Fatal(err)
			}
			if err := s.DelayNativeActivity(t.Context(), item, now.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			due, err := s.DueNativeActivities(t.Context(), now.Add(time.Minute-time.Millisecond))
			if err != nil || len(due) != 0 {
				t.Fatalf("activity sent early: %v %v", due, err)
			}
			due, err = s.DueNativeActivities(t.Context(), now.Add(time.Minute))
			if err != nil || len(due) != 1 {
				t.Fatalf("activity missing: %v %v", due, err)
			}
			if due[0].Request != item.Request || due[0].ExpiresAt != item.ExpiresAt || due[0].Failures != 1 {
				t.Fatal("retry changed encrypted plan or lost failures", due)
			}
			if terminal == "revoked" {
				_, err = s.RevokeNativePairing(t.Context(), item.PairingID)
			} else {
				err = s.FinishNativeActivity(t.Context(), item, terminal)
			}
			if err != nil {
				t.Fatal(err)
			}
			// A late failure from an in-flight request preserves the terminal decision.
			if err = s.DelayNativeActivity(t.Context(), item, now.Add(2*time.Minute)); err != nil {
				t.Fatal(err)
			}
			var state, request string
			if err = s.db.QueryRow("SELECT state,request FROM native_activities WHERE event_id=?", item.EventID).Scan(&state, &request); err != nil {
				t.Fatal(err)
			}
			want := terminal
			if terminal == "revoked" {
				want = "failed"
			}
			if state != want || request != "" {
				t.Fatalf("terminal state or ciphertext changed: %s %q", state, request)
			}
			due, err = s.DueNativeActivities(t.Context(), now.Add(time.Hour))
			if err != nil || len(due) != 0 {
				t.Fatalf("terminal work queued: %v %v", due, err)
			}
		})
	}
}

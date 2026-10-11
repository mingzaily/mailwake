package native

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

func addDevice(t *testing.T, store *storage.Store, id, key string) string {
	t.Helper()
	ctx := context.Background()
	p := storage.NativePairing{ID: uuid.NewString(), State: "waiting", TokenHash: "fixture", EncryptedToken: []byte{1}, ExpiresAt: time.Now().Unix() + 300, CreatedAt: time.Now().Unix()}
	if err := store.CreateNativePairing(ctx, p); err != nil {
		t.Fatal(err)
	}
	p.DeviceID = id
	p.SigningKey = key
	p.EncryptionKey = key
	p.DeviceName = id
	if err := store.SelectNativeDevice(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishNativePairing(ctx, p.ID, "active", ""); err != nil {
		t.Fatal(err)
	}
	return p.ID
}
func TestSelectedDeviceRestartReusesEnvelopeAndClearsTerminalContent(t *testing.T) {
	ctx := context.Background()
	v := vector(t)
	dir := t.TempDir()
	store, err := storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	vault, err := settings.OpenVault(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	first := addDevice(t, store, "first", v.Encryption.PublicKey)
	second := addDevice(t, store, "second", v.Core.PublicKey)
	requests := map[string][]string{}
	reject := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/info" {
			json.NewEncoder(w).Encode(Info{Audience: v.Audience, Environment: "sandbox", Version: 2})
			return
		}
		var body struct {
			PairingID string   `json:"pairing_id"`
			EventID   string   `json:"event_id"`
			Envelope  Envelope `json:"envelope"`
			Signature string   `json:"content_signature"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		raw, _ := json.Marshal(body.Envelope)
		requests[body.PairingID] = append(requests[body.PairingID], string(raw)+body.Signature)
		if reject && body.PairingID == second {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(202)
		json.NewEncoder(w).Encode(relayDelivery{ID: uuid.NewString(), PairingID: body.PairingID, EventID: body.EventID, Status: "queued"})
	}))
	defer server.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, _ := New(store, vault, server.URL, log)
	n := event.Notification{ID: "restart-event", Account: "Work", Folder: "Clients", Sender: "Alice", Subject: "secret subject", ReceivedAt: time.Now()}
	if err = store.Enqueue(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err = service.ForPairing(second).Send(ctx, n); !temporary(err) {
		t.Fatal(err)
	}
	items, _ := store.NativeDeliveries(ctx, n.ID)
	if len(items) != 1 {
		t.Fatal(len(items))
	}
	for _, d := range items {
		if d.PairingID == first && (d.Envelope != "" || d.RelayID == "") {
			t.Fatal("accepted payload retention")
		}
		if d.PairingID == second && d.Envelope == "" {
			t.Fatal("retry payload missing")
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	vault, err = settings.OpenVault(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	service, _ = New(store, vault, server.URL, log)
	reject = false
	if err = service.ForPairing(first).Send(ctx, n); err != nil {
		t.Fatal(err)
	}
	if len(requests[first]) != 0 || len(requests[second]) != 2 || requests[second][0] != requests[second][1] {
		t.Fatal("per-device idempotency changed")
	}
	if err = store.Attempt(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	// Simulate a restart at the default exhausted attempt budget after all device acknowledgements.
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	dispatcher := delivery.New(store, service, 0, log)
	dispatcher.SetNativeSender(service)
	go func() { defer close(done); dispatcher.Run(runCtx) }()
	deadline := time.Now().Add(3 * time.Second)
	accepted := false
	for time.Now().Before(deadline) {
		records, _ := store.Recent(ctx)
		if len(records) > 0 && records[0].State == "accepted" {
			accepted = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if !accepted {
		t.Fatal("durable Relay acknowledgements were discarded at exhausted attempt budget")
	}

	items, _ = store.NativeDeliveries(ctx, n.ID)
	for _, d := range items {
		if d.Envelope != "" || d.Signature != "" || d.RelayID == "" {
			t.Fatal("terminal privacy")
		}
	}
	// A failed native event also clears its outbox plaintext and pending envelope.
	n.ID = "failed-event"
	reject = true
	if err = store.Enqueue(ctx, n); err != nil {
		t.Fatal(err)
	}
	_ = service.ForPairing(second).Send(ctx, n)
	if err = store.Failed(ctx, n.ID, fault.New("attempts_exhausted"), time.Now(), true); err != nil {
		t.Fatal(err)
	}
	items, _ = store.NativeDeliveries(ctx, n.ID)
	for _, d := range items {
		if d.Envelope != "" || d.Signature != "" {
			t.Fatal("dead ciphertext retained")
		}
	}
	if ok, err := store.Retry(ctx, n.ID); err != nil || ok {
		t.Fatal("privacy-cleared event was retried", err)
	}
}
func TestPlaintextLimitAndNativeNoDevices(t *testing.T) {
	n := event.Notification{Account: "Work", Folder: "Clients", Code: "482913", Sender: strings.Repeat("发件人", 1000), Subject: strings.Repeat("世界<", 1000), ReceivedAt: time.Now()}
	b, err := notificationPlain(n)
	if err != nil || len(b) > 2032 {
		t.Fatal(len(b), err)
	}
	var p plaintext
	json.Unmarshal(b, &p)
	if p.Subject != "" || p.Sender == "" || p.Code != "482913" {
		t.Fatal("truncation order changed")
	}
	n.Account = strings.Repeat("x", 2040)
	if _, err = notificationPlain(n); err == nil {
		t.Fatal("oversized fixed fields accepted")
	}
	service, store := testService(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no-device delivery contacted Relay")
		w.WriteHeader(503)
	})
	n = event.Notification{ID: "no-device", Test: true, ReceivedAt: time.Now()}
	err = service.Send(context.Background(), n)
	if safeCode(err) != "native_target_required" || temporary(err) {
		t.Fatal(err)
	}
	records, _ := store.Recent(context.Background())
	if len(records) != 1 || records[0].State != "dead" {
		t.Fatal(records)
	}
}

func TestPlaintextStableMailboxIdentity(t *testing.T) {
	for _, mailboxID := range []string{"mbx_stable", ""} {
		n := event.Notification{MailboxID: mailboxID, Account: "Renamed", Folder: "Clients", ReceivedAt: time.Now()}
		body, err := notificationPlain(n)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(body, &fields); err != nil {
			t.Fatal(err)
		}
		if fields["v"] != float64(1) || fields["account"] != "Renamed" {
			t.Fatal("v1 metadata changed")
		}
		id, present := fields["mailbox_id"]
		if mailboxID == "" && present {
			t.Fatal("empty optional ID encoded")
		}
		if mailboxID != "" && id != mailboxID {
			t.Fatal("stable identity lost")
		}
		if _, present := fields["excerpt"]; present {
			t.Fatal("summary encoded")
		}
	}
}

func TestPlaintextOptionalVerificationCode(t *testing.T) {
	for _, code := range []string{"", "482913"} {
		raw, err := notificationPlain(event.Notification{Code: code, Subject: "Sign in", ReceivedAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		got, present := fields["code"]
		if code == "" && present || code != "" && got != code {
			t.Fatalf("code=%v present=%v", got, present)
		}
	}
}

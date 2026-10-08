package runtime

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/logging"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestConfigurationAndMailboxLifecycleLogs(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	vault, err := settings.OpenVault(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	log := logging.New(&output)
	m, err := newManager(t.Context(), store, vault, log, "", func(settings.Mailbox) mail.Source { return &multiSource{} }, func(settings.Delivery) delivery.Sender { return &countingSender{} })
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	id, input := addMailbox(t, m, "private-host", 3)
	subscribeMailbox(t, m, id)
	waitFor(t, func() bool { cp, _ := store.Checkpoint(t.Context(), id, "INBOX"); return cp != "" })
	input.Password = nil
	input.Label = ptr("private-label")
	if err := m.UpdateMailbox(t.Context(), id, input); err != nil {
		t.Fatal(err)
	}
	for _, channel := range []string{"bark", "pushover", "webhook"} {
		revision := m.delivery.Revision
		d := settings.DeliveryUpdate{Revision: &revision, Channel: channel, Preview: "subject", Language: "en"}
		d.Bark.Endpoint = "https://private-bark.example/path"
		d.Bark.Key = ptr("private-bark-key")
		d.Pushover.Token = ptr("private-pushover-token")
		d.Pushover.User = ptr("private-pushover-user")
		d.Webhook.URL = ptr("https://hooks.example/path?token=private-route")
		d.Webhook.Secret = ptr(strings.Repeat("z", 32))
		if err := m.UpdateDelivery(t.Context(), d); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.DeleteMailbox(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	m.Close()
	page := logging.Read(log, logging.Query{})
	data, _ := json.Marshal(page)
	for _, message := range []string{"Mailbox monitoring started", "Mailbox monitoring stopped", "Mailbox created", "Mailbox updated", "Mailbox deleted", "Subscriptions updated", "Delivery settings updated", "Folder connection established", "Folder baseline rebuilt", "New mail enqueued"} {
		if !strings.Contains(string(data), message) {
			t.Fatalf("missing lifecycle event %s", message)
		}
	}
	for _, entry := range page.Entries {
		if entry.Attrs["mailbox_id"] == nil {
			t.Fatal("mailbox scope missing")
		}
	}
	for _, secret := range []string{"private-host", "user@example.test", "test-password", "private-label", "private-bark", "private-pushover", "private-route", strings.Repeat("z", 32), `"private"`} {
		if strings.Contains(output.String(), secret) || strings.Contains(string(data), secret) {
			t.Fatal("configuration or message secret logged")
		}
	}
}

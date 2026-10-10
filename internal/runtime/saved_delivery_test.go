package runtime

import (
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/i18n"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/settings"
)

func TestSavedDeliveryTestUsesTheSavedChannel(t *testing.T) {
	m, _, _ := managerFixture(t, nil)
	sender := &countingSender{}
	m.newSender = func(settings.Delivery) delivery.Sender { return sender }
	revision := m.DeliveryView()["revision"].(int64)
	input := settings.DeliveryUpdate{Revision: &revision, Channel: "webhook", Preview: "off", Language: "en"}
	input.Webhook.URL = ptr("https://original.example.test")
	input.Webhook.Secret = ptr(strings.Repeat("x", 32))
	if err := m.UpdateDelivery(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	before := sender.calls.Load()
	if err := m.TestSavedDelivery(t.Context()); err != nil || sender.calls.Load() != before+1 {
		t.Fatal(err, sender.calls.Load())
	}
}

func TestSavedMailboxTestUsesStoredSource(t *testing.T) {
	constructed := 0
	m, _, _ := managerFixture(t, func(cfg settings.Mailbox) mail.Source {
		constructed++
		if cfg.Host != "imap.saved.test" || cfg.Username != "user@example.test" || cfg.Password != "test-password" {
			t.Fatal("stored mailbox changed during probe", cfg.Host, cfg.Username)
		}
		return &multiSource{}
	})
	id, _ := addMailbox(t, m, "imap.saved.test", 6)
	before := constructed
	if err := m.TestSavedMailbox(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if constructed != before {
		t.Fatal("saved test replaced the published source")
	}
}

func TestNotificationLanguagesPersistAndRender(t *testing.T) {
	m, _, _ := managerFixture(t, nil)
	for _, locale := range i18n.Languages() {
		t.Run(locale.Code, func(t *testing.T) {
			revision := m.DeliveryView()["revision"].(int64)
			input := settings.DeliveryUpdate{Revision: &revision, Channel: "bark", Preview: "off", Language: locale.Code}
			input.Bark.Key = ptr("fixture-key")
			if err := m.UpdateDelivery(t.Context(), input); err != nil {
				t.Fatal(err)
			}
			saved, err := m.settings.LoadDelivery(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if saved.Language != locale.Code || m.DeliveryView()["language"] != locale.Code {
				t.Fatal("notification language was not persisted", saved.Language)
			}
			if got := delivery.Render(saved.Language, event.Notification{}).Body; got != i18n.Message(locale.Code, "notification.new_mail", nil) {
				t.Fatal(got)
			}
		})
	}
}

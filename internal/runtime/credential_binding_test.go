package runtime

import (
	"context"
	"reflect"
	"testing"

	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/outlook"
	"github.com/mingzaily/mailwake/internal/settings"
	"golang.org/x/oauth2"
)

func TestMailboxTargetChangesRequirePasswordBeforeProbeAndPersistence(t *testing.T) {
	for _, field := range []string{"host", "port", "username"} {
		t.Run(field, func(t *testing.T) {
			m, _, _ := managerFixture(t, func(settings.Mailbox) mail.Source { return &multiSource{} })
			id, input := addMailbox(t, m, "original.example.test", 10)
			before, err := m.settings.Mailboxes(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			input.Password = nil
			switch field {
			case "host":
				input.Host = "other.example.test"
			case "port":
				input.Port = 994
			case "username":
				input.Username = "other"
			}
			m.newSource = func(settings.Mailbox) mail.Source {
				t.Fatal("rejected credentials reached connection factory")
				return nil
			}
			_, err = m.TestMailbox(t.Context(), id, input)
			if fault.From(err, "").Code != "mailbox_password_required_for_change" {
				t.Fatal("test", err)
			}
			if err = m.UpdateMailbox(t.Context(), id, input); fault.From(err, "").Code != "mailbox_password_required_for_change" {
				t.Fatal("save", err)
			}
			after, err := m.settings.Mailboxes(t.Context())
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("rejection changed persisted mailbox", err)
			}
			view, err := m.Mailbox(id)
			if err != nil || view.Host != before[0].Host || view.Port != before[0].Port || view.Username != before[0].Username || view.Revision != before[0].Revision {
				t.Fatal("rejection changed runtime mailbox", err)
			}
			input.Password = ptr("replacement-password")
			m.newSource = func(next settings.Mailbox) mail.Source {
				if next.Password != *input.Password || next.Host != input.Host || next.Port != input.Port || next.Username != input.Username {
					t.Fatal("wrong connection credentials")
				}
				return &multiSource{}
			}
			if _, err = m.TestMailbox(t.Context(), id, input); err != nil {
				t.Fatal(err)
			}
			tested, loadErr := m.settings.Mailboxes(t.Context())
			if loadErr != nil || !reflect.DeepEqual(before, tested) {
				t.Fatal("test changed saved mailbox", loadErr)
			}
			if err = m.UpdateMailbox(t.Context(), id, input); err != nil {
				t.Fatal(err)
			}
			after, err = m.settings.Mailboxes(t.Context())
			if err != nil || after[0].Password != *input.Password || after[0].Revision != before[0].Revision+1 {
				t.Fatal("replacement not persisted", err)
			}
		})
	}
}

func TestBarkTargetChangesRequireKeyBeforeSendingAndPersistence(t *testing.T) {
	m, _, _ := managerFixture(t, nil)
	revision := m.DeliveryView().Revision
	input := DeliveryUpdate{Revision: &revision, Channel: "bark", Preview: "off"}
	input.Bark.Endpoint = "https://original.example.test"
	input.Bark.Key = ptr("saved-key")
	if err := m.UpdateDelivery(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	before, err := m.settings.LoadDelivery(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	view := m.DeliveryView()
	revision = view.Revision
	input.Bark.Endpoint = "https://other.example.test"
	input.Bark.Key = nil
	m.newSender = func(settings.Delivery) delivery.Sender {
		t.Fatal("rejected credentials reached sender factory")
		return nil
	}
	if err = m.TestDelivery(t.Context(), input); fault.From(err, "").Code != "bark_key_required_for_change" {
		t.Fatal("test", err)
	}
	if err = m.UpdateDelivery(t.Context(), input); fault.From(err, "").Code != "bark_key_required_for_change" {
		t.Fatal("save", err)
	}
	after, err := m.settings.LoadDelivery(t.Context())
	if err != nil || !reflect.DeepEqual(before, after) || m.DeliveryView() != view {
		t.Fatal("rejection changed delivery configuration", err)
	}
	counter := &countingSender{}
	input.Bark.Key = ptr("replacement-key")
	m.newSender = func(next settings.Delivery) delivery.Sender {
		if next.Bark.Endpoint != input.Bark.Endpoint || next.Bark.Key != *input.Bark.Key {
			t.Fatal("wrong Bark credentials")
		}
		return counter
	}
	if err = m.TestDelivery(t.Context(), input); err != nil || counter.calls.Load() != 1 {
		t.Fatal("replacement test failed", err)
	}
	tested, loadErr := m.settings.LoadDelivery(t.Context())
	if loadErr != nil || !reflect.DeepEqual(before, tested) || m.DeliveryView() != view {
		t.Fatal("test changed saved delivery", loadErr)
	}
	if err = m.UpdateDelivery(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	after, err = m.settings.LoadDelivery(t.Context())
	if err != nil || after.Bark.Key != *input.Bark.Key || after.Bark.Endpoint != input.Bark.Endpoint || m.DeliveryView().Revision != revision+1 {
		t.Fatal("replacement not persisted", err)
	}
}

func TestOutlookReauthorizationVerifiesMailboxAndPreservesProgress(t *testing.T) {
	m, store, _ := managerFixture(t, func(box settings.Mailbox) mail.Source {
		access, err := box.Outlook.AccessToken(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if access == "rejected" {
			return &rejectedOutlookSource{}
		}
		return &multiSource{}
	})
	ctx := t.Context()
	input := settings.MailboxUpdate{Host: outlook.Host, Port: 993, Username: "owner@example.test", Outlook: &oauth2.Token{AccessToken: "approved", RefreshToken: "first"}}
	view, err := m.CreateMailbox(ctx, settings.MailboxCreate{MailboxUpdate: input, Folders: mail.RealtimeFolders([]string{"INBOX"})})
	if err != nil || !view.Outlook {
		t.Fatal("create", err)
	}
	waitFor(t, func() bool { cp, _ := store.Checkpoint(ctx, view.ID, "INBOX"); return cp == "progress" })
	input.Revision = view.Revision
	input.Outlook = &oauth2.Token{AccessToken: "rejected", RefreshToken: "wrong"}
	if err = m.UpdateMailbox(ctx, view.ID, input); fault.From(err, "").Code != mail.OutlookReauthorize {
		t.Fatal("unverified account replaced a working mailbox", err)
	}
	input.Outlook = &oauth2.Token{AccessToken: "renewed", RefreshToken: "second"}
	if err = m.UpdateMailbox(ctx, view.ID, input); err != nil {
		t.Fatal("reauthorization", err)
	}
	cp, err := store.Checkpoint(ctx, view.ID, "INBOX")
	if err != nil || cp != "progress" {
		t.Fatal("lost progress", err)
	}
	subs, err := m.Subscriptions(view.ID)
	if err != nil || len(subs.Folders) != 1 || subs.Folders[0].Name != "INBOX" {
		t.Fatal("lost subscriptions", err)
	}
}

type rejectedOutlookSource struct{ multiSource }

func (*rejectedOutlookSource) Folders(context.Context) (*mail.FolderDiscovery, error) {
	return nil, fault.New(mail.OutlookReauthorize)
}

package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestInvalidConfigurationStartsAndCanBeRepaired(t *testing.T) {
	for _, scope := range []string{"mailbox", "delivery", "malformed", "limit_one"} {
		t.Run(scope, func(t *testing.T) {
			dir := t.TempDir()
			store, err := storage.Open(t.Context(), dir)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			vault, err := OpenVault(dir, false)
			if err != nil {
				t.Fatal(err)
			}
			plain := []byte(`{"host":"private-host","port":0,"username":"private-user","password":"private-secret"}`)
			name := scope
			if scope == "limit_one" {
				plain = []byte(`{"host":"private-host","port":993,"username":"private-user","password":"private-secret","connection_limit":1}`)
				name = "mailbox"
			}
			if scope == "delivery" {
				plain = []byte(`{"channel":"webhook","preview":"content","webhook":{"secret":"private-secret"}}`)
			}
			if scope == "malformed" {
				plain = []byte(`{"password":"private-secret"`)
				name = "mailbox"
			}
			encrypted, err := vault.Seal(name, plain)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.SaveConfiguration(t.Context(), name, encrypted); err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			opened := make(chan string, 5)
			var closed atomic.Int32
			s, err := newService(t.Context(), store, vault, slog.New(slog.NewTextHandler(&logs, nil)), func(m Mailbox) mail.Source { return observedSource{opened, &closed, m.Host} }, func(Delivery) delivery.Sender { return &countingSender{} })
			if err != nil {
				t.Fatal("invalid configuration blocked startup", err)
			}
			defer s.Close()
			noticeScope := name
			if name == "mailbox" {
				noticeScope = "mailbox:mbx_primary"
			}
			if notice := s.Notices()[noticeScope]; notice == nil || notice.Code != "configuration_invalid" {
				t.Fatal("notice missing")
			}
			if bytes.Contains(logs.Bytes(), []byte("private-")) {
				t.Fatal("invalid configuration leaked into logs")
			}
			if name == "mailbox" {
				if s.MailboxView()["host"] != "" {
					t.Fatal("invalid mailbox remains configured")
				}
				err = s.UpdateMailbox(t.Context(), MailboxUpdate{Host: "valid", Port: 993, Username: "user", Password: ptr("valid-password")})
			} else {
				if s.Channel() != "" {
					t.Fatal("invalid delivery remains configured")
				}
				input := DeliveryUpdate{Channel: "bark", Preview: "off"}
				input.Bark.Key = ptr("valid-key")
				err = s.UpdateDelivery(t.Context(), input)
			}
			if err != nil {
				t.Fatal("repair failed", err)
			}
			if s.Notices()[noticeScope] != nil {
				t.Fatal("notice survived repair")
			}
		})
	}
}
func TestOverBudgetStartupPausesUntilSubscriptionsRepaired(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	vault, err := OpenVault(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(Mailbox{ID: "mbx_primary", Label: "user", Host: "imap", Port: 993, Username: "user", Password: "synthetic", ConnectionLimit: 2})
	encrypted, err := vault.Seal(storage.MailboxConfigurationName("mbx_primary"), data)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMailboxRecord(t.Context(), "mbx_primary", 0, encrypted, false); err != nil {
		t.Fatal(err)
	}
	if _, err := seedSubscriptions(t.Context(), store, "mbx_primary", []string{"one", "two"}); err != nil {
		t.Fatal(err)
	}
	opened := make(chan string, 4)
	var closed atomic.Int32
	s, err := newService(t.Context(), store, vault, slog.Default(), func(m Mailbox) mail.Source { return observedSource{opened, &closed, m.Host} }, sender)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if notice := s.Notices()["subscriptions:mbx_primary"]; notice == nil || notice.Code != "connection_budget_exceeded" {
		t.Fatal("budget notice missing")
	}
	if s.mailboxes["mbx_primary"].cancel != nil || s.Status()[0].State != "paused" {
		t.Fatal("over-budget watchers started")
	}
	if _, err := s.UpdateSubscriptions(t.Context(), mail.Subscriptions{Revision: 2, Folders: mail.RealtimeFolders([]string{"one"})}); err != nil {
		t.Fatal(err)
	}
	awaitValue(t, opened, "imap")
	if s.Notices()["subscriptions:mbx_primary"] != nil {
		t.Fatal("budget notice survived repair")
	}
}
func TestDecryptionFailureStillRejectsStartup(t *testing.T) {
	for _, wrongKey := range []bool{true, false} {
		t.Run(map[bool]string{true: "wrong key", false: "damaged ciphertext"}[wrongKey], func(t *testing.T) {
			dir := t.TempDir()
			store, err := storage.Open(t.Context(), dir)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			vault, err := OpenVault(dir, false)
			if err != nil {
				t.Fatal(err)
			}
			encrypted, err := vault.Seal(storage.MailboxConfigurationName("mbx_primary"), []byte(`{"id":"mbx_primary"}`))
			if err != nil {
				t.Fatal(err)
			}
			if wrongKey {
				vault, err = OpenVault(t.TempDir(), false)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				encrypted[len(encrypted)-1] ^= 1
			}
			if err := store.SaveMailboxRecord(t.Context(), "mbx_primary", 0, encrypted, false); err != nil {
				t.Fatal(err)
			}
			s, err := New(t.Context(), store, vault, slog.Default(), "")
			if s != nil {
				s.Close()
			}
			if fault.From(err, "").Code != "configuration_corrupt" {
				t.Fatal("decryption failed open", err)
			}
		})
	}
}

// seedSubscriptions saves folders as the second revision of an account.
func seedSubscriptions(ctx context.Context, store *storage.Store, account string, folders []string) (mail.Subscriptions, error) {
	if _, err := store.LoadSubscriptions(ctx, account); err != nil {
		return mail.Subscriptions{}, err
	}
	if err := store.SaveSubscriptions(ctx, account, mail.Subscriptions{Revision: 2, Folders: mail.RealtimeFolders(folders)}); err != nil {
		return mail.Subscriptions{}, err
	}
	return store.LoadSubscriptions(ctx, account)
}

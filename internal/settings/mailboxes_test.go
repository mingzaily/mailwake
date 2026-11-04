package settings

import (
	"bytes"
	"encoding/json"
	"github.com/mingzaily/mailwake/internal/outlook"
	"golang.org/x/oauth2"
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestMailboxModelsAndCiphertextIsolation(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	vault, err := OpenVault(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(store, vault)
	first := NewMailbox()
	second := NewMailbox()
	if first.ID == second.ID || !strings.HasPrefix(first.ID, "mbx_") {
		t.Fatal("unstable mailbox IDs")
	}
	for _, base := range []Mailbox{first, second} {
		next, err := base.Merge(MailboxUpdate{Host: "imap.test", Port: 993, Username: "user@example.test", Password: ptr("private-password")})
		if err != nil || next.Label != "user@example.test" {
			t.Fatal(next, err)
		}
		if err := repo.SaveMailboxRecord(t.Context(), base, next, base.Revision); err != nil {
			t.Fatal(err)
		}
	}
	records, err := store.MailboxConfigurations(t.Context())
	if err != nil || len(records) != 2 {
		t.Fatal(records, err)
	}
	if bytes.Contains(records[0].Encrypted, []byte("private-password")) {
		t.Fatal("plaintext password")
	}
	if _, err := vault.Open(records[1].Name, records[0].Encrypted); fault.From(err, "").Code != "configuration_corrupt" {
		t.Fatal("ciphertext can be swapped", err)
	}
	values, err := repo.Mailboxes(t.Context())
	if err != nil || len(values) != 2 {
		t.Fatal(values, err)
	}
	if values[0].Identity() == values[1].Identity() {
		t.Fatal("different mailbox IDs share event namespace")
	}
	base := values[0].Mailbox
	base.Label = strings.Repeat("邮", 64)
	next, err := base.Merge(MailboxUpdate{Host: base.Host, Port: base.Port, Username: base.Username})
	if err != nil || next.Password != "private-password" || next.Label != base.Label {
		t.Fatal("merge lost label or credential", err)
	}
	if _, err := base.Merge(MailboxUpdate{Host: base.Host, Port: base.Port, Username: base.Username, Label: ptr(strings.Repeat("邮", 65))}); fault.From(err, "").Code != "mailbox_label_invalid" {
		t.Fatal("long label accepted", err)
	}
}

func TestOutlookAuthorizationSurvivesRestartAndCredentialRotation(t *testing.T) {
	ctx := t.Context()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	vault, err := OpenVault(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(store, vault)
	input := MailboxUpdate{Host: outlook.Host, Port: 993, Username: "owner@example.test", Outlook: &oauth2.Token{AccessToken: "private-access", RefreshToken: "private-refresh"}}
	box, err := NewMailbox().Merge(input)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.CreateMailboxRecord(ctx, box, nil); err != nil {
		t.Fatal(err)
	}
	box.Revision = 1
	name := storage.OutlookCredentialName(box.ID)
	old, err := store.Configuration(ctx, name)
	if err != nil || len(old) == 0 || bytes.Contains(old, []byte("private-")) {
		t.Fatal("unencrypted credential", err)
	}
	rotated, err := vault.Seal(name, []byte("rotated"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RotateOutlookCredential(ctx, name, old, rotated); err != nil {
		t.Fatal(err)
	}
	if err := repo.RefreshMailboxDisplay(ctx, box); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveMailboxRecord(ctx, box, box, 1); err != nil {
		t.Fatal(err)
	}
	load := func() Mailbox {
		t.Helper()
		rows, err := NewRepository(store, vault).Mailboxes(ctx)
		if err != nil || len(rows) != 1 || rows[0].Invalid {
			t.Fatal("reload", err)
		}
		return rows[0].Mailbox
	}
	current := load()
	raw, _ := json.Marshal(current.View(0))
	if current.Revision != 2 || current.Outlook.RefreshToken() != "rotated" || bytes.Contains(raw, []byte("rotated")) || bytes.Contains(raw, []byte("private-")) {
		t.Fatal("stale grant or unsafe view")
	}
	for _, bad := range []MailboxUpdate{{Host: "untrusted.example", Port: 993, Username: current.Username}, {Host: outlook.Host, Port: 143, Username: current.Username}, {Host: outlook.Host, Port: 993, Username: "other"}} {
		if _, err := current.Merge(bad); err == nil {
			t.Fatal("reused credential on another target")
		}
		bad.Outlook = &oauth2.Token{RefreshToken: "fresh"}
		if bad.Host != outlook.Host || bad.Port != 993 {
			if _, err := current.Merge(bad); err == nil {
				t.Fatal("accepted OAuth for arbitrary IMAP endpoint")
			}
		}
	}
	input.Outlook = &oauth2.Token{RefreshToken: "new-grant"}
	next, err := current.Merge(input)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveMailboxRecord(ctx, current, next, current.Revision); err != nil {
		t.Fatal(err)
	}
	if err = store.RotateOutlookCredential(ctx, name, rotated, old); fault.From(err, "").Code != "settings_conflict" {
		t.Fatal("stale refresh overwrote reauthorization", err)
	}
	current = load()
	if current.Outlook.RefreshToken() != "new-grant" {
		t.Fatal("lost replacement")
	}
	// Repair a recoverable mailbox with a password and verify the saved method
	// on reload, even when the invalid configuration hid its previous grant.
	broken, err := vault.Seal(storage.MailboxConfigurationName(box.ID), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.RefreshMailboxDisplay(ctx, box.ID, current.Revision, broken); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.Mailboxes(ctx)
	if err != nil || !rows[0].Invalid {
		t.Fatal("expected recoverable mailbox", err)
	}
	repair := rows[0].Mailbox
	passwordBox, err := repair.Merge(MailboxUpdate{Host: "imap.example.test", Port: 993, Username: "user", Password: ptr("local-password")})
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveMailboxRecord(ctx, repair, passwordBox, repair.Revision); err != nil {
		t.Fatal(err)
	}
	current = load()
	if current.Outlook != nil || current.Password != "local-password" {
		t.Fatal("repair retained stale credential")
	}
	// Replace the password with a grant once more before deleting the mailbox.
	next, err = current.Merge(input)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveMailboxRecord(ctx, current, next, current.Revision); err != nil {
		t.Fatal(err)
	}
	current = load()
	if err = store.DeleteMailbox(ctx, box.ID, current.Revision); err != nil {
		t.Fatal(err)
	}
	if data, err := store.Configuration(ctx, name); err != nil || data != nil {
		t.Fatal("deleted mailbox retained grant", err)
	}
}

package settings

import (
	"bytes"
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

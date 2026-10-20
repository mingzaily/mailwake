package storage

import (
	"fmt"
	"testing"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
)

func TestMailboxTransactionsAndIndependentRevisions(t *testing.T) {
	s, err := Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	for _, id := range []string{"mbx_a", "mbx_b"} {
		if err := s.SaveMailboxRecord(ctx, id, 0, []byte(id), false); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveSubscriptions(ctx, id, mail.Subscriptions{Revision: 2, Folders: mail.RealtimeFolders([]string{"INBOX"})}); err != nil {
			t.Fatal(err)
		}
		if err := s.Commit(ctx, id, "INBOX", "", "progress", []event.Notification{{ID: id, Account: id}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveSubscriptions(ctx, "mbx_a", mail.Subscriptions{Revision: 3, Folders: mail.RealtimeFolders([]string{"Archive"})}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMailboxRecord(ctx, "mbx_a", 1, []byte("new"), true); err != nil {
		t.Fatal(err)
	}
	if cp, _ := s.Checkpoint(ctx, "mbx_a", "INBOX"); cp != "" {
		t.Fatal("identity change retained cursor")
	}
	if cp, _ := s.Checkpoint(ctx, "mbx_b", "INBOX"); cp != "progress" {
		t.Fatal("identity change reset another mailbox")
	}
	if err := s.SaveMailboxRecord(ctx, "mbx_a", 1, []byte("stale"), false); fault.From(err, "").Code != "settings_conflict" {
		t.Fatal("stale configuration saved", err)
	}
	if err := s.SaveDelivery(ctx, []byte("delivery"), "off", "webhook", false, 0); err != nil {
		t.Fatal("mailbox changes affected delivery revision", err)
	}
	if err := s.SaveDelivery(ctx, []byte("stale"), "off", "webhook", false, 0); fault.From(err, "").Code != "settings_conflict" {
		t.Fatal("stale delivery saved", err)
	}
	if err := s.DeleteMailbox(ctx, "mbx_a", 2); err != nil {
		t.Fatal(err)
	}
	if cp, _ := s.Checkpoint(ctx, "mbx_b", "INBOX"); cp != "progress" {
		t.Fatal("delete reset another mailbox")
	}
	subs, err := s.LoadSubscriptions(ctx, "mbx_b")
	if err != nil || subs.Revision != 2 {
		t.Fatal("delete lost another subscription", subs, err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM mailbox_subscriptions WHERE account='mbx_a'").Scan(&count); err != nil || count != 0 {
		t.Fatal("deleted subscriptions remain", count, err)
	}
	counts, err := s.Summary(ctx)
	if err != nil || counts.Pending != 2 {
		t.Fatal("delete removed queued events", counts, err)
	}
	if err := s.SaveMailboxRecord(ctx, "mbx_a", 2, []byte("revive"), false); fault.From(err, "").Code != "mailbox_not_found" {
		t.Fatal("deleted mailbox revived", err)
	}
}
func TestMailboxLimitInsideTransaction(t *testing.T) {
	s, err := Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 20; i++ {
		if err := s.SaveMailboxRecord(t.Context(), fmt.Sprintf("mbx_%d", i), 0, []byte("encrypted"), false); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveMailboxRecord(t.Context(), "mbx_overflow", 0, []byte("encrypted"), false); fault.From(err, "").Code != "mailbox_limit_exceeded" {
		t.Fatal("accepted twenty-first mailbox", err)
	}
	records, err := s.MailboxConfigurations(t.Context())
	if err != nil || len(records) != 20 {
		t.Fatal(records, err)
	}
}

func TestCreateMailboxAndInitialFoldersRollbackTogether(t *testing.T) {
	s, err := Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.db.ExecContext(t.Context(), "CREATE TRIGGER reject_initial_folders BEFORE INSERT ON mailbox_subscriptions BEGIN SELECT RAISE(ABORT, 'synthetic failure'); END")
	if err != nil {
		t.Fatal(err)
	}
	folders := []mail.Folder{{Name: "INBOX", Check: "5m"}}
	if err := s.CreateMailboxRecord(t.Context(), "new", []byte("encrypted"), folders); err == nil {
		t.Fatal("expected persistence error")
	}
	records, err := s.MailboxConfigurations(t.Context())
	if err != nil || len(records) != 0 {
		t.Fatalf("partial mailbox: %v %v", records, err)
	}
	if _, err := s.db.ExecContext(t.Context(), "DROP TRIGGER reject_initial_folders"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMailboxRecord(t.Context(), "new", []byte("encrypted"), folders); err != nil {
		t.Fatal(err)
	}
	state, err := s.LoadSubscriptions(t.Context(), "new")
	if err != nil || len(state.Folders) != 1 || state.Folders[0] != folders[0] {
		t.Fatalf("retry: %+v %v", state, err)
	}
}

package storage

import (
	"context"
	"testing"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
)

// seedSubscriptions saves folders as the second revision of an account.
func seedSubscriptions(ctx context.Context, s *Store, account string, folders []string) (mail.Subscriptions, error) {
	if _, err := s.LoadSubscriptions(ctx, account); err != nil {
		return mail.Subscriptions{}, err
	}
	if err := s.SaveSubscriptions(ctx, account, mail.Subscriptions{Revision: 2, Folders: mail.RealtimeFolders(folders)}); err != nil {
		return mail.Subscriptions{}, err
	}
	return s.LoadSubscriptions(ctx, account)
}

func TestSubscriptionsPersistEmptyAndRejectStaleWrites(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	state, err := s.LoadSubscriptions(ctx, "a")
	if err != nil || state.Revision != 1 || len(state.Folders) != 0 {
		t.Fatalf("new account: %+v %v", state, err)
	}
	if err := s.SaveSubscriptions(ctx, "a", mail.Subscriptions{Revision: 2, Folders: mail.RealtimeFolders([]string{"INBOX"})}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSubscriptions(ctx, "a", mail.Subscriptions{Revision: 2, Folders: mail.RealtimeFolders([]string{"Old"})}); fault.From(err, "unknown_error").Code != "subscriptions_conflict" {
		t.Fatalf("stale update: %v", err)
	}
	if err := s.SaveSubscriptions(ctx, "a", mail.Subscriptions{Revision: 3, Folders: mail.RealtimeFolders([]string{})}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	state, err = s.LoadSubscriptions(ctx, "a")
	if err != nil || state.Revision != 3 || len(state.Folders) != 0 {
		t.Fatalf("empty selection restored: %+v %v", state, err)
	}
}

func TestAddedFolderStartsFromNewBaseline(t *testing.T) {
	ctx := t.Context()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := seedSubscriptions(ctx, s, "a", []string{"Kept", "Resubscribed"}); err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"Kept", "Resubscribed"} {
		if err := s.Commit(ctx, "a", folder, "", "saved-"+folder, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveSubscriptions(ctx, "a", mail.Subscriptions{Revision: 3, Folders: mail.RealtimeFolders([]string{"Kept"})}); err != nil {
		t.Fatal(err)
	}
	if cp, _ := s.Checkpoint(ctx, "a", "Resubscribed"); cp != "saved-Resubscribed" {
		t.Fatalf("unsubscribing kept progress until the folder returns: %q", cp)
	}
	if err := s.SaveSubscriptions(ctx, "a", mail.Subscriptions{Revision: 4, Folders: mail.RealtimeFolders([]string{"Kept", "Resubscribed"})}); err != nil {
		t.Fatal(err)
	}
	if cp, _ := s.Checkpoint(ctx, "a", "Resubscribed"); cp != "" {
		t.Fatalf("resubscribed folder must rebuild its baseline, kept %q", cp)
	}
	if cp, _ := s.Checkpoint(ctx, "a", "Kept"); cp != "saved-Kept" {
		t.Fatalf("unchanged subscription lost progress: %q", cp)
	}
}

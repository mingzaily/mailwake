package runtime

import (
	"bytes"
	"context"
	"errors"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
	"testing"
)

type displaySource struct {
	*multiSource
	discover func(context.Context) (*mail.FolderDiscovery, error)
}

func (s *displaySource) Folders(ctx context.Context) (*mail.FolderDiscovery, error) {
	return s.discover(ctx)
}
func displayDirectory(leaf string) *mail.FolderDiscovery {
	return &mail.FolderDiscovery{Folders: []string{"parent/child"}, FolderNames: map[string]string{"parent/child": leaf}}
}

func TestFolderDisplaySurvivesScanRenameAndReload(t *testing.T) {
	source := &displaySource{multiSource: &multiSource{}, discover: func(context.Context) (*mail.FolderDiscovery, error) { return displayDirectory("child"), nil }}
	m, store, dir := managerFixture(t, func(settings.Mailbox) mail.Source { return source })
	id, input := addMailbox(t, m, "mail", 2)
	record, err := store.ConfigurationRecord(t.Context(), storage.MailboxConfigurationName(id))
	if err != nil || bytes.Contains(record.Encrypted, []byte("parent/child")) {
		t.Fatal("folder display must be encrypted", err)
	}
	view, _ := m.Mailbox(id)
	if view.FolderDisplay.FolderNames["parent/child"] != "child" {
		t.Fatal(view)
	}
	source.discover = func(context.Context) (*mail.FolderDiscovery, error) { return displayDirectory("new leaf"), nil }
	if _, err := m.Folders(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	view, _ = m.Mailbox(id)
	if view.Revision != 1 || view.FolderDisplay.FolderNames["parent/child"] != "new leaf" {
		t.Fatal(view)
	}
	// Display refresh preserves a form's configuration revision and original subscription identity.
	input.Password = nil
	input.Label = ptr("Renamed")
	if err := m.UpdateMailbox(t.Context(), id, input); err != nil {
		t.Fatal(err)
	}
	if _, err := m.UpdateSubscriptions(t.Context(), id, mail.Subscriptions{Revision: 1, Folders: []mail.Folder{{Name: "parent/child", Check: "5m"}}}); err != nil {
		t.Fatal(err)
	}
	subs, _ := m.Subscriptions(id)
	if subs.Folders[0].Name != "parent/child" {
		t.Fatal(subs)
	}
	m.Close()
	vault, err := settings.OpenVault(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := newManager(t.Context(), store, vault, m.log, "", func(settings.Mailbox) mail.Source { return source }, m.newSender)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	view, _ = restarted.Mailbox(id)
	if view.Label != "Renamed" || view.Revision != 2 || view.FolderDisplay.FolderNames["parent/child"] != "new leaf" {
		t.Fatal(view)
	}
}

func TestStaleFolderScanCannotOverwriteChangedMailbox(t *testing.T) {
	source := &displaySource{multiSource: &multiSource{}, discover: func(context.Context) (*mail.FolderDiscovery, error) { return displayDirectory("initial"), nil }}
	m, _, _ := managerFixture(t, func(settings.Mailbox) mail.Source { return source })
	id, input := addMailbox(t, m, "mail", 2)
	started, release := make(chan struct{}), make(chan struct{})
	source.discover = func(context.Context) (*mail.FolderDiscovery, error) {
		close(started)
		<-release
		return displayDirectory("stale"), nil
	}
	done := make(chan error, 1)
	go func() { _, err := m.Folders(t.Context(), id); done <- err }()
	<-started
	input.Password = nil
	input.Label = ptr("Updated")
	err := m.UpdateMailbox(t.Context(), id, input)
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; fault.From(err, "").Code != "settings_conflict" {
		t.Fatal(err)
	}
	view, _ := m.Mailbox(id)
	if view.Label != "Updated" || view.FolderDisplay.FolderNames["parent/child"] != "initial" {
		t.Fatal(view)
	}
}

func TestFailedScanPreservesDisplayAndMailboxReplacementRefreshesIt(t *testing.T) {
	first := &displaySource{multiSource: &multiSource{}, discover: func(context.Context) (*mail.FolderDiscovery, error) { return displayDirectory("first"), nil }}
	second := &displaySource{multiSource: &multiSource{}, discover: func(context.Context) (*mail.FolderDiscovery, error) {
		return &mail.FolderDiscovery{Folders: []string{"plain"}}, nil
	}}
	m, _, _ := managerFixture(t, func(config settings.Mailbox) mail.Source {
		if config.Host == "other" {
			return second
		}
		return first
	})
	id, input := addMailbox(t, m, "mail", 2)
	first.discover = func(context.Context) (*mail.FolderDiscovery, error) { return nil, errors.New("synthetic failure") }
	if _, err := m.Folders(t.Context(), id); err == nil {
		t.Fatal("failed scan succeeded")
	}
	view, _ := m.Mailbox(id)
	if view.FolderDisplay.FolderNames["parent/child"] != "first" {
		t.Fatal(view)
	}
	input.Host = "other"
	if err := m.UpdateMailbox(t.Context(), id, input); err != nil {
		t.Fatal(err)
	}
	view, _ = m.Mailbox(id)
	if len(view.FolderDisplay.FolderNames) != 0 || len(view.FolderDisplay.FolderRoles) != 0 {
		t.Fatal("old account display survived replacement", view)
	}
}

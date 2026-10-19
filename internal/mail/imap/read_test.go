package imap

import (
	"context"
	"slices"
	"strings"
	"testing"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
)

func TestMarkReadPreservesOtherFlagsAndWatcherProgress(t *testing.T) {
	source, user := fixture(t)
	watcher, err := source.Open(t.Context(), "Clients")
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()
	baseline, err := watcher.Poll(t.Context(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	added, err := user.Append("Clients", strings.NewReader("From: Alice <alice@example.org>\r\nSubject: Hello\r\n\r\nBody"), &protocol.AppendOptions{Flags: []protocol.Flag{protocol.FlagFlagged}})
	if err != nil {
		t.Fatal(err)
	}
	location := mail.Location{UIDValidity: added.UIDValidity, UID: uint32(added.UID)}
	for range 2 {
		if err := source.MarkRead(t.Context(), "Clients", location); err != nil {
			t.Fatal(err)
		}
	}
	batch, err := watcher.Poll(t.Context(), baseline.Checkpoint, false)
	if err != nil || len(batch.Messages) != 1 {
		t.Fatalf("watcher progress: %+v %v", batch, err)
	}
	messages, err := watcher.(*session).client.Fetch(protocol.UIDSetNum(added.UID), &protocol.FetchOptions{Flags: true}).Collect()
	if err != nil || len(messages) != 1 || !slices.Contains(messages[0].Flags, protocol.FlagSeen) || !slices.Contains(messages[0].Flags, protocol.FlagFlagged) {
		t.Fatalf("flags: %+v %v", messages, err)
	}
	for _, invalid := range []mail.Location{{UIDValidity: added.UIDValidity + 1, UID: uint32(added.UID)}, {UIDValidity: added.UIDValidity, UID: 9999}} {
		if err := source.MarkRead(t.Context(), "Clients", invalid); fault.From(err, "").Code != "message_not_found" {
			t.Fatal(err)
		}
	}
	if err := source.MarkRead(t.Context(), "Missing", location); fault.From(err, "").Code != "message_not_found" {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := source.MarkRead(ctx, "Clients", location); err == nil {
		t.Fatal("cancelled write succeeded")
	}
}

func TestMarkReadReportsProviderFailuresAndTimeouts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options serverOptions
		code    string
	}{
		{"write rejected", serverOptions{caps: "IMAP4rev1", storeFailure: "[NOPERM] private provider detail"}, "message_unavailable"},
		{"write timeout", serverOptions{caps: "IMAP4rev1", silent: "STORE"}, "imap_command_timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, _, _, _ := scriptedServer(t, tc.options)
			err := source.MarkRead(t.Context(), "Clients", mail.Location{UIDValidity: 1, UID: 1})
			if err == nil || fault.From(err, "").Code != tc.code || strings.Contains(err.Error(), "private provider detail") {
				t.Fatalf("write error: %v", err)
			}
		})
	}
}

func TestMarkReadRequiresPermanentSeenPermission(t *testing.T) {
	for _, tc := range []struct {
		name    string
		flags   string
		allowed bool
	}{
		{"omitted uses protocol default", "", true},
		{"seen allowed", `(\Seen \Flagged)`, true},
		{"case insensitive seen", `(\sEeN)`, true},
		{"empty permission list", `()`, false},
		{"other flags only", `(\Flagged)`, false},
		{"wildcard permits keywords", `(\*)`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, _, _, _ := scriptedServer(t, serverOptions{caps: "IMAP4rev1", permanentFlags: tc.flags})
			err := source.MarkRead(t.Context(), "Clients", mail.Location{UIDValidity: 1, UID: 1})
			if tc.allowed {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || fault.From(err, "").Code != "message_unavailable" {
				t.Fatalf("session-only flag update: %v", err)
			}
		})
	}
}

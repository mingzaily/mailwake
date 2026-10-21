package imap

import (
	"context"
	"slices"
	"strings"
	"testing"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/logging"
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
		{"write rejected", serverOptions{readScenario: "unread", caps: "IMAP4rev1", storeFailure: "[NOPERM] private provider detail"}, "message_unavailable"},
		{"write timeout", serverOptions{readScenario: "unread", caps: "IMAP4rev1", silent: "STORE"}, "imap_command_timeout"},
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
			source, _, _, _ := scriptedServer(t, serverOptions{readScenario: "unread", caps: "IMAP4rev1", permanentFlags: tc.flags})
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

func TestMarkReadVerifiesCurrentMessageState(t *testing.T) {
	for _, tc := range []struct{ name, scenario, code, outcome string }{
		{"unread with silent STORE response", "unread", "", "marked_read"},
		{"explicit empty flags", "empty_flags", "", "marked_read"},
		{"already read without write permission", "seen", "", "already_read"},
		{"missing before write", "missing", "message_not_found", ""},
		{"deleted during write", "vanishes", "message_not_found", ""},
		{"provider accepted without setting Seen", "unchanged", "message_unavailable", ""},
		{"missing requested flags", "missing_flags", "message_unavailable", ""},
		{"unrelated UID", "other_uid", "message_not_found", ""},
		{"response without UID", "no_uid", "message_not_found", ""},
		{"split UID flag responses", "split_flags", "", "marked_read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := serverOptions{caps: "IMAP4rev1", readScenario: tc.scenario}
			if tc.scenario == "seen" {
				options.permanentFlags = "()"
				options.storeFailure = "[NOPERM] must not write"
			}
			source, _, _, _ := scriptedServer(t, options)
			ctx, trace := logging.WithRequestTrace(t.Context())
			err := source.MarkRead(ctx, "Clients", mail.Location{UIDValidity: 1, UID: 1})
			code := ""
			if err != nil {
				code = fault.From(err, "unexpected").Code
			}
			if code != tc.code || trace.Outcome != tc.outcome {
				t.Fatalf("code=%s outcome=%s err=%v", code, trace.Outcome, err)
			}
		})
	}
}

func TestMessageFolderAndFlagFailuresRetainAccurateMeaning(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		options               serverOptions
		code, stage, response string
	}{
		{"lowercase missing folder", serverOptions{openFailure: "[nonexistent] private detail"}, "message_not_found", "imap_select", "NONEXISTENT"},
		{"mixed-case permission", serverOptions{openFailure: "[NoPeRm] private detail"}, "imap_folder_rejected", "imap_select", "NOPERM"},
		{"folder absent", serverOptions{openFailure: "[NONEXISTENT] private detail"}, "message_not_found", "imap_select", "NONEXISTENT"},
		{"folder permission", serverOptions{openFailure: "[NOPERM] private detail"}, "imap_folder_rejected", "imap_select", "NOPERM"},
		{"provider unavailable", serverOptions{openFailure: "[UNAVAILABLE] private detail"}, "imap_folder_rejected", "imap_select", "UNAVAILABLE"},
		{"provider-defined lookalike code", serverOptions{openFailure: "[NONEXISTENT!] private detail"}, "imap_folder_rejected", "imap_select", "OTHER"},
		{"generic selection rejection", serverOptions{openFailure: "private detail"}, "imap_folder_open_failed", "imap_select", ""},
		{"initial flags rejected", serverOptions{fetchFailure: "[NOPERM] private detail"}, "message_unavailable", "read_flags", "NOPERM"},
		{"verification rejected", serverOptions{verifyFailure: "[UNAVAILABLE] private detail"}, "message_unavailable", "verify_seen", "UNAVAILABLE"},
		{"store rejected", serverOptions{storeFailure: "[NOPERM] private detail"}, "message_unavailable", "store_seen", "NOPERM"},
		{"flags timeout", serverOptions{silent: "FETCH"}, "imap_command_timeout", "read_flags", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.options.caps, tc.options.readScenario = "IMAP4rev1", "unread"
			source, _, _, _ := scriptedServer(t, tc.options)
			ctx, trace := logging.WithRequestTrace(t.Context())
			err := source.MarkRead(ctx, "Clients", mail.Location{UIDValidity: 1, UID: 1})
			if err == nil || fault.From(err, "").Code != tc.code || trace.Stage != tc.stage || trace.ResponseCode != tc.response || strings.Contains(err.Error(), "private detail") {
				t.Fatalf("err=%v trace=%+v", err, trace)
			}
		})
	}
}

func TestCachedBodyRemainsReadableAfterRemoteReferenceExpires(t *testing.T) {
	for _, change := range []string{"move", "delete", "folder deleted"} {
		t.Run(change, func(t *testing.T) {
			source, user := fixture(t)
			added, err := user.Append("Clients", strings.NewReader("Content-Type: text/plain\r\n\r\ncached body"), &protocol.AppendOptions{})
			if err != nil {
				t.Fatal(err)
			}
			location := mail.Location{UIDValidity: added.UIDValidity, UID: uint32(added.UID)}
			cached, err := source.ReadContent(t.Context(), "Clients", location)
			if err != nil || cached.Text != "cached body" {
				t.Fatalf("body=%+v err=%v", cached, err)
			}
			writer, err := source.connect(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			if _, err = writer.client.Select("Clients", nil).Wait(); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "move":
				if err = user.Create("Archive", nil); err != nil {
					t.Fatal(err)
				}
				_, err = writer.client.Move(protocol.UIDSetNum(added.UID), "Archive").Wait()
			case "delete":
				err = writer.client.Store(protocol.UIDSetNum(added.UID), &protocol.StoreFlags{Op: protocol.StoreFlagsAdd, Flags: []protocol.Flag{protocol.FlagDeleted}}, nil).Close()
				if err == nil {
					err = writer.client.Expunge().Close()
				}
			case "folder deleted":
				err = user.Delete("Clients")
			}
			if err != nil {
				t.Fatal(err)
			}
			err = source.MarkRead(t.Context(), "Clients", location)
			if err == nil || fault.From(err, "").Code != "message_not_found" || cached.Text != "cached body" {
				t.Fatalf("cached=%+v err=%v", cached, err)
			}
		})
	}
}

func TestMarkReadRespectsLowercasePermanentFlags(t *testing.T) {
	source, _, _, _ := scriptedServer(t, serverOptions{caps: "IMAP4rev1", readScenario: "unread", permanentFlags: "()", lowercasePermanentFlags: true})
	err := source.MarkRead(t.Context(), "Clients", mail.Location{UIDValidity: 1, UID: 1})
	if err == nil || fault.From(err, "").Code != "message_unavailable" {
		t.Fatalf("read succeeded without permanent flag permission: %v", err)
	}
}

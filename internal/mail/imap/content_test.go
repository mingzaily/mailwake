package imap

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
)

func TestReadContentSelectsTextAndPreservesUnread(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
		truncated       bool
	}{
		{"plain", "Content-Type: text/plain\r\n\r\nhello", "hello", false},
		{"html", "Content-Type: text/html\r\n\r\n<p>Hello</p><ul><li>one</li></ul><a href=\"https://example.com\">site</a>", "https://example.com", false},
		{"truncated", "Content-Type: text/plain\r\n\r\n" + strings.Repeat("a", 70000), "aaa", true},
		{"attachments", "Content-Type: multipart/mixed; boundary=x\r\n\r\n--x\r\nContent-Type: text/plain\r\n\r\nhello\r\n--x\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=secret.txt\r\n\r\nattachment secret\r\n--x--", "hello", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, user := fixture(t)
			added, err := user.Append("Clients", strings.NewReader(tc.raw), &protocol.AppendOptions{})
			if err != nil {
				t.Fatal(err)
			}
			body, err := source.ReadContent(t.Context(), "Clients", mail.Location{UIDValidity: added.UIDValidity, UID: uint32(added.UID)})
			if err != nil || !strings.Contains(body.Text, tc.want) || body.Truncated != tc.truncated || strings.Contains(body.Text, "attachment secret") {
				t.Fatalf("body length=%d truncated=%v error=%v", len(body.Text), body.Truncated, err)
			}
			opened, err := source.Open(t.Context(), "Clients")
			if err != nil {
				t.Fatal(err)
			}
			defer opened.Close()
			messages, err := opened.(*session).client.Fetch(protocol.UIDSetNum(added.UID), &protocol.FetchOptions{Flags: true}).Collect()
			if err != nil || len(messages) != 1 || slices.Contains(messages[0].Flags, protocol.FlagSeen) {
				t.Fatal("read modified flags", err)
			}
		})
	}
}
func TestReadContentRejectsMissingEpochOversizeAndMalformed(t *testing.T) {
	source, user := fixture(t)
	for _, tc := range []struct{ raw, code string }{
		{"Content-Type: text/plain\r\n\r\n" + strings.Repeat("x", 1<<20), "message_too_large"},
		{"Content-Type: text/plain\r\nContent-Transfer-Encoding: base64\r\n\r\n%%%", "message_content_invalid"},
	} {
		added, err := user.Append("Clients", strings.NewReader(tc.raw), &protocol.AppendOptions{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = source.ReadContent(t.Context(), "Clients", mail.Location{UIDValidity: added.UIDValidity, UID: uint32(added.UID)})
		if fault.From(err, "").Code != tc.code {
			t.Fatalf("wanted %s got %v", tc.code, err)
		}
		for _, location := range []mail.Location{{UIDValidity: added.UIDValidity + 1, UID: uint32(added.UID)}, {UIDValidity: added.UIDValidity, UID: 9999}} {
			_, err := source.ReadContent(t.Context(), "Clients", location)
			if fault.From(err, "").Code != "message_not_found" {
				t.Fatal(err)
			}
		}
	}
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := source.ReadContent(ctx, "Clients", mail.Location{UIDValidity: 1, UID: 1}); err == nil {
		t.Fatal("expired request succeeded")
	}
}

func TestReadContentTracksOriginalFolderAcrossMoveAndDeletion(t *testing.T) {
	source, user := fixture(t)
	added, err := user.Append("Clients", strings.NewReader("Content-Type: text/plain\r\n\r\noriginal"), &protocol.AppendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := user.Create("Archive", nil); err != nil {
		t.Fatal(err)
	}
	writer, err := source.connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.client.Select("Clients", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.client.Move(protocol.UIDSetNum(added.UID), "Archive").Wait(); err != nil {
		t.Fatal(err)
	}
	location := mail.Location{UIDValidity: added.UIDValidity, UID: uint32(added.UID)}
	if _, err := source.ReadContent(t.Context(), "Clients", location); fault.From(err, "").Code != "message_not_found" {
		t.Fatal(err)
	}
	if err := user.Delete("Clients"); err != nil {
		t.Fatal(err)
	}
	if _, err := source.ReadContent(t.Context(), "Clients", location); fault.From(err, "").Code != "message_not_found" {
		t.Fatal(err)
	}
}

func TestReadContentCancelsAStalledFetch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	source, user := fixtureWithFetch(t, func(_ *protocol.FetchOptions) { cancel() })
	added, err := user.Append("Clients", strings.NewReader("Content-Type: text/plain\r\n\r\nhello"), &protocol.AppendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.ReadContent(ctx, "Clients", mail.Location{UIDValidity: added.UIDValidity, UID: uint32(added.UID)}); err == nil {
		t.Fatal("cancelled fetch succeeded")
	}
}

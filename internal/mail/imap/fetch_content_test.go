package imap

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/mingzaily/mailwake/internal/content"
	"github.com/mingzaily/mailwake/internal/fault"
)

func TestContentPartSurvivesFlagsAndBodyBeforeUID(t *testing.T) {
	const raw = "<p>正文</p>"
	response := fmt.Sprintf("* 1 FETCH (UID 1 FLAGS ())\r\n* 1 FETCH (BODY[1]<0> {%d}\r\n%s UID 1)\r\n", len(raw), raw)
	s := metadataSession(t, response, response)
	reader := contentParts{connection: s.connection, uid: 1, remaining: content.MaxReadBytes}
	body, err := reader.fetch(t.Context(), &protocol.FetchItemBodySection{Part: []int{1}, Peek: true}, 128)
	if err != nil || string(body) != raw || reader.remaining != content.MaxReadBytes-len(raw) {
		t.Fatalf("part: %q %v", body, err)
	}
}

func TestBodyStructureSurvivesFlagUpdate(t *testing.T) {
	response := "* 1 FETCH (UID 1 FLAGS ())\r\n* 1 FETCH (UID 1 BODYSTRUCTURE (\"TEXT\" \"PLAIN\" (\"CHARSET\" \"UTF-8\") NIL NIL \"7BIT\" 10 1))\r\n"
	s := metadataSession(t, response, response)
	items, err := s.fetch(t.Context(), "message_unavailable", protocol.UIDSetNum(1), &protocol.FetchOptions{BodyStructure: &protocol.FetchItemBodyStructure{Extended: true}}, 0)
	if err != nil || items[1] == nil || items[1].BodyStructure == nil {
		t.Fatal("structure lost", err)
	}
}

func TestBodyLimitsCoverBothUIDOrders(t *testing.T) {
	for _, bodyFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(bodyFirst), func(t *testing.T) {
			raw := strings.Repeat("x", 130)
			body := fmt.Sprintf("BODY[1]<0> {%d}\r\n%s", len(raw), raw)
			fields := "UID 1 " + body
			if bodyFirst {
				fields = body + " UID 1"
			}
			response := "* 1 FETCH (" + fields + ")\r\n"
			s := metadataSession(t, response, response)
			reader := contentParts{connection: s.connection, uid: 1, remaining: 128}
			_, err := reader.fetch(t.Context(), &protocol.FetchItemBodySection{Part: []int{1}, Peek: true}, 128)
			if fault.From(err, "").Code != "message_too_large" {
				t.Fatal("size limit", err)
			}
		})
	}
}

func TestPreUIDLiteralLimitRejectsBeforeReadingBody(t *testing.T) {
	response := fmt.Sprintf("* 1 FETCH (BODY[]<0> {%d}\r\n", content.MaxReadBytes+2)
	s := metadataSession(t, response, response)
	batch, err := s.Poll(t.Context(), `{"validity":1,"next":1}`, true)
	if fault.From(err, "").Code != "imap_metadata_failed" || batch.Checkpoint != "" {
		t.Fatalf("unbounded pre-UID body: %+v %v", batch, err)
	}
}

func TestDuplicateBodyResponsesShareOneReadBudget(t *testing.T) {
	raw := strings.Repeat("x", 80)
	response := fmt.Sprintf("* 1 FETCH (UID 1 BODY[1]<0> {%d}\r\n%s)\r\n", len(raw), raw)
	s := metadataSession(t, response+response, response+response)
	reader := contentParts{connection: s.connection, uid: 1, remaining: 128}
	_, err := reader.fetch(t.Context(), &protocol.FetchItemBodySection{Part: []int{1}, Peek: true}, 128)
	if fault.From(err, "").Code != "message_too_large" {
		t.Fatal("duplicate responses escaped shared budget", err)
	}
}

func TestUnrelatedPreUIDBodyKeepsRequestedMessageIsolated(t *testing.T) {
	unrelated := "Unrelated private body"
	expected := "Requested body"
	response := fmt.Sprintf("* 3 FETCH (BODY[1]<0> {%d}\r\n%s UID 3)\r\n* 1 FETCH (BODY[1]<0> {%d}\r\n%s UID 1)\r\n", len(unrelated), unrelated, len(expected), expected)
	s := metadataSession(t, response, response)
	reader := contentParts{connection: s.connection, uid: 1, remaining: 128}
	raw, err := reader.fetch(t.Context(), &protocol.FetchItemBodySection{Part: []int{1}, Peek: true}, 128)
	if err != nil || string(raw) != expected {
		t.Fatalf("UID isolation: %q %v", raw, err)
	}
}

func TestCancellationInterruptsBodyInEitherUIDOrder(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(fmt.Sprint(before), func(t *testing.T) {
			fields := "UID 1 BODY[]<0> {1000}\r\n"
			if before {
				fields = "BODY[]<0> {1000}\r\n"
			}
			started := make(chan struct{})
			s := metadataSessionWithSearch(t, "* 1 FETCH ("+fields, "", func(int) string { return "1 2" }, func() { close(started) })
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := s.Poll(ctx, `{"validity":1,"next":1}`, true); done <- err }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("literal response did not start")
			}
			cancel()
			select {
			case err := <-done:
				if err != context.Canceled {
					t.Fatal("cancellation lost", err)
				}
			case <-time.After(time.Second):
				t.Fatal("literal read remained blocked")
			}
		})
	}
}

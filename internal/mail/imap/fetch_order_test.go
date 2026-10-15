package imap

import (
	"fmt"
	"testing"
)

func TestBodyBeforeUIDPreservesMessageAndCode(t *testing.T) {
	raw := "Subject: First\r\n\r\nYour verification code is 482913\r\n"
	response := fmt.Sprintf("* 1 FETCH (BODY[]<0> {%d}\r\n%s UID 1 ENVELOPE (NIL \"First\" NIL NIL NIL NIL NIL NIL NIL NIL) INTERNALDATE \"15-Oct-2026 03:25:33 +0000\" RFC822.SIZE %d)\r\n", len(raw), raw, len(raw))
	s := metadataSession(t, response, response)
	batch, err := s.Poll(t.Context(), `{"validity":1,"next":1}`, true)
	t.Logf("count=%d checkpoint=%s err=%v", len(batch.Messages), batch.Checkpoint, err)
	if err != nil || len(batch.Messages) != 2 || (batch.Messages[0].Subject != "First" || batch.Messages[0].Code != "482913") {
		t.Fatal("existing message skipped")
	}
}

func TestRepeatedFlagsBeforeMetadataProgresses(t *testing.T) {
	response := "* 1 FETCH (UID 1 FLAGS ())\r\n* 1 FETCH (UID 1 ENVELOPE (NIL \"First\" NIL NIL NIL NIL NIL NIL NIL NIL) INTERNALDATE \"15-Oct-2026 03:25:33 +0000\")\r\n"
	s := metadataSession(t, response, response)
	for i := 0; i < 3; i++ {
		batch, err := s.Poll(t.Context(), `{"validity":1,"next":1}`, false)
		t.Logf("poll=%d count=%d checkpoint=%s err=%v", i, len(batch.Messages), batch.Checkpoint, err)
		if err != nil || len(batch.Messages) != 2 || batch.Messages[0].Subject != "First" || batch.Checkpoint != `{"validity":1,"next":3}` {
			t.Fatalf("complete responses failed: %+v %v", batch, err)
		}
	}
}

func TestMissingFetchSkipsOnlyConfirmedExpungedUIDs(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(fmt.Sprint(deleted), func(t *testing.T) {
			s := metadataSessionWithSearch(t, "", "", func(round int) string {
				if round > 1 && deleted {
					return ""
				}
				if round > 1 {
					return "1"
				}
				return "1 2"
			})
			batch, err := s.Poll(t.Context(), `{"validity":1,"next":1}`, false)
			if deleted {
				if err != nil || len(batch.Messages) != 1 || batch.Messages[0].Location.UID != 2 || batch.Checkpoint != `{"validity":1,"next":3}` {
					t.Fatalf("deleted UID: %+v %v", batch, err)
				}
			} else if err == nil || batch.Checkpoint != "" || len(batch.Messages) != 0 {
				t.Fatalf("existing UID skipped: %+v %v", batch, err)
			}
		})
	}
}

func TestSplitMetadataIsCombinedBeforeCheckpoint(t *testing.T) {
	response := "* 1 FETCH (UID 1 ENVELOPE (NIL \"First\" NIL NIL NIL NIL NIL NIL NIL NIL))\r\n* 1 FETCH (UID 1 INTERNALDATE \"15-Oct-2026 03:25:33 +0000\")\r\n"
	s := metadataSession(t, response, response)
	batch, err := s.Poll(t.Context(), `{"validity":1,"next":1}`, false)
	if err != nil || len(batch.Messages) != 2 || batch.Messages[0].Subject != "First" {
		t.Fatalf("split metadata: %+v %v", batch, err)
	}
}

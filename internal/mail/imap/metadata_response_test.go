package imap

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/mingzaily/mailwake/internal/content"
	"github.com/mingzaily/mailwake/internal/fault"
)

func TestIncompleteMetadataKeepsBatchForNextPoll(t *testing.T) {
	const complete = `UID 1 ENVELOPE (NIL "=?UTF-8?B?5rWL6K+V5Li76aKY?=" NIL NIL NIL NIL NIL NIL NIL NIL) INTERNALDATE "15-Oct-2026 03:25:33 +0000"`
	for _, tc := range []struct {
		name, response string
		incomplete     bool
		subject        string
	}{
		{"complete", "* 1 FETCH (" + complete + ")\r\n", false, "测试主题"},
		{"flags precede metadata", "* 1 FETCH (UID 1 FLAGS ())\r\n* 1 FETCH (" + complete + ")\r\n", false, "测试主题"},
		{"flags follow metadata", "* 1 FETCH (" + complete + ")\r\n* 1 FETCH (UID 1 FLAGS ())\r\n", false, "测试主题"},
		{"missing envelope", "* 1 FETCH (UID 1 INTERNALDATE \"15-Oct-2026 03:25:33 +0000\")\r\n", true, "测试主题"},
		{"missing internal date", "* 1 FETCH (UID 1 ENVELOPE (NIL \"Subject\" NIL NIL NIL NIL NIL NIL NIL NIL))\r\n", true, "测试主题"},
		{"genuine empty subject", "* 1 FETCH (" + strings.Replace(complete, "\"=?UTF-8?B?5rWL6K+V5Li76aKY?=\"", "NIL", 1) + ")\r\n", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sess := metadataSession(t, tc.response, "* 1 FETCH ("+complete+")\r\n")
			const checkpoint = `{"validity":1,"next":1}`
			batch, err := sess.Poll(t.Context(), checkpoint, false)
			if tc.incomplete {
				if fault.From(err, "").Code != "imap_metadata_failed" || batch.Checkpoint != "" || len(batch.Messages) != 0 {
					t.Fatalf("incomplete batch exposed: %+v, %v", batch, err)
				}
				batch, err = sess.Poll(t.Context(), checkpoint, false)
			}
			if err != nil || len(batch.Messages) != 2 {
				t.Fatalf("complete batch: %+v, %v", batch, err)
			}
			if batch.Messages[0].Subject != tc.subject || batch.Messages[1].Subject != "Second" || batch.Checkpoint != `{"validity":1,"next":3}` {
				t.Fatalf("metadata lost: %+v", batch)
			}
		})
	}
}

// Every poll receives a complete second message first, exercising whole-batch
// rollback when the first message's metadata is incomplete.
func metadataSession(t *testing.T, first, subsequent string) *session {
	return metadataSessionWithSearch(t, first, subsequent, func(int) string { return "1 2" })
}
func metadataSessionWithSearch(t *testing.T, first, subsequent string, search func(int) string, onFetch ...func()) *session {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer serverConn.Close()
		fmt.Fprint(serverConn, "* PREAUTH [CAPABILITY IMAP4rev1] ready\r\n")
		reader := bufio.NewReader(serverConn)
		response := first
		searches := 0
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			parts := strings.Fields(line)
			if len(parts) < 2 {
				return
			}
			tag := parts[0]
			switch {
			case strings.Contains(line, "UID SEARCH"):
				searches++
				fmt.Fprintf(serverConn, "* SEARCH %s\r\n%s OK searched\r\n", search(searches), tag)
			case strings.Contains(line, "UID FETCH"):
				fmt.Fprint(serverConn, "* 2 FETCH (UID 2 ENVELOPE (NIL \"Second\" NIL NIL NIL NIL NIL NIL NIL NIL) INTERNALDATE \"15-Oct-2026 03:25:33 +0000\")\r\n")
				fmt.Fprint(serverConn, response)
				for _, notify := range onFetch {
					notify()
				}
				fmt.Fprintf(serverConn, "%s OK fetched\r\n", tag)
				response = subsequent
			default:
				fmt.Fprintf(serverConn, "%s OK done\r\n", tag)
			}
		}
	}()
	client := imapclient.New(clientConn, &imapclient.Options{WordDecoder: headerDecoder, MaxBufferedFetchBytes: content.MaxReadBytes + 1})
	t.Cleanup(func() { client.Close(); serverConn.Close(); <-done })
	return &session{connection: &connection{conn: clientConn, client: client, timeout: 3 * time.Second, updates: make(chan struct{}, 1)}, validity: 1}
}

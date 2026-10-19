package imap

import (
	"encoding/base64"
	"strings"
	"testing"

	protocol "github.com/emersion/go-imap/v2"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestFromNamePreservesAddressAndDecodesHeaders(t *testing.T) {
	gbk, err := simplifiedchinese.GBK.NewEncoder().String("账单提醒")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ from, name string }{
		{"Alice <sender@example.org>", "Alice"},
		{"sender@example.org", ""},
		{"通知中心 <sender@example.org>", "通知中心"},
		{"=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte("通知中心")) + "?= <sender@example.org>", "通知中心"},
		{"=?GBK?B?" + base64.StdEncoding.EncodeToString([]byte(gbk)) + "?= <sender@example.org>", "账单提醒"},
	} {
		t.Run(tc.from, func(t *testing.T) {
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
			_, err = user.Append("Clients", strings.NewReader("From: "+tc.from+"\r\nSubject: hello\r\n\r\nbody"), &protocol.AppendOptions{})
			if err != nil {
				t.Fatal(err)
			}
			batch, err := watcher.Poll(t.Context(), baseline.Checkpoint, false)
			if err != nil || len(batch.Messages) != 1 {
				t.Fatalf("batch: %+v %v", batch, err)
			}
			if got := batch.Messages[0]; got.Sender != "sender@example.org" || got.SenderName != tc.name {
				t.Fatalf("sender: %+v", got)
			}
		})
	}
}

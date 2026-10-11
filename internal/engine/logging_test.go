package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/logging"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/storage"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

type failingLogSource struct{ err error }

func (s failingLogSource) Folders(context.Context) (*mail.FolderDiscovery, error) { return nil, nil }
func (s failingLogSource) Open(context.Context, string) (mail.Session, error)     { return nil, s.err }
func TestReconnectLogsKeepRawErrorsPrivate(t *testing.T) {
	for _, tc := range []struct {
		err   error
		code  string
		delay float64
	}{{errors.New("private-subject private-password https://host/?token=private-route"), "request_failed", 1}, {fault.New(mail.CodeAuthFailed), mail.CodeAuthFailed, 1800}} {
		synctest.Test(t, func(t *testing.T) {
			store, err := storage.Open(t.Context(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			var output bytes.Buffer
			log := logging.New(&output)
			e := New(failingLogSource{tc.err}, store, "mbx_test", mail.Subscriptions{Folders: mail.RealtimeFolders([]string{"INBOX"})}, time.Minute, true, log)
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})
			go func() { defer close(done); e.Run(ctx) }()
			synctest.Wait()
			cancel()
			<-done
			p := logging.Read(log, logging.Query{})
			if len(p.Entries) != 1 {
				t.Fatalf("reconnect event count: %+v", p)
			}
			entry := p.Entries[0]
			if entry.Level != "warn" || entry.Attrs["code"] != tc.code || entry.Attrs["mailbox_id"] != "mbx_test" || entry.Attrs["folder"] != "INBOX" || entry.Attrs["backoff_seconds"].(float64) < tc.delay {
				t.Fatalf("reconnect event: %+v", entry)
			}
			data, _ := json.Marshal(p)
			if strings.Contains(output.String(), "private-") || strings.Contains(string(data), "private-") {
				t.Fatal("raw error leaked")
			}
		})
	}
}

func (s failingLogSource) ReadContent(context.Context, string, mail.Location) (mail.Body, error) {
	return mail.Body{}, nil
}

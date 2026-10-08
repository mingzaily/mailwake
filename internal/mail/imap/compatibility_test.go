package imap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/buildinfo"
	"github.com/mingzaily/mailwake/internal/engine"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestIdentificationBeforeFolderCommands(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options serverOptions
		wantID  bool
		mode    string
	}{
		{"NetEase", serverOptions{caps: "IMAP4rev1 ID IDLE", requireID: true}, true, "idle"},
		{"unsupported", serverOptions{caps: "IMAP4rev1"}, false, "poll"},
		{"rejected", serverOptions{caps: "IMAP4rev1 ID IDLE", rejectID: true}, true, "idle"},
		{"login_capabilities", serverOptions{caps: "IMAP4rev1 ID IDLE", loginCaps: true, requireID: true}, true, "idle"},
	} {
		for _, operation := range []string{"EXAMINE", "LIST"} {
			t.Run(tc.name+"/"+operation, func(t *testing.T) {
				source, commands, _, ended := scriptedServer(t, tc.options)
				source.timeout = time.Second
				var logs bytes.Buffer
				source.log = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
				if operation == "EXAMINE" {
					session, err := source.Open(t.Context(), "Clients")
					if err != nil {
						t.Fatal(err)
					}
					if session.Mode() != tc.mode {
						t.Errorf("mode = %s, want %s", session.Mode(), tc.mode)
					}
					session.Close()
				} else {
					folders, err := source.Folders(t.Context())
					if err != nil || !slices.Equal(folders, []string{"Clients"}) {
						t.Fatalf("folders = %v, error = %v", folders, err)
					}
				}
				select {
				case <-ended:
				case <-time.After(time.Second):
					t.Fatal("connection remained open")
				}
				var names, ids []string
				for line := range commands {
					_, command, _ := strings.Cut(line, " ")
					name, _, _ := strings.Cut(command, " ")
					names = append(names, name)
					if name == "ID" {
						ids = append(ids, command)
					}
				}
				wantCaps := 1
				if tc.options.loginCaps {
					wantCaps = 0
				}
				count := 0
				for _, name := range names {
					if name == "CAPABILITY" {
						count++
					}
				}
				if count != wantCaps {
					t.Fatalf("capability queries = %d, want %d: %v", count, wantCaps, names)
				}
				if names[0] != "LOGIN" || names[len(names)-1] != operation {
					t.Fatalf("command order: %v", names)
				}
				if tc.wantID {
					version, _ := buildinfo.Current()
					want := fmt.Sprintf(`ID ("name" "Mailwake" "version" %q "vendor" "Mailwake" "support-url" "https://github.com/mingzaily/mailwake")`, version)
					if !slices.Equal(ids, []string{want}) {
						t.Fatalf("ID must contain only product fields: %v", ids)
					}
					if slices.Index(names, "ID") <= slices.Index(names, "CAPABILITY") {
						t.Fatalf("ID precedes capability discovery: %v", names)
					}
				} else if len(ids) != 0 {
					t.Fatalf("ID sent without capability: %v", ids)
				}
				if tc.options.rejectID {
					var record map[string]any
					if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
						t.Fatalf("want one log record: %s (%v)", &logs, err)
					}
					if record["level"] != "DEBUG" || record["msg"] != "IMAP ID command was rejected" {
						t.Fatal(record)
					}
					if strings.Contains(logs.String(), "private server explanation") {
						t.Fatal("raw server text leaked into log")
					}
				} else if logs.Len() != 0 {
					t.Fatalf("unexpected log: %s", &logs)
				}
			})
		}
	}
}

func TestFolderOpenResponseCodeInLastError(t *testing.T) {
	for _, tc := range []struct{ raw, clean string }{
		{"UNAVAILABLE", "UNAVAILABLE"}, {"NONEXISTENT", "NONEXISTENT"}, {"", ""},
		{"CODE_123/ABC.DEF-9", "CODE123/ABCDEF-9"}, {"..._", ""},
		{strings.Repeat("X", 40), strings.Repeat("X", 32)},
	} {
		code := tc.raw
		t.Run("code_"+code, func(t *testing.T) {
			failure := "private-account@example.org private folder detail"
			if code != "" {
				failure = "[" + code + "] " + failure
			}
			source, _, _, _ := scriptedServer(t, serverOptions{caps: "IMAP4rev1", openFailure: failure})
			source.timeout = time.Second
			store, err := storage.Open(t.Context(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			monitor := engine.New(source, store, "test", mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders([]string{"Clients"})}, time.Minute, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})
			go func() { defer close(done); monitor.Run(ctx) }()
			defer func() { cancel(); <-done }()
			deadline := time.Now().Add(time.Second)
			var status engine.FolderStatus
			for time.Now().Before(deadline) {
				status = monitor.Status()[0]
				if status.LastError != nil {
					break
				}
				time.Sleep(time.Millisecond)
			}
			wantCode := "imap_folder_open_failed"
			if tc.clean != "" {
				wantCode = "imap_folder_rejected"
			}
			if status.LastError == nil || status.LastError.Code != wantCode {
				t.Fatalf("missing folder open error: %+v", status)
			}
			params := status.LastError.Params
			if tc.clean != "" && (len(params) != 1 || params["response_code"] != tc.clean) {
				t.Fatalf("response code lost: %v", params)
			}
			if tc.clean == "" && len(params) != 0 {
				t.Fatalf("missing response code must be omitted: %v", params)
			}
			data, err := json.Marshal(status)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "private") {
				t.Fatalf("raw server text leaked: %s", data)
			}
			if tc.clean == "" && strings.Contains(string(data), "response_code") {
				t.Fatalf("unexpected response_code: %s", data)
			}
		})
	}
}

func TestDiscoveryNegotiationTimeout(t *testing.T) {
	for _, command := range []string{"CAPABILITY", "ID"} {
		t.Run(command, func(t *testing.T) {
			source, _, ended := silentServer(t, command)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			_, err := source.Folders(ctx)
			if ctx.Err() != nil || err == nil || err.Error() != "imap_command_timeout" {
				t.Fatalf("negotiation exceeded command deadline: %v (context: %v)", err, ctx.Err())
			}
			select {
			case <-ended:
			case <-time.After(time.Second):
				t.Fatal("timed-out negotiation kept transport open")
			}
		})
	}
}

func TestResponseCodeAlphabetAndLength(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"UNAVAILABLE", "UNAVAILABLE"}, {"A-Z/0123456789", "A-Z/0123456789"},
		{"a_中. A\nB@example.org", "AB"}, {"小写 lowercase?!", ""},
		{strings.Repeat("-/AZ09", 20), strings.Repeat("-/AZ09", 5) + "-/"},
	} {
		if got := responseCode(tc.input); got != tc.want {
			t.Errorf("responseCode(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestScheduledConnectionSelectsFoldersWithOneLogin(t *testing.T) {
	source, commands, _, ended := scriptedServer(t, serverOptions{caps: "IMAP4rev1 ID", requireID: true})
	conn, err := source.OpenScheduled(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"A", "B", "C"} {
		session, err := conn.Select(t.Context(), folder)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := session.Poll(t.Context(), "", false); err != nil {
			t.Fatal(err)
		}
	}
	conn.Close()
	<-ended
	logins, opens := 0, 0
	for command := range commands {
		if strings.Contains(command, " LOGIN ") {
			logins++
		}
		if strings.Contains(command, " EXAMINE ") {
			opens++
		}
	}
	if logins != 1 || opens != 3 {
		t.Fatal("shared connection commands", logins, opens)
	}
}

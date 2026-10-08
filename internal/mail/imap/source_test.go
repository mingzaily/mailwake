package imap

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/mingzaily/mailwake/internal/engine"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/storage"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func fixture(t *testing.T) (*Source, *imapmemserver.User) {
	return fixtureWithFetch(t, nil)
}

func fixtureWithFetch(t *testing.T, observe func(*protocol.FetchOptions)) (*Source, *imapmemserver.User) {
	t.Helper()
	certServer := httptest.NewTLSServer(nil)
	certificates := certServer.TLS.Certificates
	pool := x509.NewCertPool()
	pool.AddCert(certServer.Certificate())
	certServer.Close()
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: certificates, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	backend := imapmemserver.New()
	user := imapmemserver.NewUser("test@example.org", "test-password")
	if err := user.Create("Clients", nil); err != nil {
		t.Fatal(err)
	}
	backend.AddUser(user)
	server := imapserver.New(&imapserver.Options{NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		return &fetchObservedSession{Session: backend.NewSession(), observe: observe}, nil, nil
	}, Caps: protocol.CapSet{protocol.CapIMAP4rev1: {}, protocol.CapIdle: {}}, Logger: log.New(io.Discard, "", 0)})
	done := make(chan struct{})
	go func() { defer close(done); server.Serve(listener) }()
	t.Cleanup(func() { server.Close(); <-done })
	port := listener.Addr().(*net.TCPAddr).Port
	s := New("127.0.0.1", port, "test@example.org", "test-password", slog.Default())
	s.tlsConfig.RootCAs = pool
	return s, user
}

func appendMail(t *testing.T, user *imapmemserver.User, subject string) {
	t.Helper()
	message := "From: Sender <sender@example.org>\r\nTo: test@example.org\r\nSubject: " + subject + "\r\nDate: Fri, 25 Sep 2026 12:00:00 +0000\r\n\r\nbody must stay unread\r\n"
	if _, err := user.Append("Clients", strings.NewReader(message), &protocol.AppendOptions{Time: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func TestRejectedLoginReportsAuthFailure(t *testing.T) {
	s, _ := fixture(t)
	s.password = "wrong-password"
	_, err := s.Open(t.Context(), "Clients")
	if code := fault.From(err, "").Code; code != mail.CodeAuthFailed {
		t.Fatalf("rejected credentials code %q (%v)", code, err)
	}
}

func TestExplicitIMAPRootsPreserveHostnameAndAuthUsername(t *testing.T) {
	s, _ := fixture(t)
	host, portText, _ := net.SplitHostPort(s.address)
	port, _ := strconv.Atoi(portText)
	defaults := New(host, port, s.username, s.password, s.log)
	if _, err := defaults.Folders(t.Context()); fault.From(err, "").Code != "imap_connection_failed" {
		t.Fatal("unknown CA default", err)
	}
	configured := NewWithOptions(host, port, s.username, s.password, s.log, Options{RootCAs: s.tlsConfig.RootCAs})
	if _, err := configured.Folders(t.Context()); err != nil {
		t.Fatal("explicit CA and original username", err)
	}
	wrong := NewWithOptions("wrong.stage35.test", port, s.username, s.password, s.log, Options{RootCAs: s.tlsConfig.RootCAs, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, s.address)
	}})
	if _, err := wrong.Folders(t.Context()); fault.From(err, "").Code != "imap_connection_failed" {
		t.Fatal("wrong hostname accepted", err)
	}
}

func TestLegacyCharsetHeadersAreDecoded(t *testing.T) {
	encoded, err := simplifiedchinese.GBK.NewEncoder().String("您的 9 月账单已出")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := headerDecoder.DecodeHeader("=?GBK?B?" + base64.StdEncoding.EncodeToString([]byte(encoded)) + "?=")
	if err != nil || subject != "您的 9 月账单已出" {
		t.Fatalf("GBK subject %q %v", subject, err)
	}
}

func TestBaselineCatchupIdleAndEpochReset(t *testing.T) {
	s, user := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	appendMail(t, user, "old mail")
	folders, err := s.Folders(ctx)
	if err != nil || len(folders) != 1 || folders[0] != "Clients" {
		t.Fatalf("发现文件夹: %v %v", folders, err)
	}
	opened, err := s.Open(ctx, "Clients")
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	sess := opened.(*session)
	baseline, err := sess.Poll(ctx, "", false)
	if err != nil || len(baseline.Messages) != 0 || baseline.Checkpoint == "" {
		t.Fatalf("首次建立基线: %+v %v", baseline, err)
	}
	for i := 0; i < 501; i++ {
		appendMail(t, user, fmt.Sprintf("new %d", i))
	}
	batch, err := sess.Poll(ctx, baseline.Checkpoint, false)
	if err != nil || len(batch.Messages) != 500 || !batch.More {
		t.Fatalf("分批补查: %d %v %v", len(batch.Messages), batch.More, err)
	}
	if batch.Messages[0].Sender != "sender@example.org" || batch.Messages[0].Subject != "new 0" {
		t.Fatalf("元数据: %+v", batch.Messages[0])
	}
	replay, err := sess.Poll(ctx, baseline.Checkpoint, false)
	if err != nil || replay.Messages[0].Key != batch.Messages[0].Key {
		t.Fatalf("同游标应返回相同标识: %v", err)
	}
	last, err := sess.Poll(ctx, batch.Checkpoint, false)
	if err != nil || len(last.Messages) != 1 || last.More {
		t.Fatalf("最后一批: %+v %v", last, err)
	}
	unchanged, err := sess.Poll(ctx, last.Checkpoint, false)
	if err != nil || len(unchanged.Messages) != 0 {
		t.Fatalf("相同进度重复通知: %+v %v", unchanged, err)
	}
	if sess.Mode() != "idle" {
		t.Fatal("应使用 IDLE")
	}
	wait := make(chan error, 1)
	go func() { wait <- sess.Wait(ctx, 5*time.Second) }()
	// 内存测试服务器在进入 IDLE 后才安装更新订阅。
	time.Sleep(50 * time.Millisecond)
	appendMail(t, user, "idle mail")
	select {
	case err := <-wait:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("IDLE 未及时唤醒")
	}
	fresh, err := sess.Poll(ctx, last.Checkpoint, false)
	if err != nil || len(fresh.Messages) != 1 {
		t.Fatalf("IDLE 后补查: %+v %v", fresh, err)
	}
	status, err := user.Status("Clients", &protocol.StatusOptions{NumUnseen: true})
	if err != nil || *status.NumUnseen != 503 {
		t.Fatalf("读取应保留未读标记: %+v %v", status, err)
	}
	var old cursor
	if err := json.Unmarshal([]byte(fresh.Checkpoint), &old); err != nil {
		t.Fatal(err)
	}
	old.Validity++
	encoded, _ := json.Marshal(old)
	reset, err := sess.Poll(ctx, string(encoded), false)
	if err != nil || !reset.Reset || len(reset.Messages) != 0 {
		t.Fatalf("UIDVALIDITY 变更应重建基线: %+v %v", reset, err)
	}
	sess.idle = false
	if sess.Mode() != "poll" {
		t.Fatal("无 IDLE 应周期补查")
	}
	if err := sess.Wait(ctx, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Poll(ctx, "broken", false); err == nil {
		t.Fatal("损坏游标应报告错误")
	}
}

func TestEngineRestartResumesAndKeepsPreviewPrivate(t *testing.T) {
	source, user := fixture(t)
	ctx := context.Background()
	dir := t.TempDir()
	store, err := storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	appendMail(t, user, "history")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	start := func() (context.CancelFunc, <-chan struct{}) {
		runCtx, cancel := context.WithCancel(ctx)
		monitor := engine.New(source, store, "primary", mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders([]string{"Clients"})}, 50*time.Millisecond, false, logger)
		done := make(chan struct{})
		go func() { defer close(done); monitor.Run(runCtx) }()
		return cancel, done
	}
	stop, done := start()
	defer func() { stop(); <-done }()
	eventually(t, func() bool { cp, err := store.Checkpoint(ctx, "primary", "Clients"); return err == nil && cp != "" })
	summary, err := store.Summary(ctx)
	if err != nil || summary.Pending != 0 {
		t.Fatalf("历史邮件应跳过: %+v %v", summary, err)
	}
	appendMail(t, user, "new private subject")
	eventually(t, func() bool { summary, err := store.Summary(ctx); return err == nil && summary.Pending == 1 })
	task, err := store.Due(ctx, time.Now().Add(time.Second))
	if err != nil || task == nil || task.Event.Subject != "" || task.Event.Sender != "" {
		t.Fatalf("关闭预览后应只存通知元数据: %+v %v", task, err)
	}
	stop()
	<-done
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	appendMail(t, user, "while offline")
	store, err = storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	stop, done = start()
	eventually(t, func() bool { summary, err := store.Summary(ctx); return err == nil && summary.Pending == 2 })
	stop()
	<-done
	summary, err = store.Summary(ctx)
	if err != nil || summary.Pending != 2 {
		t.Fatalf("重启应续接且任务唯一: %+v %v", summary, err)
	}
}

func TestRecreatedFolderResetsOnNewSelection(t *testing.T) {
	source, user := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := source.Open(ctx, "Clients")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	baseline, err := sess.Poll(ctx, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := user.Delete("Clients"); err != nil {
		t.Fatal(err)
	}
	if err := user.Create("Clients", nil); err != nil {
		t.Fatal(err)
	}
	appendMail(t, user, "new folder history")
	// The old selection must not report mail from another UID epoch; the engine's
	// session lifetime reopens it and the new selection resets the baseline.
	if stale, err := sess.Poll(ctx, baseline.Checkpoint, false); err == nil && len(stale.Messages) != 0 {
		t.Fatalf("旧选择会话不应产生通知: %+v", stale)
	}
	fresh, err := source.Open(ctx, "Clients")
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	batch, err := fresh.Poll(ctx, baseline.Checkpoint, false)
	if err != nil || !batch.Reset || len(batch.Messages) != 0 {
		t.Fatalf("新文件夹应重建基线: %+v %v", batch, err)
	}
}

func eventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("等待状态变化超时")
}

type observedSource struct {
	mail.Source
	closed chan string
}
type observedSession struct {
	mail.Session
	folder string
	closed chan string
}

func (s observedSource) Open(ctx context.Context, folder string) (mail.Session, error) {
	session, err := s.Source.Open(ctx, folder)
	if err != nil {
		return nil, err
	}
	return &observedSession{Session: session, folder: folder, closed: s.closed}, nil
}
func (s *observedSession) Close() error {
	err := s.Session.Close()
	s.closed <- s.folder
	return err
}

func TestLiveResubscriptionStartsFromNewBaseline(t *testing.T) {
	source, user := fixture(t)
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	initial, err := store.LoadSubscriptions(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan string, 10)
	monitor := engine.New(observedSource{source, closed}, store, "a", initial, 50*time.Millisecond, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); monitor.Run(ctx) }()
	defer func() { cancel(); <-done }()
	appendMail(t, user, "existing history")
	state, err := monitor.UpdateSubscriptions(t.Context(), mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders([]string{"Clients"})})
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { cp, err := store.Checkpoint(t.Context(), "a", "Clients"); return err == nil && cp != "" })
	appendMail(t, user, "first new mail")
	eventually(t, func() bool { summary, err := store.Summary(t.Context()); return err == nil && summary.Pending == 1 })
	state.Folders = []mail.Folder{}
	state, err = monitor.UpdateSubscriptions(t.Context(), state)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("unsubscribe kept the IMAP connection open")
	}
	if len(monitor.Status()) != 0 {
		t.Fatal(monitor.Status())
	}
	appendMail(t, user, "while unsubscribed")
	state.Folders = mail.RealtimeFolders([]string{"Clients"})
	if _, err = monitor.UpdateSubscriptions(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		status := monitor.Status()
		return len(status) == 1 && status[0].State == "watching"
	})
	appendMail(t, user, "after resubscribing")
	eventually(t, func() bool { summary, err := store.Summary(t.Context()); return err == nil && summary.Pending == 2 })
	// Several poll intervals: a replay of the unsubscribed period would raise the count to 3.
	time.Sleep(300 * time.Millisecond)
	cancel()
	<-done
	summary, err := store.Summary(t.Context())
	if err != nil || summary.Pending != 2 {
		t.Fatalf("resubscription must skip mail from the unsubscribed period: %+v %v", summary, err)
	}
}

func TestStaleMailFiledIntoFolderIsSkipped(t *testing.T) {
	source, user := fixture(t)
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	monitor := engine.New(source, store, "a", mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders([]string{"Clients"})}, 50*time.Millisecond, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); monitor.Run(ctx) }()
	defer func() { cancel(); <-done }()
	eventually(t, func() bool { cp, err := store.Checkpoint(t.Context(), "a", "Clients"); return err == nil && cp != "" })
	old := "From: Sender <sender@example.org>\r\nSubject: filed by hand\r\n\r\nbody\r\n"
	if _, err := user.Append("Clients", strings.NewReader(old), &protocol.AppendOptions{Time: time.Now().Add(-72 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	appendMail(t, user, "fresh mail")
	eventually(t, func() bool { summary, err := store.Summary(t.Context()); return err == nil && summary.Pending == 1 })
	time.Sleep(200 * time.Millisecond)
	if summary, err := store.Summary(t.Context()); err != nil || summary.Pending != 1 {
		t.Fatalf("mail received three days ago must not notify: %+v %v", summary, err)
	}
}

// Observe the server-side request to verify the body access and PEEK boundary.
type fetchObservedSession struct {
	imapserver.Session
	observe func(*protocol.FetchOptions)
}

func (s *fetchObservedSession) Fetch(w *imapserver.FetchWriter, set protocol.NumSet, options *protocol.FetchOptions) error {
	if s.observe != nil {
		s.observe(options)
	}
	return s.Session.Fetch(w, set, options)
}

func TestPollDetectsCodesOnDemandAndKeepsMailUnread(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("detect=%v", enabled), func(t *testing.T) {
			requests := make(chan *protocol.FetchOptions, 1)
			source, user := fixtureWithFetch(t, func(options *protocol.FetchOptions) {
				if options.Envelope {
					requests <- options
				}
			})
			opened, err := source.Open(t.Context(), "Clients")
			if err != nil {
				t.Fatal(err)
			}
			defer opened.Close()
			sess := opened.(*session)
			baseline, err := sess.Poll(t.Context(), "", enabled)
			if err != nil {
				t.Fatal(err)
			}
			message := "From: sender@example.org\r\nSubject: Sign in\r\n\r\nYour verification code is 482913\r\n"
			appended, err := user.Append("Clients", strings.NewReader(message), &protocol.AppendOptions{Time: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			batch, err := sess.Poll(t.Context(), baseline.Checkpoint, enabled)
			if err != nil || len(batch.Messages) != 1 {
				t.Fatalf("batch=%+v err=%v", batch, err)
			}
			want := ""
			if enabled {
				want = "482913"
			}
			if batch.Messages[0].Code != want {
				t.Fatalf("code=%q, want %q", batch.Messages[0].Code, want)
			}
			options := <-requests
			if enabled {
				if len(options.BodySection) != 1 {
					t.Fatal("expected one body section")
				}
				section := options.BodySection[0]
				if !section.Peek || section.Partial == nil || section.Partial.Offset != 0 || section.Partial.Size != 256<<10 {
					t.Fatalf("expected BODY.PEEK[]<0.262144>, got %+v", section)
				}
			} else if len(options.BodySection) != 0 {
				t.Fatal("body requested with detection disabled")
			}
			messages, err := sess.client.Fetch(protocol.UIDSetNum(appended.UID), &protocol.FetchOptions{Flags: true}).Collect()
			if err != nil || len(messages) != 1 {
				t.Fatalf("flags: %v %v", messages, err)
			}
			if slices.Contains(messages[0].Flags, protocol.FlagSeen) {
				t.Fatal("mail marked as read")
			}
		})
	}
}

func TestPollOmitsCodesFromTruncatedOrMalformedMessages(t *testing.T) {
	header := "From: sender@example.org\r\nSubject: Sign in\r\n\r\n"
	body := "Your verification code is 482913\r\n"
	for _, tc := range []struct{ name, raw, want string }{
		{"exact limit", header + body + strings.Repeat("x", (256<<10)-len(header)-len(body)), "482913"},
		{"truncated", header + body + strings.Repeat("x", 256<<10), ""},
		{"broken base64", "From: sender@example.org\r\nSubject: Sign in\r\nContent-Type: text/plain\r\nContent-Transfer-Encoding: base64\r\n\r\n" + base64.StdEncoding.EncodeToString([]byte("Your verification code is 4829")) + "!", ""},
		{"malformed MIME", "From: sender@example.org\r\nSubject: Sign in\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain\r\n\r\n" + body, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, user := fixture(t)
			sess, err := source.Open(t.Context(), "Clients")
			if err != nil {
				t.Fatal(err)
			}
			defer sess.Close()
			baseline, err := sess.Poll(t.Context(), "", true)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := user.Append("Clients", strings.NewReader(tc.raw), &protocol.AppendOptions{Time: time.Now()}); err != nil {
				t.Fatal(err)
			}
			batch, err := sess.Poll(t.Context(), baseline.Checkpoint, true)
			if err != nil || len(batch.Messages) != 1 {
				t.Fatalf("batch=%+v err=%v", batch, err)
			}
			if batch.Messages[0].Code != tc.want || batch.Messages[0].Subject != "Sign in" || batch.Checkpoint == baseline.Checkpoint {
				t.Fatalf("message was not processed as expected: %+v", batch)
			}
		})
	}
}

func TestCodeDetectionCatchesUpAcrossReconnects(t *testing.T) {
	source, user := fixture(t)
	sess, err := source.Open(t.Context(), "Clients")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	baseline, err := sess.Poll(t.Context(), "", true)
	if err != nil {
		t.Fatal(err)
	}
	const total = 19
	raw := "From: sender@example.org\r\nSubject: Sign in\r\n\r\nYour verification code is 482913\r\n"
	raw += strings.Repeat("x", (256<<10)-len(raw))
	for range total {
		if _, err := user.Append("Clients", strings.NewReader(raw), &protocol.AppendOptions{Time: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	checkpoint := baseline.Checkpoint
	seen := make(map[string]bool)
	for len(seen) < total {
		batch, err := sess.Poll(t.Context(), checkpoint, true)
		if err != nil {
			t.Fatal(err)
		}
		if batch.Checkpoint == checkpoint || len(batch.Messages) == 0 {
			t.Fatal("backlog progress stalled")
		}
		if len(seen) == 0 && !batch.More {
			t.Fatal("expected bounded progress through the message backlog")
		}
		for _, msg := range batch.Messages {
			if seen[msg.Key] {
				t.Fatalf("message %s repeated after reconnect", msg.Key)
			}
			if msg.Code != "482913" || msg.Subject != "Sign in" {
				t.Fatalf("mail content lost: %+v", msg)
			}
			seen[msg.Key] = true
		}
		if batch.More != (len(seen) < total) {
			t.Fatalf("more=%v after %d messages", batch.More, len(seen))
		}
		checkpoint = batch.Checkpoint
		// Resume each successful batch from its checkpoint on a fresh connection.
		if err := sess.Close(); err != nil {
			t.Fatal(err)
		}
		sess, err = source.Open(t.Context(), "Clients")
		if err != nil {
			t.Fatal(err)
		}
		defer sess.Close()
	}
	caughtUp, err := sess.Poll(t.Context(), checkpoint, true)
	if err != nil || len(caughtUp.Messages) != 0 || caughtUp.More || caughtUp.Checkpoint != checkpoint {
		t.Fatalf("catch-up did not finish: %+v, %v", caughtUp, err)
	}
}

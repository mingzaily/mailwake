package delivery

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/storage"
)

type senderFunc func(context.Context, event.Notification) error

func (f senderFunc) Send(ctx context.Context, n event.Notification) error { return f(ctx, n) }

func TestDefaultRequestsOnceAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	if err := s.Enqueue(ctx, event.Notification{ID: "one-request"}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	sender := senderFunc(func(context.Context, event.Notification) error {
		calls++
		return &Failure{Code: "network_timeout", Retryable: true}
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := New(s, sender, 0, logger)
	if _, err := d.deliverOne(ctx, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	d = New(s, sender, 0, logger)
	if worked, err := d.deliverOne(ctx, time.Now().Add(time.Hour)); err != nil || worked || calls != 1 {
		t.Fatalf("默认失败后仍重试: worked=%v calls=%d err=%v", worked, calls, err)
	}
	// 模拟请求已开始、进程在保存结果前退出，重启后同样结束任务。
	if err := s.Enqueue(ctx, event.Notification{ID: "interrupted"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Attempt(ctx, "interrupted"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.deliverOne(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("结果未知的请求被自动重复发送")
	}
}

func TestDispatcherRetriesAndAccepts(t *testing.T) {
	ctx := context.Background()
	s, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Enqueue(ctx, event.Notification{ID: "n"}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	d := New(s, senderFunc(func(context.Context, event.Notification) error {
		calls++
		if calls == 1 {
			return &Failure{Code: "rate_limited", Retryable: true, RetryAfter: time.Minute}
		}
		return nil
	}), 9, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if ok, err := d.deliverOne(ctx, time.Now().Add(time.Second)); err != nil || !ok {
		t.Fatalf("首次处理: %v %v", ok, err)
	}
	if ok, err := d.deliverOne(ctx, time.Now().Add(30*time.Second)); err != nil || ok {
		t.Fatalf("重试时间前应等待: %v %v", ok, err)
	}
	if ok, err := d.deliverOne(ctx, time.Now().Add(time.Hour)); err != nil || !ok {
		t.Fatalf("重试: %v %v", ok, err)
	}
	if ok, err := d.deliverOne(ctx, time.Now().Add(2*time.Hour)); err != nil || ok {
		t.Fatalf("成功后应停止投递: %v %v", ok, err)
	}
	items, err := s.Recent(ctx)
	if err != nil || len(items) != 1 || items[0].State != "accepted" || items[0].Attempts != 2 || calls != 2 {
		t.Fatalf("成功记录: %+v %v calls=%d", items, err, calls)
	}
}

func TestDispatcherStopsPermanentAndExhaustedFailures(t *testing.T) {
	for _, retryable := range []bool{false, true} {
		t.Run(map[bool]string{false: "permanent", true: "exhausted"}[retryable], func(t *testing.T) {
			ctx := context.Background()
			s, err := storage.Open(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err := s.Enqueue(ctx, event.Notification{ID: "n"}); err != nil {
				t.Fatal(err)
			}
			calls := 0
			d := New(s, senderFunc(func(context.Context, event.Notification) error {
				calls++
				return &Failure{Code: "failed", Retryable: retryable}
			}), 9, slog.New(slog.NewTextHandler(io.Discard, nil)))
			for i := 0; i < 12; i++ {
				if _, err := d.deliverOne(ctx, time.Now().Add(24*time.Hour)); err != nil {
					t.Fatal(err)
				}
			}
			want := 1
			if retryable {
				want = 10
			}
			items, err := s.Recent(ctx)
			if err != nil || len(items) != 1 || items[0].State != "dead" || calls != want {
				t.Fatalf("终止重试: %+v %v calls=%d", items, err, calls)
			}
		})
	}
}

func TestServerRetryDelaySurvivesRestart(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	s, err := storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	if err := s.Enqueue(ctx, event.Notification{ID: "rate-limited"}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	sender := senderFunc(func(context.Context, event.Notification) error {
		calls++
		if calls == 1 {
			return &Failure{Code: "rate_limited", Retryable: true, RetryAfter: time.Hour}
		}
		return nil
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := New(s, sender, 1, logger)
	now := time.Now()
	if worked, err := d.deliverOne(ctx, now.Add(time.Second)); err != nil || !worked {
		t.Fatalf("initial delivery: worked=%v err=%v", worked, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	d = New(s, sender, 1, logger)
	if worked, err := d.deliverOne(ctx, now.Add(45*time.Minute)); err != nil || worked || calls != 1 {
		t.Fatalf("delivery before server delay: worked=%v calls=%d err=%v", worked, calls, err)
	}
	if worked, err := d.deliverOne(ctx, now.Add(61*time.Minute)); err != nil || !worked || calls != 2 {
		t.Fatalf("delivery after server delay: worked=%v calls=%d err=%v", worked, calls, err)
	}
	items, err := s.Recent(ctx)
	if err != nil || len(items) != 1 || items[0].State != "accepted" || items[0].Attempts != 2 {
		t.Fatalf("persisted delivery: %+v err=%v", items, err)
	}
}

func TestExcessiveRetryAfterEndsTask(t *testing.T) {
	ctx := t.Context()
	s, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Enqueue(ctx, event.Notification{ID: "throttled"}); err != nil {
		t.Fatal(err)
	}
	d := New(s, senderFunc(func(context.Context, event.Notification) error {
		return &Failure{Code: "bark_http_error", Retryable: true, RetryAfter: 48 * time.Hour}
	}), 3, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := d.deliverOne(ctx, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if summary, err := s.Summary(ctx); err != nil || summary.Dead != 1 || summary.Pending != 0 {
		t.Fatalf("a wait beyond the cap must end the task: %+v %v", summary, err)
	}
}

type channelEventSender struct {
	senderFunc
	channel string
}

func (s channelEventSender) Channel() string { return s.channel }

func TestDispatcherKeepsVerificationCodeOnlyForNative(t *testing.T) {
	for _, channel := range []string{"bark", "pushover", "webhook", "native"} {
		for _, preview := range []bool{false, true} {
			t.Run(channel+map[bool]string{false: "/off", true: "/subject"}[preview], func(t *testing.T) {
				store, err := storage.Open(t.Context(), t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				n := event.Notification{ID: "code-event", Code: "482913", Sender: "sender@example.org", Subject: "Sign in"}
				if err := store.Enqueue(t.Context(), n); err != nil {
					t.Fatal(err)
				}
				var sent event.Notification
				sender := channelEventSender{channel: channel, senderFunc: func(_ context.Context, n event.Notification) error { sent = n; return nil }}
				d := New(store, sender, 0, slog.New(slog.NewTextHandler(io.Discard, nil)))
				d.Configure(sender, 0, preview)
				if worked, err := d.deliverOne(t.Context(), time.Now().Add(time.Second)); err != nil || !worked {
					t.Fatalf("worked=%v err=%v", worked, err)
				}
				wantCode, wantSubject, wantSender := "", n.Subject, n.Sender
				if channel == "native" {
					wantCode = n.Code
				} else if !preview {
					wantSubject, wantSender = "", ""
				}
				if sent.Code != wantCode || sent.Subject != wantSubject || sent.Sender != wantSender || sent.ID != n.ID {
					t.Fatalf("unexpected event: %+v", sent)
				}
			})
		}
	}
}

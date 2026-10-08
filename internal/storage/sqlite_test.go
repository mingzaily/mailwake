package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
)

func TestAtomicCommitRecoveryAndDedup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	other, err := Open(ctx, dir)
	if err == nil {
		other.Close()
		t.Fatal("两个进程同时持有数据目录")
	}
	n := event.Notification{ID: event.ID("a", "Clients", "1:2"), Subject: "private"}
	if err := s.Commit(ctx, "a", "Clients", "", "baseline", nil); err != nil {
		t.Fatal(err)
	}
	_, err = s.db.ExecContext(ctx, "CREATE TRIGGER fail_progress BEFORE UPDATE ON cursors BEGIN SELECT RAISE(ABORT, 'injected'); END")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(ctx, "a", "Clients", "baseline", "next", []event.Notification{n}); err == nil {
		t.Fatal("应触发事务失败")
	}
	summary, err := s.Summary(ctx)
	if err != nil || summary.Pending != 0 {
		t.Fatalf("回滚后残留通知: %+v %v", summary, err)
	}
	if cp, err := s.Checkpoint(ctx, "a", "Clients"); err != nil || cp != "baseline" {
		t.Fatalf("回滚后进度: %q %v", cp, err)
	}
	if _, err := s.db.ExecContext(ctx, "DROP TRIGGER fail_progress"); err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(ctx, "a", "Clients", "baseline", "next", []event.Notification{n, n}); err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(ctx, "a", "Clients", "baseline", "stale", nil); !errors.Is(err, ErrCheckpointConflict) {
		t.Fatalf("旧进度应拒绝: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cp, err := s.Checkpoint(ctx, "a", "Clients"); err != nil || cp != "next" {
		t.Fatalf("重启后进度: %q %v", cp, err)
	}
	summary, err = s.Summary(ctx)
	if err != nil || summary.Pending != 1 {
		t.Fatalf("去重失败: %+v %v", summary, err)
	}
	task, err := s.Due(ctx, time.Now().Add(time.Second))
	if err != nil || task == nil || task.Event.Subject != "private" {
		t.Fatalf("队列未恢复: %+v %v", task, err)
	}
	if err := s.Accepted(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := s.db.QueryRowContext(ctx, "SELECT payload FROM outbox WHERE id=?", n.ID).Scan(&payload); err != nil || payload != "{}" {
		t.Fatalf("成功后应清除内容: %q %v", payload, err)
	}
	if err := s.Prune(ctx, time.Now().Add(time.Hour), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	summary, err = s.Summary(ctx)
	if err != nil || summary.Accepted != 0 {
		t.Fatalf("清理失败: %+v %v", summary, err)
	}
}

func TestPruneKeepsDeadTasksUntilTheirRetentionEnds(t *testing.T) {
	ctx := t.Context()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Enqueue(ctx, event.Notification{ID: "dead", Sender: "private"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Failed(ctx, "dead", fault.New("bark_http_error"), time.Now(), true); err != nil {
		t.Fatal(err)
	}
	if err := s.Prune(ctx, time.Now().Add(time.Hour), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if summary, err := s.Summary(ctx); err != nil || summary.Dead != 1 {
		t.Fatalf("dead task removed before its retention ended: %+v %v", summary, err)
	}
	if err := s.Prune(ctx, time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if summary, err := s.Summary(ctx); err != nil || summary.Dead != 0 {
		t.Fatalf("expired dead task kept: %+v %v", summary, err)
	}
}

func TestRetryOnlyDeadTasks(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Enqueue(ctx, event.Notification{ID: "one"}); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Retry(ctx, "one"); err != nil || ok {
		t.Fatalf("pending 不应重复排队: %v %v", ok, err)
	}
	if err := s.Attempt(ctx, "one"); err != nil {
		t.Fatal(err)
	}
	if err := s.Failed(ctx, "one", &fault.Error{Code: "bark_http_error", Params: map[string]string{"status": "400"}}, time.Now(), true); err != nil {
		t.Fatal(err)
	}
	if task, err := s.Due(ctx, time.Now().Add(time.Hour)); err != nil || task != nil {
		t.Fatalf("dead 应等待处理: %+v %v", task, err)
	}
	if ok, err := s.Retry(ctx, "one"); err != nil || !ok {
		t.Fatalf("重试失败: %v %v", ok, err)
	}
	task, err := s.Due(ctx, time.Now().Add(time.Hour))
	if err != nil || task == nil || task.Attempts != 0 {
		t.Fatalf("重试应重置计数: %+v %v", task, err)
	}
}

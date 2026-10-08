// Package engine coordinates folder sessions and durable notification creation.
package engine

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/i18n"
	"github.com/mingzaily/mailwake/internal/mail"
)

type Store interface {
	// SaveSubscriptions also clears saved progress of newly added folders so they start from a new baseline.
	SaveSubscriptions(context.Context, string, mail.Subscriptions) error
	Checkpoint(context.Context, string, string) (string, error)
	Commit(context.Context, string, string, string, string, []event.Notification) error
}

type FolderStatus struct {
	MailboxID    string       `json:"mailbox_id"`
	MailboxLabel string       `json:"mailbox_label"`
	Folder       string       `json:"folder"`
	State        string       `json:"state"`
	Check        string       `json:"check"`
	NextCheck    *time.Time   `json:"next_check,omitempty"`
	Mode         string       `json:"mode,omitempty"`
	LastCheck    *time.Time   `json:"last_check,omitempty"`
	LastError    *fault.Error `json:"last_error,omitempty"`
	Notice       string       `json:"notice,omitempty"`
}

type Engine struct {
	source          mail.Source
	store           Store
	account         string
	label           string
	subscriptions   mail.Subscriptions
	changed         chan struct{}
	interval        time.Duration
	preview         atomic.Bool
	detectCodes     atomic.Bool
	eventNamespace  string
	connectionLimit int
	log             *slog.Logger
	authRetry       time.Duration
	staleAfter      time.Duration
	lifetime        time.Duration
	settingsMu      sync.Mutex
	mu              sync.RWMutex
	status          map[string]FolderStatus
}

// authRetryDelay spaces out logins after the provider rejects the credentials,
// A configuration update replaces the source and starts fresh watchers.
const authRetryDelay = 30 * time.Minute

// staleMessageAge skips mail whose server receive time is older than this when it first
// appears in a watched folder: old mail filed by hand, or a backlog after long downtime.
const staleMessageAge = 24 * time.Hour

// sessionLifetime bounds how long one folder session stays open. Reopening re-reads the
// folder's UIDVALIDITY, so a deleted and recreated folder is detected within this time.
const sessionLifetime = 30 * time.Minute

// errSessionExpired ends a healthy session so the watcher reopens it without backoff.
var errSessionExpired = errors.New("session lifetime reached")

func New(source mail.Source, store Store, account string, subscriptions mail.Subscriptions, interval time.Duration, preview bool, log *slog.Logger) *Engine {
	e := &Engine{source: source, store: store, account: account, label: account, subscriptions: mail.Subscriptions{Revision: subscriptions.Revision, Folders: append([]mail.Folder{}, subscriptions.Folders...)}, changed: make(chan struct{}, 1), interval: interval, log: log.With("mailbox_id", account), authRetry: authRetryDelay, staleAfter: staleMessageAge, lifetime: sessionLifetime, status: make(map[string]FolderStatus)}
	e.connectionLimit = mail.DefaultConnectionLimit
	e.preview.Store(preview)
	e.eventNamespace = account
	for _, f := range subscriptions.Folders {
		e.status[f.Name] = FolderStatus{Folder: f.Name, State: "connecting"}
	}
	return e
}

func (e *Engine) Status() []FolderStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()
	items := make([]FolderStatus, 0, len(e.subscriptions.Folders))
	for _, f := range e.subscriptions.Folders {
		status, ok := e.status[f.Name]
		if !ok {
			status = FolderStatus{Folder: f.Name, State: "connecting"}
		}
		status.MailboxID, status.MailboxLabel, status.Check = e.account, e.label, f.Check
		if f.Check == mail.Realtime {
			status.NextCheck = nil
		}
		items = append(items, status)
	}
	return items
}

func (e *Engine) update(folder string, fn func(*FolderStatus)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.status[folder]
	fn(&s)
	e.status[folder] = s
}

// Run owns worker lifetimes. Removed workers finish before replacements start.
func (e *Engine) Run(ctx context.Context) {
	type worker struct {
		cancel context.CancelFunc
		done   chan struct{}
	}
	workers := make(map[string]worker)
	scheduleCtx, cancelSchedule := context.WithCancel(ctx)
	plans := make(chan scheduledPlan)
	scheduledDone := make(chan struct{})
	go func() { defer close(scheduledDone); e.schedule(scheduleCtx, plans) }()
	defer func() {
		cancelSchedule()
		for _, w := range workers {
			w.cancel()
		}
		for _, w := range workers {
			<-w.done
		}
		<-scheduledDone
	}()
	for ctx.Err() == nil {
		desired := e.Subscriptions()
		realtime := map[string]bool{}
		scheduled := []mail.Folder{}
		for _, f := range desired.Folders {
			if f.Check == mail.Realtime {
				realtime[f.Name] = true
			} else {
				scheduled = append(scheduled, f)
			}
		}
		for name, w := range workers {
			if !realtime[name] {
				w.cancel()
			}
		}
		for name, w := range workers {
			if !realtime[name] {
				<-w.done
				delete(workers, name)
			}
		}
		plan := scheduledPlan{folders: scheduled, ack: make(chan struct{})}
		select {
		case plans <- plan:
		case <-ctx.Done():
			return
		}
		select {
		case <-plan.ack:
		case <-ctx.Done():
			return
		}
		desiredNames := make(map[string]bool, len(desired.Folders))
		for _, f := range desired.Folders {
			desiredNames[f.Name] = true
		}
		e.mu.Lock()
		for name := range e.status {
			if !desiredNames[name] {
				delete(e.status, name)
			}
		}
		e.mu.Unlock()
		for name := range realtime {
			if _, exists := workers[name]; exists {
				continue
			}
			runCtx, cancel := context.WithCancel(ctx)
			done := make(chan struct{})
			workers[name] = worker{cancel, done}
			e.update(name, func(s *FolderStatus) { *s = FolderStatus{Folder: name, Check: mail.Realtime, State: "connecting"} })
			go func() { defer close(done); e.watch(runCtx, name) }()
		}
		select {
		case <-ctx.Done():
			return
		case <-e.changed:
		}
	}
}

func (e *Engine) Subscriptions() mail.Subscriptions {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return mail.Subscriptions{Revision: e.subscriptions.Revision, Folders: append([]mail.Folder{}, e.subscriptions.Folders...)}
}

// UpdateSubscriptions persists desired state before notifying the worker supervisor.
func (e *Engine) UpdateSubscriptions(ctx context.Context, requested mail.Subscriptions) (mail.Subscriptions, error) {
	folders, err := mail.NormalizeSubscriptions(requested.Folders)
	if err != nil {
		return mail.Subscriptions{}, err
	}
	e.settingsMu.Lock()
	defer e.settingsMu.Unlock()
	if err := mail.CheckConnectionBudget(folders, e.connectionLimit); err != nil {
		return mail.Subscriptions{}, err
	}
	if requested.Revision != e.Subscriptions().Revision {
		return mail.Subscriptions{}, fault.New("subscriptions_conflict")
	}
	next := mail.Subscriptions{Revision: requested.Revision + 1, Folders: folders}
	if err := e.store.SaveSubscriptions(ctx, e.account, next); err != nil {
		return mail.Subscriptions{}, fault.From(err, "subscriptions_storage_failed")
	}
	e.mu.Lock()
	e.subscriptions = next
	e.mu.Unlock()
	select {
	case e.changed <- struct{}{}:
	default:
	}
	return mail.Subscriptions{Revision: next.Revision, Folders: append([]mail.Folder{}, folders...)}, nil
}

func (e *Engine) watch(ctx context.Context, folder string) {
	backoff := time.Second
	defer e.update(folder, func(s *FolderStatus) { s.State = "stopped" })
	for ctx.Err() == nil {
		session, err := e.source.Open(ctx, folder)
		if err == nil {
			e.log.Info("Folder connection established", "folder", folder, "mode", session.Mode(), "check", mail.Realtime)
			err = e.consume(ctx, folder, session, func() { backoff = time.Second })
			session.Close()
		}
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errSessionExpired) {
			backoff = time.Second
			continue
		}
		// Adapter errors are deliberately safe to expose; database errors stay internal.
		failure := fault.From(err, "request_failed")
		state, logKey, delay := "reconnecting", "log.folder_reconnecting", backoff+time.Duration(rand.Int64N(int64(backoff/4)+1))
		if failure.Code == mail.CodeAuthFailed {
			state, logKey, delay = "auth_required", "log.folder_auth_required", e.authRetry
		} else {
			backoff = min(backoff*2, time.Minute)
		}
		e.update(folder, func(s *FolderStatus) { s.State = state; s.LastError = failure })
		e.logFailure(folder, mail.Realtime, i18n.Message("en", logKey, nil), failure.Code, delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (e *Engine) consume(ctx context.Context, folder string, session mail.Session, healthy func()) error {
	started := time.Now()
	waited := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		batch, err := e.sync(ctx, folder, session)
		if err != nil {
			return err
		}
		// Reset only after a minute of operation and a successful wait/reconcile cycle.
		if waited && time.Since(started) >= time.Minute {
			healthy()
		}
		now := time.Now().UTC()
		e.update(folder, func(s *FolderStatus) {
			s.State, s.Mode, s.LastError, s.LastCheck = "watching", session.Mode(), nil, &now
			if batch.Reset {
				s.Notice = "notice_baseline_reset"
			}
		})
		if batch.More {
			continue
		}
		if err := session.Wait(ctx, e.interval); err != nil {
			return err
		}
		waited = true
		if time.Since(started) >= e.lifetime {
			return errSessionExpired
		}
	}
}

func (e *Engine) sync(ctx context.Context, folder string, session mail.Session) (mail.Batch, error) {
	previous, err := e.store.Checkpoint(ctx, e.account, folder)
	if err != nil {
		return mail.Batch{}, errCheckpoints
	}
	batch, err := session.Poll(ctx, previous, e.detectCodes.Load())
	if err != nil {
		return mail.Batch{}, err
	}
	if batch.Checkpoint == previous && len(batch.Messages) == 0 {
		return batch, nil
	}
	e.mu.RLock()
	label := e.label
	e.mu.RUnlock()
	items := make([]event.Notification, 0, len(batch.Messages))
	now := time.Now()
	for _, msg := range batch.Messages {
		if !msg.ReceivedAt.IsZero() && now.Sub(msg.ReceivedAt) > e.staleAfter {
			continue
		}
		messageID := event.ID(e.eventNamespace, folder, msg.Key)
		n := event.Notification{Location: msg.Location, ID: messageID, Account: label, AccountID: e.eventNamespace, MailboxID: e.account, Folder: folder, ReceivedAt: msg.ReceivedAt}
		if e.preview.Load() {
			n.Sender, n.Subject, n.Code = msg.Sender, msg.Subject, msg.Code
		}
		items = append(items, n)
	}
	if err := e.store.Commit(ctx, e.account, folder, previous, batch.Checkpoint, items); err != nil {
		return mail.Batch{}, errCheckpoints
	}
	if previous == "" || batch.Reset {
		e.log.Info("Folder baseline rebuilt", "folder", folder)
	}
	if len(items) > 0 {
		e.log.Info("New mail enqueued", "folder", folder, "count", len(items))
	}
	if skipped := len(batch.Messages) - len(items); skipped > 0 {
		e.log.Info(i18n.Message("en", "log.stale_skipped", nil), "folder", folder, "count", skipped)
	}
	return batch, nil
}

var errCheckpoints = fault.New("checkpoint_storage_failed")

// SetEventNamespace is called before Run to separate events from different mailboxes.
func (e *Engine) SetEventNamespace(identity string) { e.eventNamespace = identity }

func (e *Engine) SetPreview(enabled bool) { e.preview.Store(enabled) }

func (e *Engine) SetDetectCodes(enabled bool) { e.detectCodes.Store(enabled) }

// SetConnectionLimit is called before Run; settings replaces the engine when it changes.
func (e *Engine) SetConnectionLimit(limit int) { e.connectionLimit = limit }

// SetMailboxLabel changes the label snapshot used by future batches.
func (e *Engine) SetMailboxLabel(label string) { e.mu.Lock(); defer e.mu.Unlock(); e.label = label }

func (e *Engine) logFailure(folder, check, message, code string, delay time.Duration) {
	level := slog.LevelWarn
	if code == "checkpoint_storage_failed" {
		level = slog.LevelError
	}
	e.log.Log(context.Background(), level, message, "folder", folder, "check", check, "code", code, "backoff_seconds", delay.Seconds())
}

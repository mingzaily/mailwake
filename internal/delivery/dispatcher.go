package delivery

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/i18n"
	"github.com/mingzaily/mailwake/internal/storage"
)

type Sender interface {
	Send(context.Context, event.Notification) error
}

const HeaderEvent = "X-Mailwake-Event"

// Failure carries a safe diagnostic code; provider response bodies and keys stay private.
type Failure struct {
	HTTPStatus int
	Code       string
	Params     map[string]string
	Retryable  bool
	RetryAfter time.Duration
}

func (f *Failure) Error() string { return f.Code }

// Queue is the durable outbox the dispatcher drains.
type Queue interface {
	Due(context.Context, time.Time) (*storage.Task, error)
	Attempt(context.Context, string) error
	Accepted(context.Context, string) error
	Failed(context.Context, string, *fault.Error, time.Time, bool) error
	Prune(context.Context, time.Time, time.Time) error
}

type Dispatcher struct {
	mu           sync.RWMutex
	previewOff   bool
	store        Queue
	sender       Sender
	nativeSender Sender
	log          *slog.Logger
	maxAttempts  int
}

const (
	acceptedRetention = 7 * 24 * time.Hour
	deadRetention     = 30 * 24 * time.Hour
	// maxRetryAfter caps a provider-requested wait; a longer request ends the task
	// instead of leaving it pending indefinitely.
	maxRetryAfter = 24 * time.Hour
)

func New(store Queue, sender Sender, retryCount int, log *slog.Logger) *Dispatcher {
	return &Dispatcher{store: store, sender: sender, log: log.With("mailbox_id", ""), maxAttempts: 1 + retryCount}
}

func (d *Dispatcher) Run(ctx context.Context) {
	nextPrune := time.Now()
	for ctx.Err() == nil {
		now := time.Now()
		if !now.Before(nextPrune) {
			if err := d.store.Prune(ctx, now.Add(-acceptedRetention), now.Add(-deadRetention)); err != nil && ctx.Err() == nil {
				d.log.Error(i18n.Message("en", "log.prune_failed", nil))
			}
			nextPrune = now.Add(time.Hour)
		}
		worked, err := d.deliverOne(ctx, now)
		if err != nil && ctx.Err() == nil {
			d.log.Error(i18n.Message("en", "log.queue_failed", nil))
		}
		if worked && err == nil {
			continue
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (d *Dispatcher) deliverOne(ctx context.Context, now time.Time) (bool, error) {
	d.mu.RLock()
	sender, maxAttempts, previewOff := d.sender, d.maxAttempts, d.previewOff
	nativeSender := d.nativeSender
	d.mu.RUnlock()
	if sender == nil {
		return false, nil
	}
	task, err := d.store.Due(ctx, now)
	if err != nil || task == nil {
		return false, err
	}
	if task.Channel == "native" {
		sender = nativeSender
		previewOff = false
		if sender == nil {
			return false, nil
		}
	}
	channel := "unknown"
	if named, ok := sender.(interface{ Channel() string }); ok {
		channel = named.Channel()
	}
	log := d.log.With("mailbox_id", task.Event.MailboxID, "folder", task.Event.Folder, "event_id", task.Event.ID, "channel", channel)
	if resumable, ok := sender.(interface {
		AlreadyAccepted(context.Context, string) (bool, error)
	}); ok {
		accepted, err := resumable.AlreadyAccepted(ctx, task.Event.ID)
		if err != nil {
			return true, err
		}
		if accepted {
			err = d.store.Accepted(ctx, task.Event.ID)
			if err == nil {
				log.Info("Delivery accepted")
			}
			return true, err
		}
	}
	if task.Attempts >= maxAttempts {
		err := d.store.Failed(ctx, task.Event.ID, fault.New("attempts_exhausted"), now, true)
		if err == nil {
			log.Warn("Delivery abandoned", "code", "attempts_exhausted", "attempt", task.Attempts)
		}
		return true, err
	}
	if err := d.store.Attempt(ctx, task.Event.ID); err != nil {
		return true, err
	}
	sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	if previewOff && channel != "native" {
		task.Event.Sender = ""
		task.Event.Subject = ""
	}
	if channel != "native" {
		task.Event.Code = ""
		task.Event.Location = nil
	}
	started := time.Now()
	err = sender.Send(sendCtx, task.Event)
	log = log.With("attempt", task.Attempts+1, "duration_ms", time.Since(started).Milliseconds())
	cancel()
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	if err == nil {
		err = d.store.Accepted(ctx, task.Event.ID)
		if err == nil {
			log.Info("Delivery accepted")
		}
		return true, err
	}
	failure := &Failure{Code: "delivery_failed", Retryable: true}
	var classified *Failure
	if errors.As(err, &classified) {
		failure = classified
	}
	delay := min(5*time.Second*time.Duration(1<<task.Attempts), 30*time.Minute)
	delay += time.Duration(rand.Int64N(int64(delay/4) + 1))
	delay = max(min(delay, 30*time.Minute), min(failure.RetryAfter, maxRetryAfter))
	dead := !failure.Retryable || task.Attempts+1 >= maxAttempts || failure.RetryAfter > maxRetryAfter
	next := time.Now().Add(delay)
	err = d.store.Failed(ctx, task.Event.ID, &fault.Error{Code: failure.Code, Params: failure.Params}, next, dead)
	if err == nil {
		log = log.With("code", failure.Code, "http_status", failure.HTTPStatus)
		if dead {
			log.Warn("Delivery abandoned")
		} else {
			log.Warn("Delivery failed; retry scheduled", "next_attempt", next.UTC())
		}
	}
	return true, err
}

// Configure atomically replaces the policy used by subsequent delivery attempts.
func (d *Dispatcher) Configure(sender Sender, retryCount int, preview bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sender = sender
	if named, ok := sender.(interface{ Channel() string }); ok && named.Channel() == "native" {
		d.nativeSender = sender
	}
	d.maxAttempts = 1 + retryCount
	d.previewOff = !preview
}

func (d *Dispatcher) SetNativeSender(sender Sender) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.nativeSender = sender
}

package engine

import (
	"context"
	"errors"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"math/rand/v2"
	"sort"
	"time"
)

type scheduledPlan struct {
	folders []mail.Folder
	ack     chan struct{}
}
type scheduler struct {
	engine    *Engine
	plans     <-chan scheduledPlan
	folders   []mail.Folder
	next      map[string]time.Time
	backoff   time.Duration
	authUntil time.Time
}

func (e *Engine) schedule(ctx context.Context, plans <-chan scheduledPlan) {
	s := scheduler{engine: e, plans: plans, next: map[string]time.Time{}, backoff: time.Second}
	for ctx.Err() == nil {
		select {
		case plan := <-plans:
			s.apply(plan)
		default:
		}
		due, wait := s.due(time.Now())
		if len(due) > 0 {
			if plan := s.round(ctx, due); plan != nil {
				s.apply(*plan)
			}
			continue
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case plan := <-plans:
			timer.Stop()
			s.apply(plan)
		case <-timer.C:
		}
	}
}
func (s *scheduler) apply(plan scheduledPlan) {
	s.folders = plan.folders
	s.next = make(map[string]time.Time, len(plan.folders))
	now := time.Now().UTC()
	for _, f := range plan.folders {
		next := now
		if next.Before(s.authUntil) {
			next = s.authUntil
		}
		s.next[f.Name] = next
		s.engine.update(f.Name, func(status *FolderStatus) {
			status.Folder = f.Name
			status.Check = f.Check
			status.Mode = "scheduled"
			status.NextCheck = &next
			if status.State == "" || status.State == "stopped" {
				status.State = "connecting"
			}
		})
	}
	close(plan.ack)
}
func (s *scheduler) due(now time.Time) ([]mail.Folder, time.Duration) {
	sorted := append([]mail.Folder{}, s.folders...)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := s.next[sorted[i].Name], s.next[sorted[j].Name]
		if a.Equal(b) {
			return sorted[i].Name < sorted[j].Name
		}
		return a.Before(b)
	})
	due := []mail.Folder{}
	wait := 24 * time.Hour
	for _, f := range sorted {
		at := s.next[f.Name]
		if !at.After(now) {
			due = append(due, f)
		} else {
			wait = min(wait, at.Sub(now))
		}
	}
	return due, wait
}
func (s *scheduler) failed(folders []mail.Folder, err error) {
	failure := fault.From(err, "request_failed")
	delay := s.backoff + time.Duration(rand.Int64N(int64(s.backoff/4)+1))
	state := "reconnecting"
	if failure.Code == mail.CodeAuthFailed {
		delay = s.engine.authRetry
		state = "auth_required"
		s.authUntil = time.Now().Add(delay)
	} else {
		s.backoff = min(s.backoff*2, time.Minute)
	}
	next := time.Now().Add(delay).UTC()
	if state == "auth_required" {
		folders = s.folders
	}
	for _, f := range folders {
		message := "Scheduled connection interrupted; reconnecting"
		if state == "auth_required" {
			message = "Mailbox authentication failed; checks paused"
		}
		s.engine.logFailure(f.Name, f.Check, message, failure.Code, delay)
		s.next[f.Name] = next
		s.engine.update(f.Name, func(status *FolderStatus) {
			status.State = state
			status.LastError = failure
			status.NextCheck = &next
			status.Mode = "scheduled"
		})
	}
}
func (s *scheduler) round(ctx context.Context, due []mail.Folder) *scheduledPlan {
	provider, ok := s.engine.source.(mail.ScheduledSource)
	if !ok {
		s.failed(due, fault.New("scheduled_check_unavailable"))
		return nil
	}
	conn, err := provider.OpenScheduled(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.failed(due, err)
		}
		return nil
	}
	defer conn.Close()
	successful := true
	for i, f := range due {
		select {
		case plan := <-s.plans:
			return &plan
		default:
		}
		session, err := conn.Select(ctx, f.Name)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var folderError *mail.FolderError
			if errors.As(err, &folderError) {
				s.failed([]mail.Folder{f}, err)
				successful = false
				continue
			}
			s.failed(due[i:], err)
			return nil
		}
		s.engine.log.Info("Folder connection established", "folder", f.Name, "mode", "scheduled", "check", f.Check)
		for {
			batch, err := s.engine.sync(ctx, f.Name, session)
			if err != nil {
				if ctx.Err() == nil {
					s.failed(due[i:], err)
				}
				return nil
			}
			now := time.Now().UTC()
			next := now.Add(f.Interval())
			s.engine.update(f.Name, func(status *FolderStatus) {
				status.State = "watching"
				status.Mode = "scheduled"
				status.Check = f.Check
				status.LastCheck = &now
				status.LastError = nil
				status.NextCheck = &next
				if batch.Reset {
					status.Notice = "notice_baseline_reset"
				}
			})
			s.next[f.Name] = next
			select {
			case plan := <-s.plans:
				return &plan
			default:
			}
			if !batch.More {
				break
			}
		}
	}
	if successful {
		s.backoff = time.Second
	}
	return nil
}

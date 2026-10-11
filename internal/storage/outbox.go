package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
)

type Task struct {
	Channel  string
	Event    event.Notification
	Attempts int
}

// Due is consumed by one dispatcher. Store.Open enforces one process per data directory.
func (s *Store) Due(ctx context.Context, now time.Time) (*Task, error) {
	var payload string
	task := &Task{}
	err := s.db.QueryRowContext(ctx, "SELECT payload,attempts,channel FROM outbox WHERE state='pending' AND next_attempt<=? ORDER BY next_attempt,created_at,id LIMIT 1", now.UnixMilli()).Scan(&payload, &task.Attempts, &task.Channel)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(payload), &task.Event); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *Store) Attempt(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE outbox SET attempts=attempts+1 WHERE id=? AND state='pending'", id)
	return err
}

func (s *Store) Accepted(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE outbox SET state='accepted',accepted_at=?,last_error='',payload='{}' WHERE id=?", time.Now().UnixMilli(), id)
	return err
}

func (s *Store) Failed(ctx context.Context, id string, failure *fault.Error, next time.Time, dead bool) error {
	state := "pending"
	if dead {
		state = "dead"
	}
	data, err := json.Marshal(failure)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "UPDATE outbox SET state=?,last_error=?,next_attempt=?,payload=CASE WHEN ? AND channel='native' THEN '{}' ELSE payload END WHERE id=?", state, string(data), next.UnixMilli(), dead, id)
	if err != nil {
		return err
	}
	if dead {
		if _, err = tx.ExecContext(ctx, "UPDATE native_deliveries SET envelope='',content_signature='',state=CASE WHEN relay_id='' AND state='pending' THEN 'failed' ELSE state END,error_code=CASE WHEN relay_id='' AND state='pending' THEN ? ELSE error_code END WHERE event_id=?", failure.Code, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Retry(ctx context.Context, id string) (bool, error) {
	r, err := s.db.ExecContext(ctx, "UPDATE outbox SET state='pending',attempts=0,next_attempt=?,last_error='' WHERE id=? AND state='dead' AND channel<>'native'", time.Now().UnixMilli(), id)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}

// DeliveryMessage is the administrator's history summary, retained with its outbox row.
// Subject is nil when preview was off at enqueue time, and empty for mail without a subject.
type DeliveryMessage struct {
	MailboxID    string  `json:"mailbox_id"`
	MailboxLabel string  `json:"mailbox_label"`
	Folder       string  `json:"folder"`
	Subject      *string `json:"subject"`
	Test         bool    `json:"test"`
}

type DeliveryStatus struct {
	Message    *DeliveryMessage `json:"message,omitempty"`
	Channel    string           `json:"channel,omitempty"`
	ID         string           `json:"id"`
	State      string           `json:"state"`
	Attempts   int              `json:"attempts"`
	LastError  *fault.Error     `json:"last_error,omitempty"`
	CreatedAt  int64            `json:"created_at"`
	AcceptedAt *int64           `json:"accepted_at,omitempty"`
}

type Summary struct {
	Pending  int `json:"pending"`
	Accepted int `json:"accepted"`
	Dead     int `json:"dead"`
}

func (s *Store) Summary(ctx context.Context) (Summary, error) {
	var r Summary
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FILTER(WHERE state='pending'),count(*) FILTER(WHERE state='accepted'),count(*) FILTER(WHERE state='dead') FROM outbox").Scan(&r.Pending, &r.Accepted, &r.Dead)
	return r, err
}

func (s *Store) Recent(ctx context.Context) ([]DeliveryStatus, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,state,attempts,last_error,created_at,accepted_at,channel,message FROM outbox ORDER BY created_at DESC,id LIMIT 50")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []DeliveryStatus{}
	for rows.Next() {
		var d DeliveryStatus
		var rawError string
		var message sql.NullString
		if err := rows.Scan(&d.ID, &d.State, &d.Attempts, &rawError, &d.CreatedAt, &d.AcceptedAt, &d.Channel, &message); err != nil {
			return nil, err
		}
		if message.Valid {
			if err := json.Unmarshal([]byte(message.String), &d.Message); err != nil {
				return nil, err
			}
		}
		if rawError != "" {
			if err := json.Unmarshal([]byte(rawError), &d.LastError); err != nil {
				return nil, err
			}
		}
		items = append(items, d)
	}
	return items, rows.Err()
}

// Prune deletes accepted records accepted before acceptedBefore and dead records created
// before deadBefore, in bounded batches. Dead records keep their payload for manual retry
// until then.
func (s *Store) Prune(ctx context.Context, acceptedBefore, deadBefore time.Time) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM outbox WHERE id IN (SELECT id FROM outbox WHERE state='accepted' AND accepted_at<? LIMIT 1000)", acceptedBefore.UnixMilli()); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM outbox WHERE id IN (SELECT id FROM outbox WHERE state='dead' AND created_at<? LIMIT 1000)", deadBefore.UnixMilli())
	return err
}

// ClearDeliveryHistory removes finished records across the full history. Active
// dispatch, Relay status checks and pending activity updates retain their rows.
func (s *Store) ClearDeliveryHistory(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM outbox
 WHERE state IN ('accepted','dead')
 AND NOT EXISTS (SELECT 1 FROM native_deliveries WHERE event_id=outbox.id
   AND (next_check>0 OR state IN ('pending','queued','sending')))
 AND NOT EXISTS (SELECT 1 FROM native_activities WHERE event_id=outbox.id AND state='pending')`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

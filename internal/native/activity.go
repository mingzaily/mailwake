package native

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mingzaily/mailwake/internal/coreidentity"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/storage"
)

type activityRequest struct {
	UpdatedAt  int64    `json:"updated_at"`
	PairingID  string   `json:"pairing_id"`
	ActivityID string   `json:"activity_id"`
	EventID    string   `json:"event_id"`
	Event      string   `json:"event"`
	ReceivedAt int64    `json:"received_at"`
	ExpiresAt  int64    `json:"expires_at"`
	Envelope   Envelope `json:"envelope"`
	Signature  string   `json:"content_signature"`
}

type activityContent struct {
	ActivityID   string    `json:"activity_id"`
	EventID      string    `json:"event_id"`
	Event        string    `json:"event"`
	ReceivedAt   int64     `json:"received_at"`
	ExpiresAt    int64     `json:"expires_at"`
	UpdatedAt    int64     `json:"updated_at"`
	Notification plaintext `json:"notification"`
}

func (s *Service) planActivities(n event.Notification, devices []storage.NativePairing, identity *coreidentity.Identity, audience string) ([]storage.NativeActivity, error) {
	expiresAt := n.ReceivedAt.Unix() + 600
	if n.Test || n.Code == "" || expiresAt <= time.Now().Unix() {
		return nil, nil
	}
	reference, err := identity.MessageReference(n)
	if err != nil {
		return nil, err
	}
	content := activityContent{ActivityID: n.ID, EventID: n.ID, Event: "start", ReceivedAt: n.ReceivedAt.Unix(), ExpiresAt: expiresAt, UpdatedAt: n.ReceivedAt.Unix(),
		Notification: plaintext{V: 1, MailboxID: n.MailboxID, Account: n.Account, Folder: n.Folder, Sender: n.Sender, Subject: n.Subject, Code: n.Code, ReceivedAt: n.ReceivedAt.UTC().Format(time.RFC3339Nano), MessageRef: reference}}
	plain, err := fitPlaintext(&content.Notification, func() ([]byte, error) { return json.Marshal(content) })
	if err != nil {
		return nil, err
	}
	var items []storage.NativeActivity
	for _, device := range devices {
		request := activityRequest{PairingID: device.ID, ActivityID: n.ID, EventID: n.ID, Event: "start", ReceivedAt: content.ReceivedAt, ExpiresAt: expiresAt, UpdatedAt: content.UpdatedAt}
		rawContext, _ := json.Marshal([]any{"mailwake-activity-v1", device.ID, n.ID, "start", n.ID, request.ReceivedAt, request.ExpiresAt, request.UpdatedAt})
		contextID := hash(rawContext)
		request.Envelope, err = encrypt(audience, device.ID, contextID, device.EncryptionKey, plain)
		if err != nil {
			return nil, err
		}
		request.Signature, err = coreidentity.Sign(identity.Key, contentText(audience, device.ID, contextID, request.Envelope))
		if err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(request)
		items = append(items, storage.NativeActivity{EventID: n.ID, PairingID: device.ID, Request: string(raw), ExpiresAt: expiresAt})
	}
	return items, nil
}

// pollActivities is separate from ordinary delivery outcomes and never changes their status.
func (s *Service) pollActivities(ctx context.Context) error {
	return s.pollActivitiesAt(ctx, time.Now)
}

func (s *Service) pollActivitiesAt(ctx context.Context, now func() time.Time) error {
	items, err := s.store.DueNativeActivities(ctx, now())
	if err != nil || len(items) == 0 {
		return err
	}
	eligible := items[:0]
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if item.ExpiresAt <= now().Unix() {
			if err := s.store.FinishNativeActivity(ctx, item, "failed"); err != nil {
				return err
			}
			continue
		}
		eligible = append(eligible, item)
	}
	if len(eligible) == 0 {
		return nil
	}
	info, err := s.ensure(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !nativeFailure(err).Retryable {
			return err
		}
		for _, item := range eligible {
			if err := s.recordActivityResult(ctx, item, err, now()); err != nil {
				return err
			}
		}
		return nil
	}
	identity, err := s.identity.Load(ctx)
	if err != nil {
		return err
	}
	for _, item := range eligible {
		if err := ctx.Err(); err != nil {
			return err
		}
		pairing, err := s.store.NativePairing(ctx, item.PairingID)
		if err != nil && safeCode(err) != "native_pairing_not_found" {
			return err
		}
		// Earlier requests can consume the lifetime or revoke this pairing.
		if item.ExpiresAt <= now().Unix() || pairing.State != "active" {
			if err := s.store.FinishNativeActivity(ctx, item, "failed"); err != nil {
				return err
			}
			continue
		}
		var result struct {
			ID string `json:"id"`
		}
		err = s.client.call(ctx, identity, info.Audience, "POST", "/v1/activities", json.RawMessage(item.Request), &result, 202)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil && nativeFailure(err).HTTPStatus == 410 && pairingGone(nativeFailure(err)) {
			if err := s.revoke(ctx, item.PairingID); err != nil {
				return err
			}
		}
		if err := s.recordActivityResult(ctx, item, err, now()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) recordActivityResult(ctx context.Context, item storage.NativeActivity, err error, now time.Time) error {
	if err == nil {
		return s.store.FinishNativeActivity(ctx, item, "accepted")
	}
	failure := nativeFailure(err)
	if failure.Retryable {
		delay := max(min(30*time.Second<<min(item.Failures, 4), 5*time.Minute), failure.RetryAfter)
		// Compare the duration before adding it so an excessive Retry-After cannot overflow a timestamp.
		if remaining := time.Unix(item.ExpiresAt, 0).Sub(now); delay < remaining {
			return s.store.DelayNativeActivity(ctx, item, now.Add(delay))
		}
	}
	return s.store.FinishNativeActivity(ctx, item, "failed")
}

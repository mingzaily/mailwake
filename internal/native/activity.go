package native

import (
	"context"
	"encoding/json"
	"time"

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

func (s *Service) planActivities(n event.Notification, devices []storage.NativePairing, identity *Identity, audience string) ([]storage.NativeActivity, error) {
	expiresAt := n.ReceivedAt.Unix() + 600
	if n.Test || n.Code == "" || expiresAt <= time.Now().Unix() {
		return nil, nil
	}
	reference, err := messageReference(identity, n)
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
		request.Signature, err = sign(identity.Key, contentText(audience, device.ID, contextID, request.Envelope))
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
	items, err := s.store.PendingNativeActivities(ctx)
	if err != nil || len(items) == 0 {
		return err
	}
	info, err := s.ensure(ctx)
	if err != nil {
		return err
	}
	identity, err := s.identity.load(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		pairing, err := s.store.NativePairing(ctx, item.PairingID)
		if err != nil {
			return err
		}
		if item.ExpiresAt <= time.Now().Unix() || pairing.State != "active" {
			if err := s.store.FinishNativeActivity(ctx, item, "failed"); err != nil {
				return err
			}
			continue
		}
		var result struct {
			ID string `json:"id"`
		}
		err = s.client.call(ctx, identity, info.Audience, "POST", "/v1/activities", json.RawMessage(item.Request), &result, 202)
		if err != nil && nativeFailure(err).Retryable {
			continue
		}
		state := "accepted"
		if err != nil {
			state = "failed"
		}
		if err := s.store.FinishNativeActivity(ctx, item, state); err != nil {
			return err
		}
	}
	return nil
}

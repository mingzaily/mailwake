package native

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/mingzaily/mailwake/internal/coreidentity"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
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
	if n.CodeExpiresAt != nil {
		expiresAt = min(n.CodeExpiresAt.Unix(), n.ReceivedAt.Unix()+3600)
	}
	if n.Test || n.Code == "" || expiresAt <= time.Now().Unix() {
		return nil, nil
	}
	reference, err := identity.MessageReference(n)
	if err != nil {
		return nil, err
	}
	content := activityContent{ActivityID: n.ID, EventID: n.ID, Event: "start", ReceivedAt: n.ReceivedAt.Unix(), ExpiresAt: expiresAt, UpdatedAt: n.ReceivedAt.Unix(),
		Notification: newPlaintext(n, reference)}
	plain, err := fitPlaintext(&content.Notification, func() ([]byte, error) { return json.Marshal(content) })
	if err != nil {
		return nil, err
	}
	var items []storage.NativeActivity
	for _, device := range devices {
		request := activityRequest{PairingID: device.ID, ActivityID: n.ID, EventID: n.ID, Event: "start", ReceivedAt: content.ReceivedAt, ExpiresAt: expiresAt, UpdatedAt: content.UpdatedAt}
		rawContext, _ := json.Marshal([]any{"mailwake-activity-v1", device.ID, n.ID, "start", n.ID, request.ReceivedAt, request.ExpiresAt, request.UpdatedAt})
		contextID := hash(rawContext)
		key := device.EncryptionKey
		if device.ActivityKey != "" {
			key = device.ActivityKey
		}
		request.Envelope, err = encrypt(audience, device.ID, contextID, key, plain)
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

// Core authenticates the device preference with the signing key saved at pairing.
func validatedActivityPreference(p storage.NativePairing, result relayPairing, audience string) (storage.NativePairing, error) {
	p.ActivityKey = ""
	p.ActivityOnly = false
	pref := result.Activity
	if pref == nil {
		return p, nil
	}
	if pref.Version < 1 || pref.Version < p.ActivityVersion {
		return p, fault.New("native_protocol_invalid")
	}
	if _, err := coreidentity.PublicKey(pref.Key); err != nil {
		return p, err
	}
	enabled, only := "0", "0"
	if pref.Enabled {
		enabled = "1"
	}
	if pref.Only {
		only = "1"
	}
	text := strings.Join([]string{"mailwake-activity-key-v2", audience, p.DeviceID, pref.Key, enabled, only, strconv.FormatInt(pref.Version, 10)}, "\n")
	if err := coreidentity.Verify(p.SigningKey, pref.Signature, text); err != nil {
		return p, err
	}
	p.ActivityVersion = pref.Version
	p.ActivityKey = pref.Key
	p.ActivityOnly = pref.Enabled && pref.Only
	return p, nil
}

// Only explicit APNs acceptance enables quiet or omitted ordinary delivery.
func (s *Service) tryActivityOnly(ctx context.Context, n event.Notification, p storage.NativePairing, activities []storage.NativeActivity, identity *coreidentity.Identity, audience string) (string, error) {
	if !p.ActivityOnly {
		return "", nil
	}
	for _, item := range activities {
		if item.EventID != n.ID || item.PairingID != p.ID {
			continue
		}
		var result struct {
			ID      string `json:"id"`
			Outcome string `json:"apns_status"`
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(item.Request), &body); err != nil {
			return "", err
		}
		body["wait_for_acceptance"] = true
		body["activity_key"] = p.ActivityKey
		err := s.client.call(ctx, identity, audience, "POST", "/v1/activities", body, &result, 202)
		accepted := err == nil && result.ID != "" && result.Outcome == "accepted"
		state := "failed"
		if accepted {
			state = "accepted"
		}
		if err := s.store.FinishNativeActivity(ctx, item, state); err != nil {
			return "", err
		}
		if accepted {
			return result.ID, nil
		}
		return "", nil
	}
	return "", nil
}

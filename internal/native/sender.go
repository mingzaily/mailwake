package native

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/storage"
)

func (s *Service) Channel() string { return "native" }

type plaintext struct {
	MessageRef *MessageReference `json:"message_ref,omitempty"`
	V          int               `json:"v"`
	MailboxID  string            `json:"mailbox_id,omitempty"`
	Account    string            `json:"account"`
	Folder     string            `json:"folder"`
	Sender     string            `json:"sender"`
	Subject    string            `json:"subject"`
	Code       string            `json:"code,omitempty"`
	ReceivedAt string            `json:"received_at"`
}

func notificationPlain(n event.Notification) ([]byte, error) {
	return notificationPlainWithReference(n, nil)
}
func notificationPlainWithReference(n event.Notification, reference *MessageReference) ([]byte, error) {
	p := plaintext{MessageRef: reference, V: 1, MailboxID: n.MailboxID, Account: n.Account, Folder: n.Folder, Sender: n.Sender, Subject: n.Subject, Code: n.Code, ReceivedAt: n.ReceivedAt.UTC().Format(time.RFC3339Nano)}
	return fitPlaintext(&p, func() ([]byte, error) { return json.Marshal(p) })
}

func fitPlaintext(p *plaintext, encode func() ([]byte, error)) ([]byte, error) {
	b, err := encode()
	if err != nil {
		return nil, err
	}
	for _, field := range []*string{&p.Subject, &p.Sender} {
		if len(b) <= 2032 {
			break
		}
		// Binary search over Unicode characters preserves valid UTF-8 and counts JSON escaping.
		runes := []rune(*field)
		lo, hi := 0, len(runes)
		for lo < hi {
			mid := (lo + hi + 1) / 2
			*field = string(runes[:mid])
			candidate, _ := encode()
			if len(candidate) <= 2032 {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		*field = string(runes[:lo])
		b, err = encode()
		if err != nil {
			return nil, err
		}
	}
	if len(b) > 2032 || !utf8.Valid(b) {
		return nil, &delivery.Failure{Code: "native_payload_too_large"}
	}
	return b, nil
}
func nativeFailure(err error) *delivery.Failure {
	var f *delivery.Failure
	if errors.As(err, &f) {
		return f
	}
	var coded *fault.Error
	if errors.As(err, &coded) {
		return &delivery.Failure{Code: coded.Code}
	}
	return &delivery.Failure{Code: "delivery_failed", Retryable: true}
}

// Send is called by the single outbox dispatcher, or directly for a test
// notification with a fresh ID, so one event never has two concurrent senders.
func (s *Service) Send(ctx context.Context, n event.Notification) (result error) {
	own, err := s.store.MarkNativeEvent(ctx, n.ID, n.Test)
	if err != nil {
		return err
	}
	if own {
		defer func() {
			if result == nil {
				result = s.store.Accepted(ctx, n.ID)
			} else {
				if cleanup := s.store.Failed(ctx, n.ID, fault.New(safeCode(result)), time.Now(), true); cleanup != nil {
					result = cleanup
				}
			}
		}()
	}
	if !s.Available() {
		return &delivery.Failure{Code: "native_push_unavailable"}
	}
	items, err := s.store.NativeDeliveries(ctx, n.ID)
	if err != nil {
		return err
	}
	var devices []storage.NativePairing
	if len(items) == 0 {
		devices, err = s.store.NativePairings(ctx, "active")
		if err != nil {
			return err
		}
		if len(devices) == 0 {
			return &delivery.Failure{Code: "native_no_devices"}
		}
	}
	info, err := s.ensure(ctx)
	if err != nil {
		return nativeFailure(err)
	}
	identity, err := s.identity.load(ctx)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		reference, err := messageReference(identity, n)
		if err != nil {
			return err
		}
		plain, err := notificationPlainWithReference(n, reference)
		if err != nil {
			return err
		}
		// Relay fingerprints expires_at, so every retry reuses the planned deadline.
		expiresAt := time.Now().Add(time.Hour).Unix()
		for _, p := range devices {
			envelope, err := encrypt(info.Audience, p.ID, n.ID, p.EncryptionKey, plain)
			if err != nil {
				return nativeFailure(err)
			}
			signature, err := sign(identity.Key, contentText(info.Audience, p.ID, n.ID, envelope))
			if err != nil {
				return err
			}
			raw, _ := json.Marshal(envelope)
			items = append(items, storage.NativeDelivery{EventID: n.ID, PairingID: p.ID, DeviceName: p.DeviceName, Envelope: string(raw), Signature: signature, ExpiresAt: expiresAt, State: "pending"})
		}
		activities, activityErr := s.planActivities(n, devices, identity, info.Audience)
		if activityErr != nil {
			s.log.Warn("Activity planning failed", "code", safeCode(activityErr))
		}
		if err = s.store.SaveNativePlan(ctx, items, activities...); err != nil {
			return err
		}
	}
	var permanent, transient error
	for _, d := range items {
		if d.RelayID != "" {
			continue
		}
		if d.State != "pending" {
			permanent = &delivery.Failure{Code: d.ErrorCode}
			continue
		}
		p, err := s.store.NativePairing(ctx, d.PairingID)
		if err != nil && safeCode(err) != "native_pairing_not_found" {
			return err
		}
		if err == nil && p.State != "active" {
			err = fault.New("pairing_revoked")
		}
		if err == nil {
			var status relayDelivery
			body := map[string]any{"pairing_id": d.PairingID, "event_id": n.ID, "envelope": json.RawMessage(d.Envelope), "content_signature": d.Signature, "max_attempts": 1, "expires_at": d.ExpiresAt}
			err = s.client.call(ctx, identity, info.Audience, "POST", "/v1/push", body, &status, 202)
			if err == nil && (status.ID == "" || !validRelayStatus(status.Status)) {
				err = &delivery.Failure{Code: "native_protocol_invalid", Retryable: true}
			}
			if err == nil {
				d.RelayID = status.ID
				d.State = status.Status
				err = s.store.NativeAccepted(ctx, d)
				if err != nil {
					return err
				}
				s.log.Info("Native delivery accepted", "event_id", n.ID, "pairing_id", d.PairingID, "channel", "native")
				continue
			}
		}
		if ctx.Err() != nil {
			return &delivery.Failure{Code: "native_relay_unavailable", Retryable: true}
		}
		failure := nativeFailure(err)
		if pairingGone(failure) {
			if err = s.revoke(ctx, d.PairingID); err != nil {
				return err
			}
			failure = &delivery.Failure{Code: "pairing_revoked"}
		}
		s.log.Warn("Native delivery failed", "event_id", n.ID, "pairing_id", d.PairingID, "channel", "native", "code", failure.Code)
		if failure.Retryable {
			transient = failure
			continue
		}
		if err = s.store.NativeRejected(ctx, d, failure.Code); err != nil {
			return err
		}
		permanent = failure
	}
	if transient != nil {
		return transient
	}
	// A concurrent revocation can cancel an in-flight submission before its acknowledgement is saved.
	saved, err := s.store.NativeDeliveries(ctx, n.ID)
	if err != nil {
		return err
	}
	for _, d := range saved {
		if d.State == "cancelled" && d.RelayID == "" {
			return &delivery.Failure{Code: d.ErrorCode}
		}
	}
	return permanent
}

// pairingGone reports Relay's answer for a pairing the device already removed.
func pairingGone(f *delivery.Failure) bool {
	return f.Code == "pairing_revoked" || f.Params["relay_code"] == "pairing_revoked" || f.HTTPStatus == 404
}

type relayDelivery struct {
	ID        string `json:"id"`
	PairingID string `json:"pairing_id"`
	EventID   string `json:"event_id"`
	Status    string `json:"status"`
	LastError string `json:"last_error"`
}

func validRelayStatus(s string) bool {
	switch s {
	case "queued", "sending", "accepted", "rejected", "unknown", "cancelled", "expired":
		return true
	}
	return false
}
func (s *Service) PollDeliveries(ctx context.Context) error {
	if !s.deliveryPolling.CompareAndSwap(false, true) {
		return nil
	}
	defer s.deliveryPolling.Store(false)
	defer func() {
		if err := s.pollActivities(ctx); err != nil {
			s.log.Warn("Activity delivery failed", "code", safeCode(err))
		}
	}()
	if !s.Available() {
		return nil
	}
	items, err := s.store.DueNativeChecks(ctx)
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
	for _, d := range items {
		var status relayDelivery
		err = s.client.call(ctx, identity, info.Audience, "GET", "/v1/deliveries/"+d.RelayID, nil, &status, 200)
		state, code := d.State, "native_relay_unavailable"
		if err == nil {
			if !validRelayStatus(status.Status) {
				code = "native_protocol_invalid"
			} else {
				state = status.Status
				code = status.LastError
				if code != "" && !codePattern.MatchString(code) {
					code = "native_protocol_invalid"
				}
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if state == "cancelled" && code == "pairing_revoked" {
			if err = s.revoke(ctx, d.PairingID); err != nil {
				return err
			}
		}
		if err = s.store.NativeChecked(ctx, d, state, code); err != nil {
			return err
		}
		s.log.Info("Native delivery status checked", "event_id", d.EventID, "pairing_id", d.PairingID, "channel", "native", "code", code)
	}
	return nil
}

// AlreadyAccepted reconciles the crash window after every Relay acknowledgement
// was saved but before the parent outbox transition committed.
func (s *Service) AlreadyAccepted(ctx context.Context, id string) (bool, error) {
	items, err := s.store.NativeDeliveries(ctx, id)
	if err != nil {
		return false, err
	}
	if len(items) == 0 {
		return false, nil
	}
	for _, d := range items {
		if d.RelayID == "" {
			return false, nil
		}
	}
	return true, nil
}

func (s *Service) revoke(ctx context.Context, id string) error {
	changed, err := s.store.RevokeNativePairing(ctx, id)
	if err == nil && changed {
		s.log.Info("Native pairing revoked on device", "pairing_id", id, "code", "pairing_revoked")
	}
	return err
}

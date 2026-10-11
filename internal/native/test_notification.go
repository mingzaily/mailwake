package native

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/fault"
)

// Test asks Relay to send its fixed, free test payload. Mail always uses /v1/push.
func (s *Service) Test(ctx context.Context, pairingID string) (result error) {
	defer func() {
		if result == nil {
			return
		}
		failure := nativeFailure(result)
		s.log.Warn("Native test failed", "pairing_id", pairingID, "code", failure.Code, "http_status", failure.HTTPStatus, "relay_code", failure.Params["relay_code"])
		code := failure.Code
		if code == "native_relay_error" {
			switch failure.Params["relay_code"] {
			case "rate_limited", "pairing_revoked", "device_unavailable", "device_revoked", "signature_invalid", "signature_expired", "request_replayed":
				code = failure.Params["relay_code"]
			}
		}
		params := failure.Params
		if failure.RetryAfter > 0 {
			if params == nil {
				params = map[string]string{}
			}
			params["retry_after"] = strconv.FormatInt(int64((failure.RetryAfter+time.Second-1)/time.Second), 10)
		}
		result = &fault.Error{Code: code, Params: params}
	}()
	if pairingID == "" {
		return fault.New("native_target_required")
	}
	pairing, err := s.store.NativePairing(ctx, pairingID)
	if err != nil {
		if safeCode(err) == "native_pairing_not_found" {
			return fault.New("native_target_unavailable")
		}
		return err
	}
	if pairing.State != "active" {
		return fault.New("native_target_unavailable")
	}
	info, err := s.ensure(ctx)
	if err != nil {
		return err
	}
	identity, err := s.identity.load(ctx)
	if err != nil {
		return err
	}
	testID := uuid.NewString()
	var response struct {
		TestID    string `json:"test_id"`
		Status    string `json:"status"`
		ErrorCode string `json:"error_code"`
	}
	err = s.client.call(ctx, identity, info.Audience, "POST", "/v1/pairing-tests", map[string]string{"pairing_id": pairingID, "test_id": testID}, &response, 200)
	if err != nil {
		return err
	}
	if response.TestID != testID {
		return fault.New("native_protocol_invalid")
	}
	code := ""
	switch response.Status {
	case "accepted":
		s.log.Info("Native test accepted", "pairing_id", pairingID, "http_status", 200)
		return nil
	case "rejected":
		code = "native_test_rejected"
	case "unknown":
		code = "native_test_unknown"
	case "sending":
		code = "native_test_sending"
	default:
		return fault.New("native_protocol_invalid")
	}
	if response.ErrorCode != "" {
		if !codePattern.MatchString(response.ErrorCode) {
			return fault.New("native_protocol_invalid")
		}
		switch response.ErrorCode {
		case "apns_device_invalid", "apns_authentication_failed", "apns_configuration_invalid", "apns_rejected", "apns_transport_error", "apns_response_invalid", "apns_rate_limited", "apns_unavailable", "apns_timeout":
			code = response.ErrorCode
		}
	}
	return &delivery.Failure{Code: code, HTTPStatus: 200, Params: map[string]string{"relay_code": response.ErrorCode}}
}

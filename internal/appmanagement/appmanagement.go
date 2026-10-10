// Package appmanagement lets the administrator invite the official App to
// manage this Core, and verifies the Platform qualification for paid actions.
package appmanagement

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/native"
	"github.com/mingzaily/mailwake/internal/storage"
)

const invitationTTL = 300

var (
	encoding  = base64.RawURLEncoding
	allScopes = []string{"channels", "content", "diagnostics", "folders", "mailboxes"}
)

// randomToken returns 32 random bytes as unpadded base64url.
func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand.Read never fails
	return encoding.EncodeToString(b)
}

// Hash returns the lowercase hex SHA-256 of b.
func Hash(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

// Origin normalizes an HTTPS origin without path, query or credentials.
func Origin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fault.New("core_origin_invalid")
	}
	return "https://" + strings.TrimSuffix(strings.ToLower(u.Host), ":443"), nil
}

// Scopes returns the requested scopes sorted, rejecting unknown or repeated ones.
func Scopes(values []string) ([]string, error) {
	scopes := slices.Clone(values)
	slices.Sort(scopes)
	if len(scopes) == 0 || len(slices.Compact(slices.Clone(scopes))) != len(scopes) {
		return nil, fault.New("scope_invalid")
	}
	for _, scope := range scopes {
		if !slices.Contains(allScopes, scope) {
			return nil, fault.New("scope_invalid")
		}
	}
	return scopes, nil
}

type Service struct {
	Store  *storage.Store
	Native *native.Service
	Trust  Trust
}

type InvitationInput struct {
	Management bool     `json:"management"`
	Native     bool     `json:"native"`
	Scopes     []string `json:"scopes"`
	CoreOrigin string   `json:"core_origin"`
}

// Invitation is the administrator's view of a new invitation; only this
// response carries the QR URI.
type Invitation struct {
	ID          string    `json:"invitation_id"`
	URI         string    `json:"uri"`
	Fingerprint string    `json:"fingerprint"`
	PairingID   string    `json:"pairing_id,omitempty"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// Create issues a five-minute invitation. Management and Native pairing can be
// requested together; the App completes them independently.
func (s *Service) Create(ctx context.Context, input InvitationInput) (Invitation, error) {
	if !input.Management && !input.Native {
		return Invitation{}, fault.New("request_invalid")
	}
	identity, err := s.Native.CoreIdentity(ctx)
	if err != nil {
		return Invitation{}, err
	}
	now := time.Now()
	invitation := storage.AppInvitation{ID: uuid.NewString(), Management: input.Management, Scopes: []string{}, ExpiresAt: now.Unix() + invitationTTL, CreatedAt: now.Unix()}
	query := url.Values{"v": {"3"}, "core": {identity.PublicKey}}
	if input.Management {
		origin, err := Origin(input.CoreOrigin)
		if err != nil {
			return Invitation{}, err
		}
		if invitation.Scopes, err = Scopes(input.Scopes); err != nil {
			return Invitation{}, err
		}
		token := randomToken()
		invitation.TokenHash = Hash([]byte(token))
		query.Set("origin", origin)
		query.Set("inv", invitation.ID)
		query.Set("t", token)
		query.Set("scopes", strings.Join(invitation.Scopes, ","))
	}
	if input.Native {
		pairing, err := s.Native.Create(ctx)
		if err != nil {
			return Invitation{}, err
		}
		invitation.PairingID = pairing.ID
		invitation.ExpiresAt = min(invitation.ExpiresAt, pairing.ExpiresAt.Unix())
		query.Set("pair", pairing.URI)
	}
	if err = s.Store.CreateAppInvitation(ctx, invitation); err != nil {
		return Invitation{}, err
	}
	return Invitation{ID: invitation.ID, URI: "mailwake://connect?" + query.Encode(), Fingerprint: identity.Fingerprint(), PairingID: invitation.PairingID, ExpiresAt: time.Unix(invitation.ExpiresAt, 0).UTC()}, nil
}

type AcceptInput struct {
	InvitationID string `json:"invitation_id"`
	Token        string `json:"token"`
	SigningKey   string `json:"signing_key"`
	DeviceName   string `json:"device_name"`
}

type Credential struct {
	ControllerID string   `json:"controller_id"`
	CoreID       string   `json:"core_id"`
	DeviceID     string   `json:"device_id"`
	Scopes       []string `json:"scopes"`
	Credential   string   `json:"credential"`
}

// Accept consumes a management invitation and returns the device's credential.
func (s *Service) Accept(ctx context.Context, input AcceptInput) (Credential, error) {
	deviceID, err := native.KeyID(input.SigningKey)
	if err != nil {
		return Credential{}, fault.New("request_invalid")
	}
	name := strings.TrimSpace(input.DeviceName)
	if name == "" || len([]rune(name)) > 64 {
		return Credential{}, fault.New("request_invalid")
	}
	identity, err := s.Native.CoreIdentity(ctx)
	if err != nil {
		return Credential{}, err
	}
	credential := "mwac_" + randomToken()
	controller, err := s.Store.AcceptAppInvitation(ctx, input.InvitationID, Hash([]byte(input.Token)), storage.AppController{ID: "ctrl_" + uuid.NewString(), DeviceID: deviceID, DeviceName: name}, Hash([]byte(credential)))
	if err != nil {
		return Credential{}, err
	}
	return Credential{ControllerID: controller.ID, CoreID: identity.ID, DeviceID: deviceID, Scopes: controller.Scopes, Credential: credential}, nil
}

// InvitationStatus reports the management acceptance of one administrator invitation.
type InvitationStatus struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	DeviceName string    `json:"device_name"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func (s *Service) InvitationStatus(ctx context.Context, id string) (InvitationStatus, error) {
	invitation, err := s.Store.AppInvitation(ctx, id)
	if err != nil {
		return InvitationStatus{}, err
	}
	status := "waiting"
	switch {
	case invitation.UsedAt > 0:
		status = "active"
	case invitation.CancelledAt > 0:
		status = "revoked"
	case invitation.ExpiresAt <= time.Now().Unix():
		status = "expired"
	}
	return InvitationStatus{ID: invitation.ID, Status: status, DeviceName: invitation.DeviceName, ExpiresAt: time.Unix(invitation.ExpiresAt, 0).UTC()}, nil
}

// Cancel withdraws an invitation and its pending Native pairing.
func (s *Service) Cancel(ctx context.Context, id string) error {
	invitation, err := s.Store.AppInvitation(ctx, id)
	if err != nil {
		return err
	}
	if err = s.Store.CancelAppInvitation(ctx, id); err != nil {
		return err
	}
	if invitation.PairingID != "" {
		return s.Native.Unlink(ctx, invitation.PairingID)
	}
	return nil
}

// Trust is the operator-installed set of Platform qualification keys.
type Trust struct {
	Issuer      string            `json:"issuer"`
	Environment string            `json:"environment"`
	Keys        map[string]string `json:"keys"`
}

type qualification struct {
	Subject     string `json:"sub"`
	Purpose     string `json:"purpose"`
	IssuedAt    int64  `json:"iat"`
	Issuer      string `json:"iss"`
	Audience    string `json:"aud"`
	Environment string `json:"environment"`
	DeviceID    string `json:"device_id"`
	CoreID      string `json:"core_id"`
	ExpiresAt   int64  `json:"exp"`
}

// Verify checks a Platform AppManagement qualification for this Core and device.
func (t Trust) Verify(token, coreID, deviceID string) error {
	return t.VerifyPurpose(token, coreID, deviceID, "app_management")
}

// VerifyPurpose binds a commercial qualification to its intended operation.
func (t Trust) VerifyPurpose(token, coreID, deviceID, purpose string) error {
	if len(t.Keys) == 0 {
		return fault.New(purpose + "_unavailable")
	}
	invalid := fault.New(purpose + "_required")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return invalid
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
	}
	raw, err := encoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(raw, &header) != nil || header.Algorithm != "ES256" {
		return invalid
	}
	key, ok := t.Keys[header.KeyID]
	if !ok || !verifyES256(key, parts[0]+"."+parts[1], parts[2]) {
		return invalid
	}
	var claims qualification
	if raw, err = encoding.DecodeString(parts[1]); err != nil || json.Unmarshal(raw, &claims) != nil {
		return invalid
	}
	// Allow future issuance clock skew while enforcing the entitlement deadline.
	now := time.Now().Unix()
	if claims.Subject == "" || claims.Purpose != purpose || claims.IssuedAt > now+30 || claims.IssuedAt <= 0 || claims.ExpiresAt <= claims.IssuedAt || claims.ExpiresAt-claims.IssuedAt > 300 {
		return invalid
	}
	if claims.Issuer != t.Issuer || claims.Audience != "mailwake-core" || claims.Environment != t.Environment || claims.CoreID != coreID || claims.DeviceID != deviceID || claims.ExpiresAt <= now {
		return invalid
	}
	return nil
}

func verifyES256(spki, input, signature string) bool {
	key, err := native.PublicKey(spki)
	if err != nil {
		return false
	}
	raw, err := encoding.DecodeString(signature)
	if err != nil || len(raw) != 64 {
		return false
	}
	digest := sha256.Sum256([]byte(input))
	return ecdsa.Verify(key, digest[:], new(big.Int).SetBytes(raw[:32]), new(big.Int).SetBytes(raw[32:]))
}

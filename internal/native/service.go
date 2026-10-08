package native

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

type Service struct {
	mu              sync.Mutex
	info            *Info
	pairingMu       sync.Mutex // serializes a waiting pairing's Relay exchange with local unlink
	pairingPolling  atomic.Bool
	deliveryPolling atomic.Bool
	store           *storage.Store
	vault           *settings.Vault
	identity        identityStore
	client          *Client
	log             *slog.Logger
}

func New(store *storage.Store, vault *settings.Vault, relayURL string, log *slog.Logger) (*Service, error) {
	return NewWithOptions(store, vault, relayURL, log, ClientOptions{})
}
func NewWithOptions(store *storage.Store, vault *settings.Vault, relayURL string, log *slog.Logger, options ClientOptions) (*Service, error) {
	s := &Service{store: store, vault: vault, identity: identityStore{store: store, vault: vault}, log: log}
	if relayURL != "" {
		c, err := NewClientWithOptions(relayURL, options)
		if err != nil {
			return nil, err
		}
		s.client = c
	}
	return s, nil
}
func (s *Service) Available() bool { return s != nil && s.client != nil }

type relayBinding struct {
	URL  string
	Info Info
}

func (s *Service) ensure(ctx context.Context) (Info, error) {
	if !s.Available() {
		return Info{}, fault.New("native_push_unavailable")
	}
	s.mu.Lock()
	cached := s.info
	s.mu.Unlock()
	if cached != nil {
		return *cached, nil
	}
	return s.refreshInfo(ctx, false)
}

func loadRelayBinding(ctx context.Context, store *storage.Store, vault *settings.Vault) (*relayBinding, error) {
	encrypted, err := store.Configuration(ctx, "native.relay")
	if err != nil || encrypted == nil {
		return nil, err
	}
	plain, err := vault.Open("native.relay", encrypted)
	if err != nil {
		return nil, err
	}
	var saved relayBinding
	if json.Unmarshal(plain, &saved) != nil {
		return nil, fault.New("configuration_corrupt")
	}
	return &saved, nil
}

func (s *Service) refreshInfo(ctx context.Context, checkClock bool) (Info, error) {
	if !s.Available() {
		return Info{}, fault.New("native_push_unavailable")
	}
	// Identify an address change even while the new endpoint is unavailable.
	saved, err := loadRelayBinding(ctx, s.store, s.vault)
	if err != nil {
		return Info{}, err
	}
	if saved != nil && saved.URL != s.client.url {
		return Info{}, fault.New("native_environment_mismatch")
	}
	// Network work precedes the cache lock. A slow refresh never holds up Send.
	info, err := s.client.info(ctx, checkClock)
	if err != nil {
		return info, err
	}
	if saved != nil && !saved.Info.sameIdentity(info) {
		s.mu.Lock()
		s.info = nil
		s.mu.Unlock()
		return info, fault.New("native_environment_mismatch")
	}
	if saved == nil {
		raw, _ := json.Marshal(relayBinding{s.client.url, info})
		encrypted, err := s.vault.Seal("native.relay", raw)
		if err != nil {
			return info, err
		}
		if err = s.store.InitializeConfiguration(ctx, "native.relay", encrypted); err != nil {
			return info, err
		}
	}
	s.mu.Lock()
	s.info = &info
	s.mu.Unlock()
	return info, nil
}
func (s *Service) StartCheck(ctx context.Context) error {
	_, err := s.refreshInfo(ctx, false)
	return err
}

type PairingView struct {
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	ID          string     `json:"id"`
	Status      string     `json:"status"`
	DeviceID    string     `json:"device_id,omitempty"`
	DeviceName  string     `json:"device_name"`
	ExpiresAt   time.Time  `json:"expires_at"`
	ErrorCode   string     `json:"error_code,omitempty"`
	URI         string     `json:"uri,omitempty"`
	Fingerprint string     `json:"fingerprint,omitempty"`
}

func view(p storage.NativePairing) PairingView {
	result := PairingView{ID: p.ID, Status: p.State, DeviceID: p.DeviceID, DeviceName: p.DeviceName, ExpiresAt: time.Unix(p.ExpiresAt, 0).UTC(), ErrorCode: p.ErrorCode}
	if p.RevokedAt > 0 {
		revoked := time.Unix(p.RevokedAt, 0).UTC()
		result.RevokedAt = &revoked
	}
	return result
}
func (s *Service) Create(ctx context.Context) (PairingView, error) {
	info, err := s.refreshInfo(ctx, true)
	if err != nil {
		return PairingView{}, err
	}
	identity, err := s.identity.load(ctx)
	if err != nil {
		return PairingView{}, err
	}
	token := make([]byte, 32)
	if _, err = rand.Read(token); err != nil {
		return PairingView{}, err
	}
	p := storage.NativePairing{ID: uuid.NewString(), State: "waiting", TokenHash: hash(token), ExpiresAt: time.Now().Unix() + 300, CreatedAt: time.Now().Unix()}
	p.EncryptedToken, err = s.vault.Seal("native.pairing:"+p.ID, token)
	if err != nil {
		return PairingView{}, err
	}
	if err = s.store.CreateNativePairing(ctx, p); err != nil {
		return PairingView{}, err
	}
	query := url.Values{"v": {"1"}, "relay": {s.client.url}, "aud": {info.Audience}, "pid": {p.ID}, "core": {identity.PublicKey}, "t": {encoding.EncodeToString(token)}, "exp": {strconv.FormatInt(p.ExpiresAt, 10)}}
	result := view(p)
	result.URI = "mailwake://pair?" + query.Encode()
	result.Fingerprint = identity.Fingerprint()
	s.log.Info("Native pairing created", "pairing_id", p.ID)
	return result, nil
}

// Get returns a pairing so the Web console can follow its QR code.
func (s *Service) Get(ctx context.Context, id string) (PairingView, error) {
	p, err := s.store.NativePairing(ctx, id)
	return view(p), err
}
func (s *Service) Devices(ctx context.Context) ([]PairingView, error) {
	if !s.Available() {
		return nil, fault.New("native_push_unavailable")
	}
	items, err := s.store.NativeDevices(ctx)
	result := []PairingView{}
	for _, p := range items {
		result = append(result, view(p))
	}
	return result, err
}

// CoreIdentity returns the Core signing identity; it does not require a Relay.
func (s *Service) CoreIdentity(ctx context.Context) (*Identity, error) { return s.identity.load(ctx) }

// Unlink revokes a pairing locally first, then removes it at Relay. A failed
// Relay call stays queued and the poller retries it with backoff.
func (s *Service) Unlink(ctx context.Context, id string) error {
	s.pairingMu.Lock()
	err := s.store.UnlinkNativePairing(ctx, id)
	s.pairingMu.Unlock()
	if err != nil {
		return err
	}
	s.log.Info("Native pairing revoked", "pairing_id", id)
	if s.Available() {
		s.unlinkAtRelay(ctx, storage.NativeUnlink{ID: id})
	}
	return nil
}

// unlinkAtRelay finishes on 2xx, 404 or 410; any other result schedules a retry.
func (s *Service) unlinkAtRelay(ctx context.Context, u storage.NativeUnlink) {
	err := s.deletePairing(ctx, u.ID)
	if err == nil || hasStatus(err, 404, 410) {
		if err = s.store.FinishNativeUnlink(ctx, u.ID); err != nil {
			s.log.Warn("Native unlink save failed", "pairing_id", u.ID, "code", safeCode(err))
		}
		return
	}
	delay := min(30*time.Second<<min(u.Attempts, 7), time.Hour)
	var f *delivery.Failure
	if errors.As(err, &f) && f.RetryAfter > delay {
		delay = min(f.RetryAfter, 24*time.Hour)
	}
	s.log.Warn("Native unlink retry scheduled", "pairing_id", u.ID, "code", safeCode(err))
	if err = s.store.DelayNativeUnlink(ctx, u.ID, time.Now().Add(delay)); err != nil {
		s.log.Warn("Native unlink save failed", "pairing_id", u.ID, "code", safeCode(err))
	}
}

func (s *Service) deletePairing(ctx context.Context, id string) error {
	info, err := s.ensure(ctx)
	if err != nil {
		return err
	}
	identity, err := s.identity.load(ctx)
	if err != nil {
		return err
	}
	err = s.client.call(ctx, identity, info.Audience, "DELETE", "/v1/pairings/"+id, nil, nil, 200)
	if hasStatus(err, 202, 204) {
		return nil
	}
	return err
}

func (s *Service) pollUnlinks(ctx context.Context) error {
	items, err := s.store.DueNativeUnlinks(ctx)
	if err != nil {
		return err
	}
	for _, u := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.unlinkAtRelay(ctx, u)
	}
	return nil
}
func hasStatus(err error, statuses ...int) bool {
	var f *delivery.Failure
	if !errors.As(err, &f) {
		return false
	}
	for _, status := range statuses {
		if f.HTTPStatus == status {
			return true
		}
	}
	return false
}
func safeCode(err error) string {
	var f *delivery.Failure
	if errors.As(err, &f) {
		return f.Code
	}
	return fault.From(err, "native_pairing_failed").Code
}
func temporary(err error) bool { var f *delivery.Failure; return errors.As(err, &f) && f.Retryable }

type Claim struct {
	DeviceID      string `json:"device_id"`
	SigningKey    string `json:"signing_key"`
	EncryptionKey string `json:"encryption_key"`
	Signature     string `json:"device_signature"`
	Name          string `json:"device_name"`
	ExpiresAt     int64  `json:"expires_at"`
}

func validateClaim(identity *Identity, info Info, p storage.NativePairing, c Claim) error {
	id, err := KeyID(c.SigningKey)
	if err != nil || id != c.DeviceID {
		return fault.New("native_signature_invalid")
	}
	if c.ExpiresAt != p.ExpiresAt || c.ExpiresAt <= time.Now().Unix() {
		return fault.New("native_pairing_expired")
	}
	if !utf8.ValidString(c.Name) || utf8.RuneCountInString(c.Name) > 64 {
		return fault.New("native_protocol_invalid")
	}
	encryptionID, err := KeyID(c.EncryptionKey)
	if err != nil {
		return err
	}
	return verify(c.SigningKey, c.Signature, consentText(info.Audience, p.ID, identity.ID, c.DeviceID, encryptionID, p.TokenHash, strconv.FormatInt(c.ExpiresAt, 10)))
}

type relayPairing struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	DeviceID      string `json:"device_id"`
	EncryptionKey string `json:"encryption_key"`
}

func (s *Service) Poll(ctx context.Context) error {
	if !s.pairingPolling.CompareAndSwap(false, true) {
		return nil
	}
	defer s.pairingPolling.Store(false)
	if !s.Available() {
		return nil
	}
	if err := s.pollUnlinks(ctx); err != nil {
		return err
	}
	waiting, err := s.store.NativePairings(ctx, "waiting")
	if err != nil || len(waiting) == 0 {
		return err
	}
	info, err := s.ensure(ctx)
	if err != nil {
		for _, p := range waiting {
			if p.ExpiresAt <= time.Now().Unix() {
				if cleanupErr := s.finish(ctx, p, "expired", "native_pairing_expired"); cleanupErr != nil {
					return cleanupErr
				}
			}
		}
		return err
	}
	identity, err := s.identity.load(ctx)
	if err != nil {
		return err
	}
	for _, p := range waiting {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = s.pollWaiting(ctx, identity, info, p.ID); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) finish(ctx context.Context, p storage.NativePairing, state, code string) error {
	err := s.store.FinishNativePairing(ctx, p.ID, state, code)
	if err == nil {
		if state == "active" {
			s.log.Info("Native pairing active", "pairing_id", p.ID, "device_id", p.DeviceID)
		} else {
			s.log.Warn("Native pairing ended", "pairing_id", p.ID, "code", code)
		}
	}
	return err
}
func (s *Service) pollPairing(ctx context.Context, identity *Identity, info Info, p storage.NativePairing) error {
	if p.ExpiresAt <= time.Now().Unix() {
		if p.DeviceID != "" {
			var result relayPairing
			err := s.client.call(ctx, identity, info.Audience, "GET", "/v1/pairings/"+p.ID, nil, &result, 200)
			if err == nil && result.ID == p.ID && result.Status == "active" && result.DeviceID == p.DeviceID && result.EncryptionKey == p.EncryptionKey {
				return s.finish(ctx, p, "active", "")
			}
		}
		return s.finish(ctx, p, "expired", "native_pairing_expired")
	}
	if p.DeviceID == "" {
		var result struct {
			Claims []Claim `json:"claims"`
		}
		err := s.client.call(ctx, identity, info.Audience, "GET", "/v1/pairing-claims/"+p.ID, nil, &result, 200)
		if hasStatus(err, 404) || temporary(err) {
			return nil
		}
		if err != nil {
			return s.finish(ctx, p, "failed", safeCode(err))
		}
		if len(result.Claims) > 4 {
			return s.finish(ctx, p, "failed", "native_protocol_invalid")
		}
		for _, c := range result.Claims {
			if err = validateClaim(identity, info, p, c); err != nil {
				s.log.Warn("Native pairing claim rejected", "pairing_id", p.ID, "code", safeCode(err))
				continue
			}
			candidate := p
			candidate.DeviceID = c.DeviceID
			candidate.SigningKey = c.SigningKey
			candidate.EncryptionKey = c.EncryptionKey
			candidate.DeviceSignature = c.Signature
			candidate.DeviceName = c.Name
			if err = s.store.SelectNativeDevice(ctx, candidate); err != nil {
				if safeCode(err) == "native_pairing_conflict" {
					continue
				}
				return err
			}
			p = candidate
			break
		}
		if p.DeviceID == "" {
			return nil
		}
	}
	token, err := s.vault.Open("native.pairing:"+p.ID, p.EncryptedToken)
	if err != nil {
		return s.finish(ctx, p, "failed", safeCode(err))
	}
	body := map[string]any{"pairing_id": p.ID, "core_public_key": identity.PublicKey, "device_id": p.DeviceID, "device_encryption_key": p.EncryptionKey, "one_time_token": encoding.EncodeToString(token), "expires_at": p.ExpiresAt, "device_signature": p.DeviceSignature}
	var result relayPairing
	err = s.client.call(ctx, identity, info.Audience, "POST", "/v1/pairings", body, &result, 200)
	if temporary(err) {
		return nil
	}
	if err != nil {
		return s.finish(ctx, p, "failed", safeCode(err))
	}
	if result.ID != p.ID || result.Status != "active" || result.DeviceID != p.DeviceID || result.EncryptionKey != p.EncryptionKey {
		return s.finish(ctx, p, "failed", "native_protocol_invalid")
	}
	return s.finish(ctx, p, "active", "")
}
func (s *Service) pollWaiting(ctx context.Context, identity *Identity, info Info, id string) error {
	s.pairingMu.Lock()
	defer s.pairingMu.Unlock()
	p, err := s.store.NativePairing(ctx, id)
	if safeCode(err) == "native_pairing_not_found" || err == nil && p.State != "waiting" {
		return nil
	}
	if err != nil {
		return err
	}
	return s.pollPairing(ctx, identity, info, p)
}

func (s *Service) Run(ctx context.Context) {
	if !s.Available() {
		return
	}
	if err := s.StartCheck(ctx); err != nil && ctx.Err() == nil {
		s.log.Warn("Native Relay check failed", "code", safeCode(err))
	}
	var wg sync.WaitGroup
	for _, worker := range []struct {
		interval time.Duration
		poll     func(context.Context) error
	}{
		{time.Second, s.PollDeliveries}, {3 * time.Second, s.Poll},
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(worker.interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := worker.poll(ctx); err != nil && ctx.Err() == nil {
						s.log.Warn("Native polling failed", "code", safeCode(err))
					}
				}
			}
		}()
	}
	wg.Wait()
}

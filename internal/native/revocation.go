package native

import "context"

// SetPairingRevoker installs the runtime configuration transaction before Run starts.
func (s *Service) SetPairingRevoker(revoke func(context.Context, string, bool) error) {
	s.revokePairing = revoke
}

func (s *Service) revoke(ctx context.Context, id string) error { return s.revokeLocal(ctx, id, false) }

func (s *Service) revokeLocal(ctx context.Context, id string, unlink bool) error {
	var err error
	if s.revokePairing != nil {
		err = s.revokePairing(ctx, id, unlink)
	} else {
		_, err = s.store.RevokeNativePairingWithDelivery(ctx, id, unlink, nil)
	}
	if err == nil {
		s.log.Info("Native pairing revoked", "pairing_id", id)
	}
	return err
}

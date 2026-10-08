package native

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
)

// MessageReference survives display-name changes and binds the original IMAP identity.
type MessageReference struct {
	CoreID    string `json:"core_id"`
	MailboxID string `json:"mailbox_id"`
	Folder    string `json:"folder"`
	mail.Location
	Signature string `json:"signature"`
}

func (r MessageReference) signingText() string {
	raw, _ := json.Marshal([]string{"mailwake-message-v1", r.CoreID, r.MailboxID, r.Folder, strconv.FormatUint(uint64(r.UIDValidity), 10), strconv.FormatUint(uint64(r.UID), 10)})
	return string(raw)
}

func messageReference(identity *Identity, n event.Notification) (*MessageReference, error) {
	if n.Location == nil {
		return nil, nil
	}
	r := &MessageReference{CoreID: identity.ID, MailboxID: n.MailboxID, Folder: n.Folder, Location: *n.Location}
	signature, err := sign(identity.Key, r.signingText())
	r.Signature = signature
	return r, err
}

// VerifyMessageReference validates untrusted references before any mailbox connection.
func (s *Service) VerifyMessageReference(ctx context.Context, r MessageReference) error {
	identity, err := s.identity.load(ctx)
	if err != nil {
		return err
	}
	if r.CoreID != identity.ID || r.UID == 0 || r.UIDValidity == 0 || verify(identity.PublicKey, r.Signature, r.signingText()) != nil {
		return fault.New("message_not_found")
	}
	return nil
}

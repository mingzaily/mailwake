package runtime

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/mingzaily/mailwake/internal/coreidentity"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/mailview"
)

const cursorDomain = "mailwake-mail-page-v1\n"

// A cursor carries progress only. Current authorization and resource membership
// are resolved on every request; the signature binds the issuing Core identity.
type mailCursor struct {
	Version   int              `json:"v"`
	Kind      string           `json:"kind"`
	Reader    mailview.Reader  `json:"reader"`
	Expires   time.Time        `json:"expires"`
	Catalog   string           `json:"catalog,omitempty"`
	Offset    int              `json:"offset,omitempty"`
	MailboxID string           `json:"mailbox,omitempty"`
	AccountID string           `json:"account,omitempty"`
	Folder    string           `json:"folder,omitempty"`
	Revision  int64            `json:"revision,omitempty"`
	Scan      mail.HistoryScan `json:"scan,omitempty"`
}

func (c mailCursor) sign(identity *coreidentity.Identity) (string, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	signature, err := coreidentity.Sign(identity.Key, cursorDomain+payload)
	if err != nil {
		return "", err
	}
	return payload + "." + signature, nil
}

func readMailCursor(identity *coreidentity.Identity, token, kind string, reader mailview.Reader, now time.Time) (mailCursor, error) {
	invalid := fault.New("sync_cursor_invalid")
	if len(token) > 64<<10 {
		return mailCursor{}, invalid
	}
	payload, signature, ok := strings.Cut(token, ".")
	if !ok || coreidentity.Verify(identity.PublicKey, signature, cursorDomain+payload) != nil {
		return mailCursor{}, invalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return mailCursor{}, invalid
	}
	var cursor mailCursor
	if json.Unmarshal(raw, &cursor) != nil || cursor.Version != 1 || cursor.Kind != kind || cursor.Reader != reader || !now.Before(cursor.Expires) {
		return mailCursor{}, invalid
	}
	return cursor, nil
}

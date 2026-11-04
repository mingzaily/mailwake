package imap

import (
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
)

// go-imap sends this response inline or after the initial empty continuation.
type xoauth2 struct{ user, token string }

func (x xoauth2) Start() (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + x.user + "\x01auth=Bearer " + x.token + "\x01\x01"), nil
}

// A subsequent challenge contains the provider's rejection details. Close the
// failed exchange through the existing login error path.
func (xoauth2) Next([]byte) ([]byte, error) { return nil, fault.New(mail.OutlookReauthorize) }

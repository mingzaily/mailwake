package native

import (
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/mail"
	"testing"
)

func TestMessageReferenceSignsOriginalAccount(t *testing.T) {
	service, _ := testService(t, nil)
	identity, err := service.CoreIdentity(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	reference, err := messageReference(identity, event.Notification{MailboxID: "mailbox", AccountID: "original-account", Folder: "INBOX", Location: &mail.Location{UIDValidity: 1, UID: 7}})
	if err != nil {
		t.Fatal(err)
	}
	if reference.AccountID != "original-account" {
		t.Fatal("original account missing")
	}
	if err := service.VerifyMessageReference(t.Context(), *reference); err != nil {
		t.Fatal(err)
	}
	reference.AccountID = "replacement-account"
	if err := service.VerifyMessageReference(t.Context(), *reference); err == nil {
		t.Fatal("changed account passed signature verification")
	}
}

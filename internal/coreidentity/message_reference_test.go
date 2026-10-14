package coreidentity

import (
	"testing"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestMessageReferenceSignsOriginalAccount(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	vault, err := settings.OpenVault(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	service := New(db, vault)
	identity, err := service.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	reference, err := identity.MessageReference(event.Notification{MailboxID: "mailbox", AccountID: "original-account", Folder: "INBOX", Location: &mail.Location{UIDValidity: 1, UID: 7}})
	if err != nil {
		t.Fatal(err)
	}
	if reference.AccountID != "original-account" {
		t.Fatal("original account missing")
	}
	if err := service.VerifyMessageReference(t.Context(), *reference); err != nil {
		t.Fatal(err)
	}

	for name, mutate := range map[string]func(*MessageReference){
		"core":        func(r *MessageReference) { r.CoreID = "other" },
		"mailbox":     func(r *MessageReference) { r.MailboxID = "other" },
		"account":     func(r *MessageReference) { r.AccountID = "other" },
		"folder":      func(r *MessageReference) { r.Folder = "other" },
		"uid":         func(r *MessageReference) { r.UID++ },
		"uidvalidity": func(r *MessageReference) { r.UIDValidity++ },
		"signature":   func(r *MessageReference) { r.Signature = "invalid" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := *reference
			mutate(&changed)
			if err := service.VerifyMessageReference(t.Context(), changed); fault.From(err, "").Code != "message_not_found" {
				t.Fatalf("modified reference: %v", err)
			}
		})
	}
	if err := New(db, vault).VerifyMessageReference(t.Context(), *reference); err != nil {
		t.Fatalf("persisted identity rejected reference: %v", err)
	}
	if empty, err := identity.MessageReference(event.Notification{}); err != nil || empty != nil {
		t.Fatalf("notification without location: %v, %v", empty, err)
	}
}

package application

import (
	"io"
	"log/slog"
	"net/url"
	"testing"

	"github.com/mingzaily/mailwake/internal/appmanagement"
	"github.com/mingzaily/mailwake/internal/coreidentity"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestManagementAndContentIdentitySurviveRestartWithoutRelay(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	vault, err := settings.OpenVault(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := New(t.Context(), store, vault, log, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	invitation, err := app.Management.Create(t.Context(), appmanagement.InvitationInput{Management: true, CoreOrigin: "https://core.example.test", Scopes: []string{"content"}})
	if err != nil {
		t.Fatal(err)
	}
	uri, err := url.Parse(invitation.URI)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := app.Management.Identity.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	grant, err := app.Management.Accept(t.Context(), appmanagement.AcceptInput{InvitationID: invitation.ID, Token: uri.Query().Get("t"), SigningKey: identity.PublicKey, DeviceName: "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	reference, err := identity.MessageReference(event.Notification{MailboxID: "missing", AccountID: "original", Folder: "INBOX", Location: &mail.Location{UID: 1, UIDValidity: 1}})
	if err != nil {
		t.Fatal(err)
	}
	app.Close()
	app.Close()
	store.Close()
	store, err = storage.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	app, err = New(t.Context(), store, vault, log, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	controller, err := store.AuthenticateAppController(t.Context(), appmanagement.Hash([]byte(grant.Credential)))
	if err != nil || !controller.HasScope("content") {
		t.Fatalf("grant after restart: %+v, %v", controller, err)
	}
	if err := coreidentity.New(store, vault).VerifyMessageReference(t.Context(), *reference); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ReadContent(t.Context(), *reference); fault.From(err, "").Code != "message_not_found" {
		t.Fatalf("missing mailbox: %v", err)
	}
	if len(app.Mailboxes()) != 0 || app.Channel() != "" {
		t.Fatal("unexpected mailbox or channel after restart")
	}
}

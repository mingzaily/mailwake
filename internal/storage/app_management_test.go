package storage

import (
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
)

func TestAppInvitationIsSingleUseAndInstallsController(t *testing.T) {
	ctx := t.Context()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().Unix()
	invitation := AppInvitation{ID: "inv", TokenHash: "token-hash", Management: true, Scopes: []string{"channels", "mailboxes"}, ExpiresAt: now + 300, CreatedAt: now}
	if err := s.CreateAppInvitation(ctx, invitation); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptAppInvitation(ctx, "inv", "wrong", AppController{ID: "c0", DeviceID: "d1", DeviceName: "Phone"}, "cred0"); fault.From(err, "").Code != "unauthorized" {
		t.Fatalf("wrong token: %v", err)
	}
	controller, err := s.AcceptAppInvitation(ctx, "inv", "token-hash", AppController{ID: "c1", DeviceID: "d1", DeviceName: "Phone"}, "cred1")
	if err != nil || len(controller.Scopes) != 2 {
		t.Fatalf("accept: %+v %v", controller, err)
	}
	if _, err := s.AcceptAppInvitation(ctx, "inv", "token-hash", AppController{ID: "c2", DeviceID: "d2", DeviceName: "Other"}, "cred2"); fault.From(err, "").Code != "invitation_used" {
		t.Fatalf("second accept: %v", err)
	}
	authenticated, err := s.AuthenticateAppController(ctx, "cred1")
	if err != nil || authenticated.ID != "c1" || !authenticated.HasScope("channels") || authenticated.HasScope("folders") {
		t.Fatalf("authenticate: %+v %v", authenticated, err)
	}
	if err := s.DeleteAppController(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateAppController(ctx, "cred1"); fault.From(err, "").Code != "unauthorized" {
		t.Fatalf("revoked credential: %v", err)
	}
}

func TestAppInvitationExpires(t *testing.T) {
	ctx := t.Context()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().Unix()
	if err := s.CreateAppInvitation(ctx, AppInvitation{ID: "old", TokenHash: "h", Management: true, Scopes: []string{"mailboxes"}, ExpiresAt: now - 1, CreatedAt: now - 301}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptAppInvitation(ctx, "old", "h", AppController{ID: "c", DeviceID: "d", DeviceName: "Phone"}, "cred"); fault.From(err, "").Code != "invitation_expired" {
		t.Fatalf("expired: %v", err)
	}
}

func TestReacceptingReplacesTheDeviceController(t *testing.T) {
	ctx := t.Context()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().Unix()
	for i, id := range []string{"a", "b"} {
		if err := s.CreateAppInvitation(ctx, AppInvitation{ID: id, TokenHash: id, Management: true, Scopes: []string{"mailboxes"}, ExpiresAt: now + 300, CreatedAt: now + int64(i)}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AcceptAppInvitation(ctx, id, id, AppController{ID: "c" + id, DeviceID: "same-device", DeviceName: "Phone"}, "cred"+id); err != nil {
			t.Fatal(err)
		}
	}
	controllers, err := s.AppControllers(ctx)
	if err != nil || len(controllers) != 1 || controllers[0].ID != "cb" {
		t.Fatalf("controllers: %+v %v", controllers, err)
	}
}

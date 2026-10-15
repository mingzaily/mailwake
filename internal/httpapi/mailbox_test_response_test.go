package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/settings"
)

type mailboxProbeRuntime struct {
	Runtime
	probe func(context.Context, string, settings.MailboxUpdate) (*mail.FolderDiscovery, error)
}

func (r mailboxProbeRuntime) TestMailbox(ctx context.Context, id string, input settings.MailboxUpdate) (*mail.FolderDiscovery, error) {
	return r.probe(ctx, id, input)
}

func TestMailboxProbeKeepsWebFoldersAndAppConnectionOnly(t *testing.T) {
	runtime := mailboxProbeRuntime{Runtime: newTestRuntime("", nil, nil), probe: func(context.Context, string, settings.MailboxUpdate) (*mail.FolderDiscovery, error) {
		return &mail.FolderDiscovery{Folders: []string{"Private folder"}}, nil
	}}
	f := newAppFixture(t, runtime)
	credential, _ := f.invite(t, []string{"mailboxes"})
	bearer := map[string]string{"Authorization": "Bearer " + credential}
	for _, path := range []string{"/mailboxes/test", "/mailboxes/box/test"} {
		web := f.do(t, "POST", "/api/v1"+path, map[string]any{}, nil)
		app := f.do(t, "POST", "/api/v1/app"+path, map[string]any{}, bearer)
		if web.Code != 200 || app.Code != 200 {
			t.Fatalf("web=%d %s; app=%d %s", web.Code, web.Body.String(), app.Code, app.Body.String())
		}
		var folders mail.FolderDiscovery
		if err := json.Unmarshal(web.Body.Bytes(), &folders); err != nil || len(folders.Folders) != 1 || folders.Folders[0] != "Private folder" {
			t.Fatalf("web folders: %s, %v", web.Body.String(), err)
		}
		var connected map[string]any
		if err := json.Unmarshal(app.Body.Bytes(), &connected); err != nil || len(connected) != 1 || connected["status"] != "connected" {
			t.Fatalf("App response: %s, %v", app.Body.String(), err)
		}
	}
	if w := f.do(t, "GET", "/api/v1/app/mailboxes/box/folders", nil, bearer); w.Code != 403 {
		t.Fatalf("folder permission: %d %s", w.Code, w.Body.String())
	}
}

func TestMailboxProbeErrorsHaveSameClientContract(t *testing.T) {
	runtime := mailboxProbeRuntime{Runtime: newTestRuntime("", nil, nil), probe: func(context.Context, string, settings.MailboxUpdate) (*mail.FolderDiscovery, error) {
		return nil, fault.New("discovery_busy")
	}}
	f := newAppFixture(t, runtime)
	credential, _ := f.invite(t, []string{"mailboxes"})
	web := f.do(t, "POST", "/api/v1/mailboxes/test", map[string]any{}, nil)
	app := f.do(t, "POST", "/api/v1/app/mailboxes/test", map[string]any{}, map[string]string{"Authorization": "Bearer " + credential})
	if web.Code != 429 || app.Code != web.Code || app.Body.String() != web.Body.String() {
		t.Fatalf("web=%d %s; app=%d %s", web.Code, web.Body.String(), app.Code, app.Body.String())
	}
}

type displayMailboxRuntime struct{ Runtime }

func (r displayMailboxRuntime) Mailbox(id string) (settings.MailboxView, error) {
	return settings.MailboxView{ID: id, FolderDisplay: &mail.FolderDisplay{FolderNames: map[string]string{"Private/Folder": "Folder"}}}, nil
}
func (r displayMailboxRuntime) Mailboxes() []settings.MailboxView {
	value, _ := r.Mailbox("box")
	return []settings.MailboxView{value}
}
func (r displayMailboxRuntime) CreateMailbox(context.Context, settings.MailboxUpdate) (settings.MailboxView, error) {
	return r.Mailbox("box")
}
func (r displayMailboxRuntime) UpdateMailbox(context.Context, string, settings.MailboxUpdate) error {
	return nil
}
func TestFolderDisplayIsLimitedToOwnerMailboxResponses(t *testing.T) {
	f := newAppFixture(t, displayMailboxRuntime{newTestRuntime("", nil, nil)})
	credential, _ := f.invite(t, []string{"mailboxes"})
	for _, tc := range []struct {
		method, path string
		body         any
	}{{"GET", "/mailboxes", nil}, {"GET", "/mailboxes/box", nil}, {"POST", "/mailboxes", map[string]any{}}, {"PUT", "/mailboxes/box", map[string]any{"revision": 1}}} {
		web := f.do(t, tc.method, "/api/v1"+tc.path, tc.body, nil)
		app := f.do(t, tc.method, "/api/v1/app"+tc.path, tc.body, map[string]string{"Authorization": "Bearer " + credential})
		if web.Code >= 300 || app.Code >= 300 {
			t.Fatalf("status web=%d app=%d", web.Code, app.Code)
		}
		if !strings.Contains(web.Body.String(), "Private/Folder") || strings.Contains(app.Body.String(), "folder_display") {
			t.Fatalf("directory boundary: web=%s app=%s", web.Body, app.Body)
		}
	}
}

type staleFolderRuntime struct{ Runtime }

func (staleFolderRuntime) Folders(context.Context, string) (*mail.FolderDiscovery, error) {
	return nil, fault.New("settings_conflict")
}
func TestScanConfigurationConflictReturns409(t *testing.T) {
	f := newAppFixture(t, staleFolderRuntime{newTestRuntime("", nil, nil)})
	credential, _ := f.invite(t, []string{"folders"})
	for path, headers := range map[string]map[string]string{"/api/v1/mailboxes/box/folders": nil, "/api/v1/app/mailboxes/box/folders": {"Authorization": "Bearer " + credential}} {
		w := f.do(t, "GET", path, nil, headers)
		if w.Code != 409 || !strings.Contains(w.Body.String(), "settings_conflict") {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
}

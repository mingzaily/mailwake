package httpapi

import (
	"context"
	"encoding/json"
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

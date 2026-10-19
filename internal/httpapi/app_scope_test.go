package httpapi

import (
	"context"
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/mail"
)

type subscriptionRuntime struct{ Runtime }

func (subscriptionRuntime) Subscriptions(string) (mail.Subscriptions, error) {
	return mail.Subscriptions{Revision: 1, Folders: []mail.Folder{}}, nil
}
func (subscriptionRuntime) UpdateSubscriptions(_ context.Context, _ string, input mail.Subscriptions) (mail.Subscriptions, error) {
	return input, nil
}

func TestAppRoutesEnforceTheirOwnScopes(t *testing.T) {
	f := newAppFixture(t, subscriptionRuntime{newTestRuntime("bark", nil, folderSource{})})
	scopes := []string{"mailboxes", "folders", "channels", "diagnostics", "content"}
	credentials := map[string]string{}
	for _, scope := range scopes {
		credentials[scope], _ = f.invite(t, []string{scope})
	}
	routes := []struct {
		method, path, scope string
		body                any
		status              int
	}{
		{"GET", "/mailboxes", "mailboxes", nil, 200},
		{"POST", "/mailboxes", "mailboxes", map[string]any{}, 400},
		{"GET", "/mailboxes/box", "mailboxes", nil, 200},
		{"PUT", "/mailboxes/box", "mailboxes", map[string]any{"revision": 1}, 400},
		{"DELETE", "/mailboxes/box", "mailboxes", nil, 404},
		{"POST", "/mailboxes/test", "mailboxes", map[string]any{}, 400},
		{"POST", "/mailboxes/box/test", "mailboxes", map[string]any{}, 400},
		{"POST", "/mailboxes/box/test-saved", "mailboxes", nil, 400},
		{"GET", "/folder-mailboxes", "folders", nil, 200},
		{"GET", "/mailboxes/box/folders", "folders", nil, 200},
		{"GET", "/mailboxes/box/subscriptions", "folders", nil, 200},
		{"PUT", "/mailboxes/box/subscriptions", "folders", map[string]any{"revision": 1, "folders": []string{}}, 202},
		{"GET", "/native/devices", "channels", nil, 200},
		{"GET", "/settings/delivery", "channels", nil, 200},
		{"PUT", "/settings/delivery", "channels", map[string]any{"revision": 0}, 400},
		{"DELETE", "/settings/delivery", "channels", map[string]any{"revision": 0}, 400},
		{"POST", "/settings/delivery/test", "channels", map[string]any{}, 400},
		{"POST", "/settings/delivery/test-saved", "channels", nil, 400},
		{"GET", "/status", "diagnostics", nil, 200},
		{"GET", "/diagnostics", "diagnostics", nil, 200},
		{"GET", "/deliveries", "diagnostics", nil, 200},
		{"POST", "/content", "content", map[string]any{}, 200},
		{"POST", "/content/read", "content", map[string]any{}, 204},
		{"GET", "/controller", "", nil, 200},
	}
	for _, route := range routes {
		t.Run(route.method+route.path, func(t *testing.T) {
			if w := f.do(t, route.method, appPrefix+route.path, route.body, map[string]string{}); w.Code != 401 {
				t.Fatalf("unauthenticated: %d %s", w.Code, w.Body.String())
			}
			for _, scope := range scopes {
				w := f.do(t, route.method, appPrefix+route.path, route.body, map[string]string{"Authorization": "Bearer " + credentials[scope]})
				if route.scope == "" || scope == route.scope {
					if w.Code != route.status {
						t.Fatalf("scope %s: %d %s; want %d", scope, w.Code, w.Body.String(), route.status)
					}
				} else {
					code := "forbidden"
					if route.scope == "content" {
						code = "content_permission_required"
					}
					if w.Code != 403 || !strings.Contains(w.Body.String(), code) {
						t.Fatalf("outside scope %s: %d %s", scope, w.Code, w.Body.String())
					}
				}
			}
		})
	}
	for _, scope := range scopes {
		headers := map[string]string{"Authorization": "Bearer " + credentials[scope]}
		if w := f.do(t, "DELETE", appPrefix+"/controller", nil, headers); w.Code != 204 {
			t.Fatalf("self revoke: %d %s", w.Code, w.Body.String())
		}
		if w := f.do(t, "GET", appPrefix+"/controller", nil, headers); w.Code != 401 {
			t.Fatalf("revoked: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestAppRoutesKeepAdministratorOperationsOutsideDeviceAccess(t *testing.T) {
	f := newAppFixture(t)
	credential, _ := f.invite(t, []string{"mailboxes", "folders", "channels", "diagnostics", "content"})
	headers := map[string]string{"Authorization": "Bearer " + credential}
	for _, route := range []struct{ method, path string }{
		{"GET", "/tokens"}, {"GET", "/sessions"}, {"GET", "/logs"}, {"POST", "/native/pairings"}, {"DELETE", "/management/devices/other"}, {"POST", "/deliveries/event/retry"},
	} {
		w := f.do(t, route.method, appPrefix+route.path, nil, headers)
		if w.Code != 404 || !strings.Contains(w.Body.String(), "route_not_found") {
			t.Fatalf("%s %s: %d %s", route.method, route.path, w.Code, w.Body.String())
		}
	}
	if w := f.do(t, "GET", "/api/v1/mailboxes", nil, headers); w.Code != 401 {
		t.Fatalf("App credential accepted as administrator: %d %s", w.Code, w.Body.String())
	}
}

func TestSavedTestLimitsAreIsolatedByControllerAndTarget(t *testing.T) {
	f := newAppFixture(t)
	first, _ := f.invite(t, []string{"mailboxes", "channels"})
	second, _ := f.invite(t, []string{"mailboxes", "channels"})
	for _, credential := range []string{first, second} {
		headers := map[string]string{"Authorization": "Bearer " + credential}
		for _, path := range []string{"/mailboxes/a/test-saved", "/mailboxes/b/test-saved", "/settings/delivery/test-saved"} {
			if w := f.do(t, "POST", appPrefix+path, nil, headers); w.Code != 400 {
				t.Fatalf("first attempt: %d %s", w.Code, w.Body.String())
			}
			if w := f.do(t, "POST", appPrefix+path, nil, headers); w.Code != 429 || w.Header().Get("Retry-After") != "60" {
				t.Fatalf("repeated attempt: %d %s", w.Code, w.Body.String())
			}
		}
	}
}

package httpapi

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/appmanagement"
	"github.com/mingzaily/mailwake/internal/auth"
	"github.com/mingzaily/mailwake/internal/native"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

var b64 = base64.RawURLEncoding

type appRuntime struct {
	Runtime
	service *appmanagement.Service
}

func (r appRuntime) AppManagement() *appmanagement.Service { return r.service }

type appFixture struct {
	router  http.Handler
	grant   auth.Grant
	coreID  string
	service *appmanagement.Service
}

func newAppFixture(t *testing.T, runtimes ...Runtime) appFixture {
	t.Helper()
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
	nativeService, err := native.New(store, vault, "", log)
	if err != nil {
		t.Fatal(err)
	}
	service := &appmanagement.Service{Store: store, Native: nativeService}
	identity, err := nativeService.CoreIdentity(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	authService, grant := testAdministrator(t, store)
	base := Runtime(newTestRuntime("bark", nil, nil))
	if len(runtimes) > 0 {
		base = runtimes[0]
	}
	router := New(authService, appRuntime{base, service}, store, log)
	return appFixture{router, grant, identity.ID, service}
}

func (f appFixture) do(t *testing.T, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = strings.NewReader(string(raw))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if headers == nil {
		authorizeSession(req, f.grant)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

// invite creates a management invitation and accepts it as a new device.
func (f appFixture) invite(t *testing.T, scopes []string) (credential, deviceID string) {
	t.Helper()
	w := f.do(t, "POST", "/api/v1/device-invitations", map[string]any{"management": true, "scopes": scopes, "core_origin": "https://core.example.test"}, nil)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var invitation appmanagement.Invitation
	json.Unmarshal(w.Body.Bytes(), &invitation)
	uri, _ := url.Parse(invitation.URI)
	query := uri.Query()
	if uri.Scheme != "mailwake" || uri.Host != "connect" || query.Get("v") != "3" || query.Get("origin") != "https://core.example.test" || query.Get("pair") != "" {
		t.Fatal(invitation.URI)
	}
	expectedScopes := slices.Clone(scopes)
	slices.Sort(expectedScopes)
	if !slices.Equal(strings.Split(query.Get("scopes"), ","), expectedScopes) {
		t.Fatal("invitation scopes", query.Get("scopes"), expectedScopes)
	}
	statusPath := "/api/v1/device-invitations/" + invitation.ID
	if response := f.do(t, "GET", statusPath, nil, map[string]string{}); response.Code != 401 {
		t.Fatal("invitation status requires authentication", response.Code)
	}
	response := f.do(t, "GET", statusPath, nil, nil)
	var status appmanagement.InvitationStatus
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil || response.Code != 200 || status.Status != "waiting" {
		t.Fatal("waiting invitation", response.Code, response.Body.String())
	}
	device, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	spki, _ := x509.MarshalPKIXPublicKey(&device.PublicKey)
	accept := map[string]any{"invitation_id": query.Get("inv"), "token": query.Get("t"), "signing_key": b64.EncodeToString(spki), "device_name": "Phone"}
	w = f.do(t, "POST", "/api/v1/app/accept", accept, map[string]string{})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	response = f.do(t, "GET", statusPath, nil, nil)
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil || response.Code != 200 || status.Status != "active" || status.DeviceName != "Phone" {
		t.Fatal("accepted invitation", response.Code, response.Body.String())
	}
	var result appmanagement.Credential
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.CoreID != f.coreID || !strings.HasPrefix(result.Credential, "mwac_") {
		t.Fatal(result)
	}
	if !slices.Equal(result.Scopes, expectedScopes) {
		t.Fatal("accepted scopes", result.Scopes, expectedScopes)
	}
	if w = f.do(t, "POST", "/api/v1/app/accept", accept, map[string]string{}); w.Code != 410 {
		t.Fatal("invitation reused", w.Code)
	}
	return result.Credential, result.DeviceID
}

func TestAppControllerAuthorizesRoutesByScope(t *testing.T) {
	f := newAppFixture(t)
	credential, _ := f.invite(t, []string{"diagnostics"})
	bearer := map[string]string{"Authorization": "Bearer " + credential}
	if w := f.do(t, "GET", "/api/v1/app/status", nil, bearer); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := f.do(t, "GET", "/api/v1/app/settings/delivery", nil, bearer); w.Code != 403 {
		t.Fatal("route outside scope", w.Code)
	}
	if w := f.do(t, "GET", "/api/v1/app/status", nil, map[string]string{"Authorization": "Bearer mwac_unknown"}); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := f.do(t, "GET", "/api/v1/app/controller", nil, bearer); w.Code != 200 || !strings.Contains(w.Body.String(), `"scopes":["diagnostics"]`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := f.do(t, "DELETE", "/api/v1/app/controller", nil, bearer); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if w := f.do(t, "GET", "/api/v1/app/status", nil, bearer); w.Code != 401 {
		t.Fatal("revoked controller still authorized", w.Code)
	}
}

func TestAppConfigurationRequiresOwnerScope(t *testing.T) {
	f := newAppFixture(t)
	credential, _ := f.invite(t, []string{"channels"})
	body := map[string]any{"revision": 0, "channel": "bark", "preview": "off", "language": "en"}
	// The runtime rejects incomplete settings after device authorization succeeds.
	bearer := map[string]string{"Authorization": "Bearer " + credential}
	if w := f.do(t, "PUT", "/api/v1/app/settings/delivery", body, bearer); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	other, _ := f.invite(t, []string{"diagnostics"})
	if w := f.do(t, "PUT", "/api/v1/app/settings/delivery", body, map[string]string{"Authorization": "Bearer " + other}); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestSavedTestsAreLimitedPerTarget(t *testing.T) {
	f := newAppFixture(t)
	credential, _ := f.invite(t, []string{"channels"})
	bearer := map[string]string{"Authorization": "Bearer " + credential}
	if w := f.do(t, "POST", "/api/v1/app/settings/delivery/test-saved", nil, bearer); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := f.do(t, "POST", "/api/v1/app/settings/delivery/test-saved", nil, bearer); w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatal(w.Code)
	}
}

func TestAdministratorListsAndRevokesControllers(t *testing.T) {
	f := newAppFixture(t)
	credential, deviceID := f.invite(t, []string{"mailboxes"})
	w := f.do(t, "GET", "/api/v1/management/devices", nil, nil)
	var listed struct {
		Devices []struct {
			ID       string `json:"controller_id"`
			DeviceID string `json:"device_id"`
		} `json:"devices"`
	}
	json.Unmarshal(w.Body.Bytes(), &listed)
	if len(listed.Devices) != 1 || listed.Devices[0].DeviceID != deviceID {
		t.Fatal(w.Body.String())
	}
	if w = f.do(t, "DELETE", "/api/v1/management/devices/"+listed.Devices[0].ID, nil, nil); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if w = f.do(t, "GET", "/api/v1/app/mailboxes", nil, map[string]string{"Authorization": "Bearer " + credential}); w.Code != 401 {
		t.Fatal(w.Code)
	}
}

func TestContentRequiresOwnerScopeAndActiveCredential(t *testing.T) {
	f := newAppFixture(t)
	credential, _ := f.invite(t, []string{"content"})
	headers := map[string]string{"Authorization": "Bearer " + credential}
	w := f.do(t, "POST", "/api/v1/app/content", map[string]any{"reference": map[string]any{}}, headers)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	readCredential, _ := f.invite(t, []string{"mailboxes"})
	headers["Authorization"] = "Bearer " + readCredential
	if w := f.do(t, "POST", "/api/v1/app/content", map[string]any{}, headers); w.Code != 403 || !strings.Contains(w.Body.String(), "content_permission_required") {
		t.Fatal(w.Code, w.Body.String())
	}
	headers["Authorization"] = "Bearer " + credential
	f.do(t, "DELETE", "/api/v1/app/controller", nil, headers)
	if w := f.do(t, "POST", "/api/v1/app/content", map[string]any{}, headers); w.Code != 401 {
		t.Fatal(w.Code, w.Body.String())
	}
}

type folderDirectoryRuntime struct{ Runtime }

func (folderDirectoryRuntime) Mailboxes() []map[string]any {
	return []map[string]any{{"id": "mail", "label": "Work", "revision": int64(2), "connection_limit": 5,
		"host": "imap.private.test", "username": "owner@private.test", "password": map[string]bool{"configured": true}}}
}
func TestFolderOnlyControllerCanReadMailboxDirectoryWithoutMailboxAccess(t *testing.T) {
	f := newAppFixture(t, folderDirectoryRuntime{newTestRuntime("bark", nil, nil)})
	credential, _ := f.invite(t, []string{"folders"})
	bearer := map[string]string{"Authorization": "Bearer " + credential}
	w := f.do(t, "GET", "/api/v1/app/folder-mailboxes", nil, bearer)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var directory struct {
		Mailboxes []map[string]any `json:"mailboxes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &directory); err != nil {
		t.Fatal(err)
	}
	if len(directory.Mailboxes) != 1 {
		t.Fatal(directory)
	}
	mailbox := directory.Mailboxes[0]
	if mailbox["id"] != "mail" || mailbox["label"] != "Work" || mailbox["connection_limit"] != float64(5) {
		t.Fatal(mailbox)
	}
	for _, field := range []string{"host", "username", "password"} {
		if _, exists := mailbox[field]; exists {
			t.Fatal("mailbox login exposed", field)
		}
	}
	for _, path := range []string{"/api/v1/app/mailboxes", "/api/v1/app/native/devices"} {
		if w := f.do(t, "GET", path, nil, bearer); w.Code != 403 {
			t.Fatal(path, w.Code)
		}
	}
	other, _ := f.invite(t, []string{"diagnostics"})
	if w := f.do(t, "GET", "/api/v1/app/folder-mailboxes", nil, map[string]string{"Authorization": "Bearer " + other}); w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func TestChannelControllerCanListReceivingDevices(t *testing.T) {
	f := newAppFixture(t)
	for _, id := range []string{"active-phone", "revoked-phone"} {
		pairing := storage.NativePairing{ID: id, DeviceID: id, DeviceName: "iPhone", TokenHash: id, CreatedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(time.Hour).Unix()}
		if err := f.service.Store.CreateNativePairing(t.Context(), pairing); err != nil {
			t.Fatal(err)
		}
		if err := f.service.Store.SelectNativeDevice(t.Context(), pairing); err != nil {
			t.Fatal(err)
		}
		if err := f.service.Store.FinishNativePairing(t.Context(), id, "active", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.service.Store.RevokeNativePairing(t.Context(), "revoked-phone"); err != nil {
		t.Fatal(err)
	}
	credential, _ := f.invite(t, []string{"channels"})
	w := f.do(t, "GET", "/api/v1/app/native/devices", nil, map[string]string{"Authorization": "Bearer " + credential})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Devices []map[string]any `json:"devices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Devices) != 1 || response.Devices[0]["id"] != "active-phone" || response.Devices[0]["device_name"] != "iPhone" {
		t.Fatal(response)
	}
	for _, field := range []string{"token_hash", "encrypted_token", "signing_key", "encryption_key"} {
		if _, exists := response.Devices[0][field]; exists {
			t.Fatal("pairing secret exposed", field)
		}
	}
}

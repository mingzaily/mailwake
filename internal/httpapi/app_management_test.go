package httpapi

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
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
	issuer  *ecdsa.PrivateKey
	service *appmanagement.Service
}

func newAppFixture(t *testing.T) appFixture {
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
	issuer, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	spki, _ := x509.MarshalPKIXPublicKey(&issuer.PublicKey)
	service := &appmanagement.Service{Store: store, Native: nativeService, Trust: appmanagement.Trust{Issuer: "mailwake-platform", Environment: "sandbox", Keys: map[string]string{"k1": b64.EncodeToString(spki)}}}
	identity, err := nativeService.CoreIdentity(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	authService, grant := testAdministrator(t, store)
	router := New(authService, appRuntime{newTestRuntime("bark", nil, nil), service}, store, log)
	return appFixture{router, grant, identity.ID, issuer, service}
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

func (f appFixture) qualification(t *testing.T, deviceID string, claims map[string]any) string {
	t.Helper()
	base := map[string]any{"iss": "mailwake-platform", "aud": "mailwake-core", "sub": "s", "purpose": "app_management", "environment": "sandbox", "device_id": deviceID, "core_id": f.coreID, "iat": time.Now().Unix(), "exp": time.Now().Unix() + 300}
	for k, v := range claims {
		base[k] = v
	}
	header, _ := json.Marshal(map[string]string{"alg": "ES256", "typ": "JWT", "kid": "k1"})
	payload, _ := json.Marshal(base)
	input := b64.EncodeToString(header) + "." + b64.EncodeToString(payload)
	digest := sha256.Sum256([]byte(input))
	r, s, _ := ecdsa.Sign(rand.Reader, f.issuer, digest[:])
	signature := append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
	return input + "." + b64.EncodeToString(signature)
}

func TestAppControllerAuthorizesFreeRoutesByScope(t *testing.T) {
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

func TestPaidRoutesRequireAQualificationForThisCoreAndDevice(t *testing.T) {
	f := newAppFixture(t)
	credential, deviceID := f.invite(t, []string{"channels"})
	revision := 0
	body := map[string]any{"revision": revision, "channel": "bark", "preview": "off", "language": "en"}
	call := func(qualification string) int {
		headers := map[string]string{"Authorization": "Bearer " + credential}
		if qualification != "" {
			headers["X-Mailwake-App-Qualification"] = qualification
		}
		return f.do(t, "PUT", "/api/v1/app/settings/delivery", body, headers).Code
	}
	if code := call(""); code != 403 {
		t.Fatal("paid route without qualification", code)
	}
	for name, claims := range map[string]map[string]any{
		"another device": {"device_id": strings.Repeat("0", 64)},
		"another Core":   {"core_id": strings.Repeat("0", 64)},
		"expired":        {"exp": time.Now().Unix() - 60},
		"another issuer": {"iss": "someone-else"},
	} {
		if code := call(f.qualification(t, deviceID, claims)); code != 403 {
			t.Fatal(name, code)
		}
	}
	// The runtime fixture rejects the update itself; reaching it proves authorization passed.
	if code := call(f.qualification(t, deviceID, nil)); code != 400 {
		t.Fatal("valid qualification", code)
	}
	// Free routes in the same scope need no qualification.
	if w := f.do(t, "GET", "/api/v1/app/settings/delivery", nil, map[string]string{"Authorization": "Bearer " + credential}); w.Code != 200 {
		t.Fatal(w.Code)
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

func TestContentRequiresOwnerScopeAndNativeQualification(t *testing.T) {
	f := newAppFixture(t)
	credential, device := f.invite(t, []string{"content"})
	headers := map[string]string{"Authorization": "Bearer " + credential}
	for _, claims := range []map[string]any{
		{"purpose": "app_management"}, {"purpose": "native_push", "core_id": "other"},
		{"purpose": "native_push", "device_id": "other"}, {"purpose": "native_push", "environment": "production"},
		{"purpose": "native_push", "exp": time.Now().Unix() - 1}, {"purpose": "native_push", "exp": time.Now().Unix() + 301},
	} {
		headers["X-Mailwake-Native-Qualification"] = f.qualification(t, device, claims)
		if w := f.do(t, "POST", "/api/v1/app/content", map[string]any{"reference": map[string]any{}}, headers); w.Code != 403 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	headers["X-Mailwake-Native-Qualification"] = f.qualification(t, device, map[string]any{"purpose": "native_push"})
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

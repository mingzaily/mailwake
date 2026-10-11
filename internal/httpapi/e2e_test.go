package httpapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/mingzaily/mailwake/internal/appmanagement"
	"github.com/mingzaily/mailwake/internal/config"
	"github.com/mingzaily/mailwake/internal/logging"
	"github.com/mingzaily/mailwake/internal/native"
	"github.com/mingzaily/mailwake/internal/runtime"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

// scripts/e2e.mjs owns the local Platform, Relay, device identity and APNs receiver.
func TestThreeServiceEndToEnd(t *testing.T) {
	relayURL := os.Getenv("MAILWAKE_E2E_RELAY")
	platformURL := os.Getenv("MAILWAKE_E2E_PLATFORM")
	if relayURL == "" || platformURL == "" {
		t.Skip("requires isolated local services from scripts/e2e.mjs")
	}
	for _, endpoint := range []string{relayURL, platformURL, os.Getenv("MAILWAKE_E2E_APNS")} {
		u, err := url.Parse(endpoint)
		if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
			t.Fatal("local fixture required")
		}
	}
	if os.Getenv("MAILWAKE_E2E_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestThreeServiceEndToEnd$", "-test.v")
		cmd.Env = append(os.Environ(), "MAILWAKE_E2E_CHILD=1", "GODEBUG=x509usefallbackroots=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("local end-to-end: %v\n%s", err, output)
		}
		t.Log(string(output))
		return
	}
	t.Setenv("MAILWAKE_RELAY_URL", relayURL)
	cert := httptest.NewTLSServer(nil)
	defer cert.Close()
	pool := x509.NewCertPool()
	pool.AddCert(cert.Certificate())
	x509.SetFallbackRoots(pool)
	listener, err := tls.Listen("tcp", "127.0.0.1:0", cert.TLS.Clone())
	if err != nil {
		t.Fatal(err)
	}
	backend := imapmemserver.New()
	user := imapmemserver.NewUser("test@example.org", "synthetic-password")
	if err = user.Create("Clients", nil); err != nil {
		t.Fatal(err)
	}
	backend.AddUser(user)
	imapServer := imapserver.New(&imapserver.Options{NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		return backend.NewSession(), nil, nil
	}, Caps: protocol.CapSet{protocol.CapIMAP4rev1: {}, protocol.CapIdle: {}}, Logger: log.New(io.Discard, "", 0)})
	go imapServer.Serve(listener)
	defer imapServer.Close()
	dir := t.TempDir()
	ctx := t.Context()
	store, err := storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	vault, err := settings.OpenVault(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := logging.New(&logs)
	manager, err := runtime.New(ctx, store, vault, logger, relayURL)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	configuration, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	manager.AppManagement().Trust = configuration.AppManagementTrust
	auth, grant := testAdministrator(t, store)
	httpServer := httptest.NewTLSServer(New(auth, manager, store, logger))
	defer httpServer.Close()
	call := func(method, path string, body any, want int) []byte {
		t.Helper()
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req, _ := http.NewRequestWithContext(ctx, method, httpServer.URL+"/api/v1"+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		authorizeSession(req, grant)
		response, err := httpServer.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		if response.StatusCode != want {
			t.Fatalf("%s %s: status %d (%s)", method, path, response.StatusCode, data)
		}
		return data
	}
	state := os.Getenv("MAILWAKE_E2E_DEVICE")
	simPath, err := filepath.Abs("../../../relay/scripts/device-sim")
	if err != nil {
		t.Fatal(err)
	}
	sim := func(args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, "node", append([]string{simPath}, args...)...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("device simulator %s: %v (%s)", args[0], err, output)
		}
		return output
	}
	var pairing native.PairingView
	json.Unmarshal(call("POST", "/native/pairings", nil, 201), &pairing)
	uriFile := filepath.Join(dir, "pair-uri.txt")
	if err = os.WriteFile(uriFile, []byte(pairing.URI), 0600); err != nil {
		t.Fatal(err)
	}
	sim("pair", state, uriFile, "E2E phone")
	until := func(timeout time.Duration, f func() bool) {
		t.Helper()
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			if f() {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("condition timed out")
	}
	until(12*time.Second, func() bool {
		var p native.PairingView
		json.Unmarshal(call("GET", "/native/pairings/"+pairing.ID, nil, 200), &p)
		return p.Status == "active"
	})
	t.Log("PASS 4: Core QR → device-sim → Relay claim → active Core pairing")
	var mailbox map[string]any
	json.Unmarshal(call("POST", "/mailboxes", map[string]any{"label": "Work", "host": "127.0.0.1", "port": listener.Addr().(*net.TCPAddr).Port, "username": "test@example.org", "password": "synthetic-password"}, 201), &mailbox)
	mailboxID := mailbox["id"].(string)
	call("PUT", "/mailboxes/"+mailboxID+"/subscriptions", map[string]any{"revision": 1, "folders": []map[string]string{{"name": "Clients", "check": "realtime"}}}, 202)
	until(8*time.Second, func() bool {
		for _, s := range manager.Status() {
			if s.State == "watching" {
				return true
			}
		}
		return false
	})
	call("PUT", "/settings/delivery", map[string]any{"revision": 0, "channel": "native", "native_pairing_id": pairing.ID, "preview": "off", "retry_count": 2, "language": "en"}, 200)
	sim("activity-preferences", state, "true")
	subject := "Encrypted sign-in mail"
	message := "From: Sender <sender@example.org>\r\nTo: test@example.org\r\nSubject: " + subject + "\r\n\r\nYour verification code is 482913\r\n"
	if _, err = user.Append("Clients", strings.NewReader(message), &protocol.AppendOptions{Time: time.Now()}); err != nil {
		t.Fatal(err)
	}
	var eventID string
	until(10*time.Second, func() bool {
		records, err := store.Recent(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range records {
			if !strings.HasPrefix(r.ID, "test_") {
				eventID = r.ID
				return r.State == "accepted"
			}
		}
		return false
	})
	envelopeFile := filepath.Join(dir, "envelope.json")
	until(10*time.Second, func() bool {
		response, err := http.Get(os.Getenv("MAILWAKE_E2E_APNS") + "/" + pairing.ID + "/" + eventID)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode == 404 {
			return false
		}
		if response.StatusCode != 200 {
			t.Fatalf("APNs receiver status: %d", response.StatusCode)
		}
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(envelopeFile, raw, 0600); err != nil {
			t.Fatal(err)
		}
		return true
	})
	plain := sim("decrypt", state, envelopeFile)
	var decoded struct {
		Subject, Account, Folder, Sender, Code string
		Reference                              native.MessageReference `json:"message_ref"`
	}
	if json.Unmarshal(plain, &decoded) != nil || decoded.Subject != subject || decoded.Account != "Work" || decoded.Folder != "Clients" || !strings.Contains(decoded.Sender, "sender@example.org") {
		t.Fatal("decrypted mail fields differ")
	}
	if decoded.Code != "482913" {
		t.Fatal("decrypted verification code differs")
	}
	t.Log("PASS 5: TLS IMAP → Core outbox → Relay → APNs payload; device-sim verified signature and decrypted mail and code 482913")

	var device struct {
		DeviceID    string `json:"device_id"`
		SigningKey  string `json:"signing_key"`
		AccessToken string `json:"access_token"`
	}
	raw, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &device); err != nil {
		t.Fatal(err)
	}
	var invitation appmanagement.Invitation
	if err = json.Unmarshal(call("POST", "/device-invitations", map[string]any{
		"management": true, "scopes": []string{"folders"}, "core_origin": httpServer.URL,
	}, 201), &invitation); err != nil {
		t.Fatal(err)
	}
	inviteURI, err := url.Parse(invitation.URI)
	if err != nil {
		t.Fatal(err)
	}
	appCall := func(method, path string, body any, credential, qualification string, want int) []byte {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(ctx, method, httpServer.URL+"/api/v1/app"+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if credential != "" {
			req.Header.Set("Authorization", "Bearer "+credential)
		}
		if qualification != "" {
			header := "X-Mailwake-App-Qualification"
			if path == "/content" {
				header = "X-Mailwake-Native-Qualification"
			}
			req.Header.Set(header, qualification)
		}
		response, err := httpServer.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("App %s %s: status %d (%s)", method, path, response.StatusCode, data)
		}
		return data
	}
	var credential appmanagement.Credential
	if err = json.Unmarshal(appCall("POST", "/accept", map[string]any{
		"invitation_id": inviteURI.Query().Get("inv"), "token": inviteURI.Query().Get("t"),
		"signing_key": device.SigningKey, "device_name": "E2E phone",
	}, "", "", 200), &credential); err != nil {
		t.Fatal(err)
	}
	coreID, err := native.KeyID(inviteURI.Query().Get("core"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(credential.Credential, "mwac_") || credential.CoreID != coreID || credential.DeviceID != device.DeviceID {
		t.Fatal("App credential identity differs from invitation/device")
	}
	route := "/mailboxes/" + mailboxID + "/folders"
	var denied struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err = json.Unmarshal(appCall("GET", route, nil, credential.Credential, "", 403), &denied); err != nil {
		t.Fatal(err)
	}
	if denied.Error.Code != "app_management_required" {
		t.Fatal("paid route error differs", denied.Error.Code)
	}
	body, err := json.Marshal(map[string]string{"core_id": credential.CoreID})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", platformURL+"/api/v1/app-management/grants", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+device.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var qualification struct {
		Qualification string `json:"qualification"`
	}
	if response.StatusCode != 200 {
		t.Fatalf("Platform grant status %d", response.StatusCode)
	}
	if err = json.NewDecoder(response.Body).Decode(&qualification); err != nil {
		t.Fatal(err)
	}
	folders := appCall("GET", route, nil, credential.Credential, qualification.Qualification, 200)
	if !bytes.Contains(folders, []byte(`"Clients"`)) {
		t.Fatal("paid folder discovery omitted Clients")
	}
	t.Log("PASS 6: owner invitation → mwac_ credential; paid route 403 without qualification, 200 with Platform grant")
	contentInput := map[string]any{"reference": decoded.Reference}
	appCall("POST", "/content", contentInput, credential.Credential, "", 403)
	var contentInvitation appmanagement.Invitation
	json.Unmarshal(call("POST", "/device-invitations", map[string]any{"management": true, "scopes": []string{"content"}, "core_origin": httpServer.URL}, 201), &contentInvitation)
	contentURI, _ := url.Parse(contentInvitation.URI)
	json.Unmarshal(appCall("POST", "/accept", map[string]any{"invitation_id": contentURI.Query().Get("inv"), "token": contentURI.Query().Get("t"), "signing_key": device.SigningKey, "device_name": "Content phone"}, "", "", 200), &credential)
	appCall("POST", "/content", contentInput, credential.Credential, qualification.Qualification, 403)
	req, _ = http.NewRequestWithContext(ctx, "POST", platformURL+"/api/v1/native-push/grants", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+device.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		t.Fatal("NativePush qualification failed", response.StatusCode)
	}
	json.NewDecoder(response.Body).Decode(&qualification)
	response.Body.Close()
	contentResponse := appCall("POST", "/content", contentInput, credential.Credential, qualification.Qualification, 200)
	if !bytes.Contains(contentResponse, []byte("482913")) {
		t.Fatal("original content missing")
	}
	decoded.Reference.UID++
	appCall("POST", "/content", map[string]any{"reference": decoded.Reference}, credential.Credential, qualification.Qualification, 404)
	t.Log("PASS content: explicit owner scope + NativePush qualification, original IMAP text, wrong scope/purpose/tampering rejected")
	activityFile := filepath.Join(dir, "activity.json")
	until(15*time.Second, func() bool {
		_ = manager.Native().PollDeliveries(ctx)
		response, err := http.Get(os.Getenv("MAILWAKE_E2E_APNS") + "/activity/" + pairing.ID + "/" + eventID)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode == 404 {
			return false
		}
		data, _ := io.ReadAll(response.Body)
		var captured struct {
			Fixture struct{ Type, Topic string }
			APS     struct{ Event string }
		}
		if json.Unmarshal(data, &captured) != nil || captured.Fixture.Type != "liveactivity" || !strings.HasSuffix(captured.Fixture.Topic, ".push-type.liveactivity") || captured.APS.Event != "start" {
			t.Fatal("ActivityKit APNs protocol differs")
		}
		if err := os.WriteFile(activityFile, data, 0600); err != nil {
			t.Fatal(err)
		}
		return true
	})
	var activityPlain struct {
		ActivityID   string `json:"activity_id"`
		Notification struct{ Code string }
	}
	if json.Unmarshal(sim("decrypt", state, activityFile), &activityPlain) != nil || activityPlain.ActivityID != eventID || activityPlain.Notification.Code != "482913" {
		t.Fatal("activity decryption/identity failed")
	}
	sim("activity-token", state, pairing.ID, eventID)
	sim("activity-preferences", state, "false")
	until(10*time.Second, func() bool {
		response, err := http.Get(os.Getenv("MAILWAKE_E2E_APNS") + "/end/" + strings.Repeat("ef", 32))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		return response.StatusCode == 200
	})
	t.Log("PASS activity: independent signed ciphertext, liveactivity topic/start, token registration and remote end")

	result, err := json.Marshal(map[string]string{"pairing_id": pairing.ID, "event_id": eventID})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("MAILWAKE_E2E_RESULT"), result, 0600); err != nil {
		t.Fatal(err)
	}
	manager.Close()
	uri, _ := url.Parse(pairing.URI)
	for _, secret := range []string{uri.Query().Get("t"), uri.Query().Get("core"), subject, "Your verification code is 482913", "482913"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("private data appeared in operational logs")
		}
	}
	t.Log("Privacy: message content and pairing secrets stay out of operational logs")
}

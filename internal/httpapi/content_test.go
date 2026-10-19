package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mingzaily/mailwake/internal/content"
	"github.com/mingzaily/mailwake/internal/coreidentity"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
)

type mimeBodyRuntime struct {
	Runtime
	raw string
}

func (r mimeBodyRuntime) ReadContent(ctx context.Context, _ coreidentity.MessageReference) (mail.Body, error) {
	return content.ReadBody(ctx, strings.NewReader(r.raw))
}

type rejectContentTransport struct{ requests *atomic.Int32 }

func (r rejectContentTransport) RoundTrip(*http.Request) (*http.Response, error) {
	r.requests.Add(1)
	return nil, errors.New("unexpected remote request")
}

func TestContentResponsePreservesImagesWithoutHTTPOrDNSRequests(t *testing.T) {
	var received, attempts, lookups atomic.Int32
	images := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Add(1); w.WriteHeader(204) }))
	defer images.Close()
	// Count both real local image traffic and attempts through the standard HTTP/DNS paths.
	oldTransport, oldResolver := http.DefaultTransport, net.DefaultResolver
	http.DefaultTransport = rejectContentTransport{&attempts}
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		lookups.Add(1)
		return nil, errors.New("unexpected DNS lookup")
	}}
	t.Cleanup(func() { http.DefaultTransport = oldTransport; net.DefaultResolver = oldResolver })
	raw := "Content-Type: text/html; charset=utf-8\r\n\r\n" + `<p>Transient body</p><img src="` + images.URL + `/src"><picture><source srcset="` + images.URL + `/small 1x, https://content-image.invalid/large 2x"><img alt="alternate"></picture><div style="background-image:url(` + images.URL + `/background)">layout</div>`
	f := newAppFixture(t, mimeBodyRuntime{Runtime: newTestRuntime("bark", nil, nil), raw: raw})
	credential, _ := f.invite(t, []string{"content"})
	headers := map[string]string{"Authorization": "Bearer " + credential}
	w := f.do(t, "POST", "/api/v1/app/content", map[string]any{"reference": map[string]any{}}, headers)
	var body mail.Body
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || !body.HasRemoteImages || !strings.Contains(body.HTML, images.URL) || !strings.Contains(body.Text, "Transient body") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response %d %s", w.Code, w.Body.String())
	}
	if received.Load() != 0 || attempts.Load() != 0 || lookups.Load() != 0 {
		t.Fatalf("network: received=%d attempted=%d DNS=%d", received.Load(), attempts.Load(), lookups.Load())
	}
	// No Platform service or Pro configuration is present in this fixture.
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

func TestContentResponseJSONBudgetIncludesEscaping(t *testing.T) {
	raw := "Content-Type: multipart/alternative; boundary=a\r\n\r\n--a\r\nContent-Type: text/plain\r\n\r\n" + strings.Repeat("<", content.MaxTextBytes) + "\r\n--a\r\nContent-Type: text/html\r\n\r\n<p>" + strings.Repeat("&lt;", 40000) + "</p>\r\n--a--"
	f := newAppFixture(t, mimeBodyRuntime{Runtime: newTestRuntime("bark", nil, nil), raw: raw})
	credential, _ := f.invite(t, []string{"content"})
	w := f.do(t, "POST", "/api/v1/app/content", map[string]any{"reference": map[string]any{}}, map[string]string{"Authorization": "Bearer " + credential})
	var body mail.Body
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || w.Body.Len() > content.MaxResponseBytes || body.HTML != "" || body.HasRemoteImages || !body.Truncated || len(body.Text) != content.MaxTextBytes {
		t.Fatalf("response status=%d bytes=%d", w.Code, w.Body.Len())
	}
}

type readActionRuntime struct {
	Runtime
	mark func(coreidentity.MessageReference) error
}

func (r readActionRuntime) MarkRead(_ context.Context, ref coreidentity.MessageReference) error {
	return r.mark(ref)
}

func TestReadActionForwardsReferenceAndReportsFailures(t *testing.T) {
	reference := coreidentity.MessageReference{CoreID: "core", MailboxID: "mailbox", AccountID: "account", Folder: "INBOX", Location: mail.Location{UIDValidity: 7, UID: 9}, Signature: "signature"}
	var marked bool
	var failure error
	runtime := readActionRuntime{Runtime: newTestRuntime("bark", nil, nil), mark: func(ref coreidentity.MessageReference) error {
		if ref != reference {
			t.Errorf("reference differs: %+v", ref)
		}
		if failure != nil {
			return failure
		}
		marked = true
		return nil
	}}
	f := newAppFixture(t, runtime)
	credential, _ := f.invite(t, []string{"content"})
	headers := map[string]string{"Authorization": "Bearer " + credential}
	body := map[string]any{"reference": reference}
	w := f.do(t, "POST", appPrefix+"/content/read", body, headers)
	if w.Code != 204 || !marked || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("read action: %d %s", w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		code   string
		status int
	}{{"message_not_found", 404}, {"message_unavailable", 503}, {"message_timeout", 504}} {
		marked = false
		failure = fault.New(tc.code)
		w = f.do(t, "POST", appPrefix+"/content/read", body, headers)
		if w.Code != tc.status || marked || !strings.Contains(w.Body.String(), tc.code) {
			t.Fatalf("failure: %d %s", w.Code, w.Body.String())
		}
	}
	f.do(t, "DELETE", appPrefix+"/controller", nil, headers)
	if w := f.do(t, "POST", appPrefix+"/content/read", body, headers); w.Code != 401 {
		t.Fatal(w.Code)
	}
}

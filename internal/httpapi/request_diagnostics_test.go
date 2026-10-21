package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/coreidentity"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/logging"
)

type diagnosedReadRuntime struct {
	Runtime
	code, outcome string
}

func (r diagnosedReadRuntime) MarkRead(ctx context.Context, _ coreidentity.MessageReference) error {
	logging.RequestStage(ctx, "verify_seen")
	if r.code != "" {
		logging.RequestResponseCode(ctx, "NOPERM")
		logging.RequestCause(ctx, "imap_folder_rejected")
		return fault.New(r.code)
	}
	logging.RequestOutcome(ctx, r.outcome)
	return nil
}

func TestAppRequestDiagnosticsExportStagesWithoutPrivateData(t *testing.T) {
	for _, tc := range []struct {
		code, outcome string
		status        int
	}{
		{"", "marked_read", 204}, {"", "already_read", 204}, {"message_not_found", "failed", 404}, {"message_unavailable", "failed", 503}, {"message_timeout", "failed", 504},
	} {
		t.Run(tc.code+tc.outcome, func(t *testing.T) {
			f := newAppFixture(t, diagnosedReadRuntime{newTestRuntime("", nil, nil), tc.code, tc.outcome})
			credential, _ := f.invite(t, []string{"content", "diagnostics"})
			secret := "private-signed-reference"
			ref := coreidentity.MessageReference{CoreID: "private-core", MailboxID: "private-mailbox", AccountID: "private-account", Folder: "private-folder", Signature: secret}
			headers := map[string]string{"Authorization": "Bearer " + credential, "X-Request-ID": "private-client-id"}
			w := f.do(t, "POST", appPrefix+"/content/read?secret=private-query", map[string]any{"reference": ref}, headers)
			requestID := w.Header().Get("X-Request-ID")
			if w.Code != tc.status || len(requestID) != 32 || requestID == "private-client-id" {
				t.Fatalf("response %d %s", w.Code, requestID)
			}
			// Ordinary logs may contain authenticated-view identities; export only the request projection.
			f.log.Info("private-log-message", "folder", "private-folder", "mailbox_id", "private-mailbox")
			for _, route := range []string{"/api/v1/diagnostics", appPrefix + "/diagnostics"} {
				exportHeaders := headers
				if route == "/api/v1/diagnostics" {
					exportHeaders = nil
				}
				report := f.do(t, "GET", route, nil, exportHeaders)
				var data struct {
					Schema   int                  `json:"schema_version"`
					Requests []logging.AppRequest `json:"app_requests"`
				}
				if report.Code != 200 || json.Unmarshal(report.Body.Bytes(), &data) != nil || data.Schema != 2 {
					t.Fatal(report.Code, report.Body.String())
				}
				found := false
				for _, request := range data.Requests {
					if request.RequestID == requestID {
						found = true
						if request.Status != int64(tc.status) || request.Code != tc.code || request.Stage != "verify_seen" || request.Outcome != tc.outcome || request.Route != appPrefix+"/content/read" || request.Method != "POST" || request.DurationMS < 0 {
							t.Fatalf("request: %+v", request)
						}
						if tc.code != "" && (request.ResponseCode != "NOPERM" || request.CauseCode != "imap_folder_rejected") {
							t.Fatal(request)
						}
					}
				}
				if !found {
					t.Fatal("read request missing from export")
				}
				for _, value := range []string{secret, credential, "private-core", "private-mailbox", "private-account", "private-folder", "private-query", "private-client-id", "private-log-message"} {
					if strings.Contains(report.Body.String(), value) {
						t.Fatalf("diagnostic export contains private value %q", value)
					}
				}
			}
		})
	}
}

func TestAppRequestDiagnosticsIncludeAuthenticationAndValidationFailures(t *testing.T) {
	f := newAppFixture(t)
	limited, _ := f.invite(t, []string{"mailboxes"})
	content, _ := f.invite(t, []string{"content"})
	for _, tc := range []struct {
		credential, body, stage, code string
		status                        int
	}{
		{"invalid", "{}", "authentication", "unauthorized", 401},
		{limited, "{}", "authorization", "content_permission_required", 403},
		{content, "{", "handler", "request_invalid", 400},
	} {
		req := httptest.NewRequest("POST", appPrefix+"/content/read", strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.credential)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, req)
		entries := logging.AppRequests(f.log)
		last := entries[len(entries)-1]
		if w.Code != tc.status || last.Status != int64(tc.status) || last.Stage != tc.stage || last.Code != tc.code || last.RequestID != w.Header().Get("X-Request-ID") {
			t.Fatalf("status=%d request=%+v", w.Code, last)
		}
	}
	// Exports retain the existing owner/session and App diagnostics scope boundary.
	for _, headers := range []map[string]string{{"Authorization": "Bearer invalid"}, {"Authorization": "Bearer " + content}} {
		w := f.do(t, "GET", appPrefix+"/diagnostics", nil, headers)
		if w.Code != 401 && w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	f.do(t, "GET", appPrefix+"/mailboxes/private-resource", nil, map[string]string{"Authorization": "Bearer " + limited})
	entries := logging.AppRequests(f.log)
	if entries[len(entries)-1].Route != appPrefix+"/mailboxes/:id" {
		t.Fatal(entries[len(entries)-1])
	}
}

func TestAppRoutingFailuresRemainDiagnosableWithoutRawURLs(t *testing.T) {
	f := newAppFixture(t)
	for _, tc := range []struct {
		method, path, code string
		status             int
	}{
		{"POST", appPrefix + "/private-unknown-route?token=private-query", "route_not_found", 404},
		{"GET", appPrefix + "/content/read", "method_not_allowed", 405},
		{"PRIVATE-METHOD", appPrefix + "/private-route", "route_not_found", 404},
	} {
		w := f.do(t, tc.method, tc.path, nil, map[string]string{})
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
		requests := logging.AppRequests(f.log)
		if len(requests) == 0 {
			t.Fatal("routing failure missing from diagnostics")
		}
		last := requests[len(requests)-1]
		if last.Code != tc.code || last.Status != int64(tc.status) || last.RequestID != w.Header().Get("X-Request-ID") || last.Route != appPrefix+"/*unmatched" {
			t.Fatalf("request %+v", last)
		}
		if tc.method == "PRIVATE-METHOD" && last.Method != "OTHER" {
			t.Fatal(last)
		}
		raw, _ := json.Marshal(requests)
		if strings.Contains(string(raw), "private-") || strings.Contains(string(raw), "PRIVATE-METHOD") {
			t.Fatal("raw request metadata leaked")
		}
	}
}

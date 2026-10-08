package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/engine"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/storage"
)

type diagnosticRuntime struct {
	testRuntime
	statuses []engine.FolderStatus
}

func (r diagnosticRuntime) Status() []engine.FolderStatus { return r.statuses }

func TestIMAPResponseCodeInStatusAndDiagnostics(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	auth, grant := testAdministrator(t, store)
	for _, code := range []string{"UNAVAILABLE", "NONEXISTENT", ""} {
		errorCode := "imap_folder_open_failed"
		params := map[string]string{"private_detail": "sensitive server text"}
		if code != "" {
			errorCode = "imap_folder_rejected"
			params["response_code"] = code
		}
		runtime := diagnosticRuntime{statuses: []engine.FolderStatus{
			{Folder: "private-folder", State: "reconnecting", LastError: &fault.Error{Code: errorCode, Params: params}},
			{Folder: "other-private-folder", State: "reconnecting", LastError: &fault.Error{Code: "request_failed", Params: map[string]string{"response_code": "unrelated-private-value"}}},
		}}
		router := New(auth, runtime, store, slog.Default())
		for _, locale := range []string{"en", "zh-CN"} {
			for _, route := range []string{"status", "diagnostics"} {
				t.Run(code+"/"+locale+"/"+route, func(t *testing.T) {
					req := httptest.NewRequest("GET", "/api/v1/"+route, nil)
					req.Header.Set("Accept-Language", locale)
					authorizeSession(req, grant)
					w := httptest.NewRecorder()
					router.ServeHTTP(w, req)
					if w.Code != 200 {
						t.Fatal(w.Code, w.Body.String())
					}
					var data struct {
						Folders []struct {
							LastError localizedError `json:"last_error"`
						} `json:"folders"`
						Watches []map[string]any `json:"watches"`
					}
					if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
						t.Fatal(err)
					}
					if route == "status" {
						failure := data.Folders[0].LastError
						if failure.Code != errorCode || failure.Params["response_code"] != code {
							t.Fatal(failure)
						}
						want := "Failed to open the IMAP folder"
						if locale == "zh-CN" {
							want = "IMAP 文件夹打开失败"
						}
						if !strings.Contains(failure.Message, want) || (code != "" && !strings.Contains(failure.Message, code)) || strings.Contains(failure.Message, "{response_code}") {
							t.Fatal(failure.Message)
						}
					} else {
						item := data.Watches[0]
						if item["error_code"] != errorCode {
							t.Fatal(item)
						}
						if code != "" && item["response_code"] != code {
							t.Fatal(item)
						}
						if code == "" && item["response_code"] != nil {
							t.Fatal(item)
						}
						if data.Watches[1]["response_code"] != nil {
							t.Fatal("unrelated response code exported")
						}
						for _, private := range []string{"private", "sensitive server text"} {
							if strings.Contains(w.Body.String(), private) {
								t.Fatalf("diagnostics leaked: %s", w.Body.String())
							}
						}
					}
				})
			}
		}
	}
}

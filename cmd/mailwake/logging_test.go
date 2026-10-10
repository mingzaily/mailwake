package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/logging"
)

func TestSetupCodeOnlyGoesToConsole(t *testing.T) {
	var console bytes.Buffer
	log := logging.New(&console)
	writeSetupCode(&console, "TEST-SETUP-CODE")
	if !strings.Contains(console.String(), "TEST-SETUP-CODE") || len(logging.Read(log, logging.Query{}).Entries) != 0 {
		t.Fatal("setup code entered log buffer or was hidden from console")
	}
}

func TestStartupFailureReportsSafeActionableMessage(t *testing.T) {
	for _, tc := range []struct {
		err           error
		code, message string
	}{
		{fault.New("data_directory_permission_denied"), "data_directory_permission_denied", "10001:10001"},
		{fault.New("data_directory_read_only"), "data_directory_read_only", "write access"},
		{fault.New("storage_open_failed"), "storage_open_failed", "database integrity"},
		{fault.New("http_listen_failed"), "http_listen_failed", "MAILWAKE_LISTEN"},
		{errors.New("private-path-and-secret"), "core_startup_failed", "process configuration"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			var console bytes.Buffer
			logStartupFailure(logging.New(&console), tc.err)
			var entry map[string]any
			if err := json.Unmarshal(console.Bytes(), &entry); err != nil {
				t.Fatal(err)
			}
			if entry["code"] != tc.code || !strings.Contains(entry["msg"].(string), tc.message) {
				t.Fatalf("unexpected log: %s", console.String())
			}
			if strings.Contains(console.String(), "private-path-and-secret") {
				t.Fatal("startup log exposed raw error")
			}
		})
	}
}

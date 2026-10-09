package main

import (
	"bytes"
	"strings"
	"testing"

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

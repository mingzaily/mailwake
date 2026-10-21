package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestAppRequestExportIsBoundedClearedAndPrivate(t *testing.T) {
	var output bytes.Buffer
	log := New(&output)
	ctx, trace := WithRequestTrace(t.Context())
	RequestResponseCode(ctx, "PRIVATE-PROVIDER-DATA")
	for i := 0; i < AppRequestLimit+5; i++ {
		log.Info(AppRequestMessage, "request_id", fmt.Sprint(i), "route", "/api/v1/app/content/read", "http_status", 204, "response_code", trace.ResponseCode, "body", "private-body", "reference", "private-reference", "credential", "private-credential", "url", "private-url", "password", "private-password")
	}
	entries := AppRequests(log)
	if len(entries) != AppRequestLimit || entries[0].RequestID != "5" || entries[len(entries)-1].RequestID != "304" || entries[0].ResponseCode != "OTHER" {
		t.Fatal(entries)
	}
	exported, _ := json.Marshal(entries)
	ring, _ := json.Marshal(Read(log, Query{Limit: 500}))
	for _, data := range []string{output.String(), string(exported), string(ring)} {
		if strings.Contains(data, "private-") || strings.Contains(data, "PRIVATE-PROVIDER-DATA") {
			t.Fatal("private value retained")
		}
	}
	Clear(log)
	if len(AppRequests(log)) != 0 || len(AppRequests(New(io.Discard))) != 0 {
		t.Fatal("clear/restart retained requests")
	}
}

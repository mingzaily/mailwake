package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestDeliveryMessageContract(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Enqueue(t.Context(), event.Notification{ID: "message-contract", MailboxID: "mbx_work", Account: "工作邮箱", Folder: "Clients", Subject: "季度报告已更新", Sender: "private@example.org"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Accepted(t.Context(), "message-contract"); err != nil {
		t.Fatal(err)
	}
	service, grant := testAdministrator(t, store)
	router := New(service, newTestRuntime("", nil, nil), store, nil)
	req := httptest.NewRequest("GET", "/api/v1/deliveries", nil)
	authorizeSession(req, grant)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var actual, expected struct {
		Deliveries []map[string]any `json:"deliveries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &actual); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("testdata/delivery-message.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fixture, &expected); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(actual.Deliveries) != 1 || !reflect.DeepEqual(actual.Deliveries[0]["message"], expected.Deliveries[0]["message"]) {
		t.Fatalf("delivery message contract: %d %s", w.Code, w.Body)
	}
}

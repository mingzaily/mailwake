package settings

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPublicViewsPreserveWireFieldsAndRedactSecrets(t *testing.T) {
	tests := []struct {
		name string
		view any
		want string
	}{
		{"mailbox", (Mailbox{ID: "box", Label: "Work", Revision: 2, Host: "imap.example.test", Port: 993, Username: "owner", Password: "private-password"}).View(1), `{"id":"box","label":"Work","revision":2,"connection_limit":10,"connections_in_use":1,"host":"imap.example.test","port":993,"username":"owner","password":{"configured":true}}`},
		{"delivery", (Delivery{NativePairingID: "phone", Revision: 3, Channel: "webhook", Preview: "off", RetryCount: 2, Language: "en", Bark: Bark{Endpoint: "https://api.day.app", Key: "private-bark"}, Pushover: Pushover{Token: "private-token"}, Webhook: Webhook{URL: "https://private.example.test/secret", Secret: "private-secret"}}).View(), `{"native_pairing_id":"phone","native_available":false,"revision":3,"channel":"webhook","preview":"off","retry_count":2,"language":"en","bark":{"endpoint":"https://api.day.app","key":{"configured":true}},"pushover":{"token":{"configured":true},"user":{"configured":false}},"webhook":{"url":{"configured":true},"secret":{"configured":true}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.view)
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tt.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("wire contract: got %s; want %s", raw, tt.want)
			}
		})
	}
}

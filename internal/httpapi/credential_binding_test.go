package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/settings"
)

type boundCredentialRuntime struct {
	Runtime
	mailbox  settings.Mailbox
	delivery settings.Delivery
}

func (r boundCredentialRuntime) TestMailbox(_ context.Context, _ string, input settings.MailboxUpdate) (*mail.FolderDiscovery, error) {
	_, err := r.mailbox.Merge(input)
	return nil, err
}
func (r boundCredentialRuntime) UpdateMailbox(_ context.Context, _ string, input settings.MailboxUpdate) error {
	_, err := r.mailbox.Merge(input)
	return err
}
func (r boundCredentialRuntime) TestDelivery(_ context.Context, input settings.DeliveryUpdate) error {
	_, err := r.delivery.Merge(input)
	return err
}
func (r boundCredentialRuntime) UpdateDelivery(_ context.Context, input settings.DeliveryUpdate) error {
	_, err := r.delivery.Merge(input)
	return err
}

func TestCredentialTargetChangesHaveSameWebAndAppContract(t *testing.T) {
	runtime := boundCredentialRuntime{Runtime: newTestRuntime("", nil, nil), mailbox: settings.Mailbox{ID: "box", Label: "Mail", Host: "original.example.test", Port: 993, Username: "user", Password: "saved-mailbox-password"}, delivery: settings.Delivery{Channel: "bark", Preview: "off", Language: "en", Bark: settings.Bark{Endpoint: "https://original.example.test", Key: "saved-bark-key"}}}
	f := newAppFixture(t, runtime)
	credential, _ := f.invite(t, []string{"mailboxes", "channels"})
	for _, tc := range []struct {
		method, path, code string
		body               map[string]any
	}{
		{"POST", "/mailboxes/box/test", "mailbox_password_required_for_change", map[string]any{"host": "other.example.test", "port": 993, "username": "user"}},
		{"PUT", "/mailboxes/box", "mailbox_password_required_for_change", map[string]any{"revision": 1, "host": "other.example.test", "port": 993, "username": "user", "password": nil}},
		{"POST", "/settings/delivery/test", "bark_key_required_for_change", map[string]any{"channel": "bark", "preview": "off", "bark": map[string]any{"endpoint": "https://other.example.test"}}},
		{"PUT", "/settings/delivery", "bark_key_required_for_change", map[string]any{"revision": 1, "channel": "bark", "preview": "off", "bark": map[string]any{"endpoint": "https://other.example.test", "key": nil}}},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			for _, app := range []bool{false, true} {
				prefix := "/api/v1"
				var headers map[string]string
				wantMessage := "Enter"
				if app {
					prefix += "/app"
					headers = map[string]string{"Authorization": "Bearer " + credential, "Accept-Language": "zh-CN"}
					wantMessage = "重新输入"
				}
				w := f.do(t, tc.method, prefix+tc.path, tc.body, headers)
				var response struct {
					Error struct{ Code, Message string }
				}
				if w.Code != 400 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Error.Code != tc.code || !strings.Contains(response.Error.Message, wantMessage) {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
				for _, secret := range []string{runtime.mailbox.Password, runtime.delivery.Bark.Key} {
					if strings.Contains(w.Body.String(), secret) {
						t.Fatal("response exposed credential")
					}
				}
			}
		})
	}
}

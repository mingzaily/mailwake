package runtime

import (
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/settings"
)

func TestSenderFactoryInjectsOnlyDeliveryRoots(t *testing.T) {
	for _, channel := range []string{"webhook", "bark"} {
		t.Run(channel, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if channel == "bark" {
					w.Write([]byte(`{"code":200}`))
				} else {
					w.WriteHeader(204)
				}
			}))
			defer server.Close()
			config := settings.Delivery{Channel: channel, Language: "en", Bark: settings.Bark{Endpoint: server.URL, Key: "fixture-key"}, Webhook: settings.Webhook{URL: server.URL, Secret: "test-fixture-signing-secret-only-32"}}
			n := event.Notification{ID: "fixture-event", Test: true}
			if err := sender(config).Send(t.Context(), n); err == nil {
				t.Fatal("default factory trusted fixture")
			}
			if err := senderWithOptions(config, x509.NewCertPool()).Send(t.Context(), n); err == nil {
				t.Fatal("factory accepted unrelated pool")
			}
			pool := x509.NewCertPool()
			pool.AddCert(server.Certificate())
			if err := senderWithOptions(config, pool).Send(t.Context(), n); err != nil {
				t.Fatal(err)
			}
		})
	}
}

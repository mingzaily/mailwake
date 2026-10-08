package delivery_test

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/delivery/bark"
	"github.com/mingzaily/mailwake/internal/delivery/pushover"
	"github.com/mingzaily/mailwake/internal/delivery/webhook"
	"github.com/mingzaily/mailwake/internal/event"
)

func TestLocalDeliveryRootsVerifyTargetAndKeepSystemTrust(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	if _, err := delivery.NewHTTPClient().Get(server.URL); err == nil {
		t.Fatal("default client trusted private fixture")
	}
	client := delivery.NewHTTPClientWithLocalTestRoots(server.URL, pool)
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	transport := client.Transport.(*http.Transport)
	u, _ := url.Parse(server.URL)
	if transport.TLSClientConfig.ServerName != u.Hostname() || transport.TLSClientConfig.MinVersion != tls.VersionTLS12 || transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("TLS target verification changed")
	}
	_, port, _ := net.SplitHostPort(u.Host)
	wrongName := "https://localhost:" + port
	if _, err = delivery.NewHTTPClientWithLocalTestRoots(wrongName, pool).Get(wrongName); err == nil {
		t.Fatal("client trusted wrong hostname")
	}
	for _, endpoint := range []string{"https://api.pushover.net/1/messages.json", "https://webhook.example.org", "https://localhost.example.org", "https://192.168.1.1", "http://127.0.0.1", ":invalid"} {
		if client = delivery.NewHTTPClientWithLocalTestRoots(endpoint, pool); client.Transport != nil {
			t.Fatal("test pool applied outside HTTPS test targets", endpoint)
		}
	}
	for _, endpoint := range []string{server.URL, "https://localhost", "https://hooks.stage35.test", "https://[::1]"} {
		client = delivery.NewHTTPClientWithLocalTestRoots(endpoint, pool)
		if client.Transport == nil || client.Timeout != 15*time.Second {
			t.Fatal("test target missing isolated transport", endpoint)
		}
	}
	if delivery.NewHTTPClientWithLocalTestRoots(server.URL, nil).Transport != nil {
		t.Fatal("default transport changed without explicit roots")
	}
}

func TestLocalDeliveryRootAdaptersAndRedirectBoundary(t *testing.T) {
	for _, channel := range []string{"webhook", "bark", "pushover"} {
		t.Run(channel, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch channel {
				case "webhook":
					w.WriteHeader(204)
				case "bark":
					w.Write([]byte(`{"code":200}`))
				case "pushover":
					w.Write([]byte(`{"status":1}`))
				}
			}))
			defer server.Close()
			pool := x509.NewCertPool()
			pool.AddCert(server.Certificate())
			client := delivery.NewHTTPClientWithLocalTestRoots(server.URL, pool)
			var sender delivery.Sender
			switch channel {
			case "webhook":
				s := webhook.New(server.URL, "test-fixture-signing-secret-only-32", "en")
				s.SetHTTPClient(client)
				sender = s
			case "bark":
				s := bark.New(server.URL, "fixture-key", "en")
				s.SetHTTPClient(client)
				sender = s
			case "pushover":
				s := pushover.New(server.URL, "fixture-token", "fixture-user", "en")
				s.SetHTTPClient(client)
				sender = s
			}
			if err := sender.Send(t.Context(), event.Notification{ID: "fixture-event", Test: true}); err != nil {
				t.Fatal(err)
			}
		})
	}
	var forwarded atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	pool := x509.NewCertPool()
	pool.AddCert(origin.Certificate())
	resp, err := delivery.NewHTTPClientWithLocalTestRoots(origin.URL, pool).Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect || forwarded.Load() != 0 {
		t.Fatal("redirect forwarded request")
	}
}

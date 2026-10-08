// Package pushover delivers notifications through the Pushover message API.
package pushover

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/event"
)

// Endpoint is the Pushover message API.
const Endpoint = "https://api.pushover.net/1/messages.json"

type Sender struct {
	endpoint, token, user, language string
	client                          *http.Client
}

// New takes the application token and the user or group key.
func New(endpoint, token, user, language string) *Sender {
	return &Sender{endpoint: endpoint, token: token, user: user, language: language, client: delivery.NewHTTPClient()}
}

func (s *Sender) Send(ctx context.Context, n event.Notification) error {
	m := delivery.Render(s.language, n)
	form := url.Values{"token": {s.token}, "user": {s.user}, "title": {m.Title}, "message": {m.Body}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return &delivery.Failure{Code: "pushover_configuration_invalid"}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set(delivery.HeaderEvent, n.ID)
	resp, err := s.client.Do(req)
	if err != nil {
		return &delivery.Failure{Code: "pushover_network_error", Retryable: true}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return delivery.HTTPFailure("pushover", resp)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8193))
	if err != nil || len(b) > 8192 {
		return &delivery.Failure{HTTPStatus: resp.StatusCode, Code: "pushover_response_invalid", Retryable: true}
	}
	var result struct {
		Status int `json:"status"`
	}
	if json.Unmarshal(b, &result) != nil || result.Status != 1 {
		return &delivery.Failure{HTTPStatus: resp.StatusCode, Code: "pushover_response_invalid", Retryable: true}
	}
	return nil
}

func (s *Sender) Channel() string { return "pushover" }

func (s *Sender) SetHTTPClient(client *http.Client) { s.client = client }

package bark

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/event"
)

type Sender struct {
	endpoint, key, language string
	client                  *http.Client
}

func New(endpoint, key, language string) *Sender {
	return &Sender{endpoint: strings.TrimRight(endpoint, "/") + "/push", key: key, language: language, client: delivery.NewHTTPClient()}
}

func (s *Sender) Send(ctx context.Context, n event.Notification) error {
	m := delivery.Render(s.language, n)
	payload := struct {
		Key   string `json:"device_key"`
		Title string `json:"title"`
		Body  string `json:"body"`
		Group string `json:"group"`
	}{s.key, m.Title, m.Body, m.Title}
	data, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(data))
	if err != nil {
		return &delivery.Failure{Code: "bark_configuration_invalid"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(delivery.HeaderEvent, n.ID)
	resp, err := s.client.Do(req)
	if err != nil {
		return &delivery.Failure{Code: "bark_network_error", Retryable: true}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return delivery.HTTPFailure("bark", resp)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8193))
	if err != nil || len(b) > 8192 {
		return &delivery.Failure{HTTPStatus: resp.StatusCode, Code: "bark_response_invalid", Retryable: true}
	}
	var result struct {
		Code int `json:"code"`
	}
	if json.Unmarshal(b, &result) != nil || result.Code == 0 {
		return &delivery.Failure{HTTPStatus: resp.StatusCode, Code: "bark_response_invalid", Retryable: true}
	}
	if result.Code != 200 {
		return &delivery.Failure{HTTPStatus: resp.StatusCode, Code: "bark_provider_error", Params: map[string]string{"status": strconv.Itoa(result.Code)}, Retryable: result.Code == 429 || result.Code >= 500, RetryAfter: delivery.ParseRetryAfter(resp.Header.Get("Retry-After"))}
	}
	return nil
}

func (s *Sender) Channel() string { return "bark" }

// SetHTTPClient installs the sender transport before Send.
func (s *Sender) SetHTTPClient(client *http.Client) { s.client = client }

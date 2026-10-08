// Package webhook posts signed notification events to a user-provided HTTPS endpoint.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/event"
)

// Payload is version 1 of the webhook body. Title and message are ready to display;
// the structured fields let receivers build their own presentation.
type Payload struct {
	Version    int       `json:"version"`
	ID         string    `json:"id"`
	Test       bool      `json:"test"`
	MailboxID  string    `json:"mailbox_id,omitempty"`
	Account    string    `json:"account,omitempty"`
	Folder     string    `json:"folder,omitempty"`
	Sender     string    `json:"sender,omitempty"`
	Subject    string    `json:"subject,omitempty"`
	ReceivedAt time.Time `json:"received_at"`
	Title      string    `json:"title"`
	Message    string    `json:"message"`
}

const (
	HeaderEvent     = delivery.HeaderEvent
	HeaderTimestamp = "X-Mailwake-Timestamp"
	HeaderSignature = "X-Mailwake-Signature"
)

type Sender struct {
	url, secret, language string
	client                *http.Client
	now                   func() time.Time
}

func New(url, secret, language string) *Sender {
	return &Sender{url: url, secret: secret, language: language, client: delivery.NewHTTPClient(), now: time.Now}
}

// Sign returns the signature header value for a timestamp and raw body:
// "sha256=" followed by hex HMAC-SHA256 of "<timestamp>.<body>".
func Sign(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (s *Sender) Send(ctx context.Context, n event.Notification) error {
	m := delivery.Render(s.language, n)
	body, _ := json.Marshal(Payload{
		Version: 1, ID: n.ID, Test: n.Test, Account: n.Account, MailboxID: n.MailboxID, Folder: n.Folder,
		Sender: delivery.Clip(n.Sender, 200), Subject: delivery.Clip(n.Subject, 600),
		ReceivedAt: n.ReceivedAt, Title: m.Title, Message: m.Body,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return &delivery.Failure{Code: "webhook_configuration_invalid"}
	}
	timestamp := strconv.FormatInt(s.now().Unix(), 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mailwake-Core")
	req.Header.Set(HeaderEvent, n.ID)
	req.Header.Set(HeaderTimestamp, timestamp)
	req.Header.Set(HeaderSignature, Sign(s.secret, timestamp, body))
	resp, err := s.client.Do(req)
	if err != nil {
		return &delivery.Failure{Code: "webhook_network_error", Retryable: true}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return delivery.HTTPFailure("webhook", resp)
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
	return nil
}

func (s *Sender) Channel() string { return "webhook" }

func (s *Sender) SetHTTPClient(client *http.Client) { s.client = client }

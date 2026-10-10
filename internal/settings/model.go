package settings

import (
	"crypto/rand"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/i18n"
	"github.com/mingzaily/mailwake/internal/mail"
)

type Mailbox struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	Revision        int64  `json:"-"`
	ConnectionLimit int    `json:"connection_limit"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Username        string `json:"username"`
	Password        string `json:"password"`
}
type MailboxUpdate struct {
	Revision        int64   `json:"revision"`
	Label           *string `json:"label"`
	ConnectionLimit *int    `json:"connection_limit"`
	Host            string  `json:"host"`
	Port            int     `json:"port"`
	Username        string  `json:"username"`
	Password        *string `json:"password"`
}

func (m Mailbox) Identity() string {
	return event.ID(m.ID, net.JoinHostPort(m.Host, strconv.Itoa(m.Port)), m.Username)
}
func (m Mailbox) validate() error {
	if !utf8.ValidString(m.Label) || utf8.RuneCountInString(m.Label) > 64 || strings.TrimSpace(m.Label) == "" || strings.ContainsAny(m.Label, "\r\n") {
		return fault.New("mailbox_label_invalid")
	}
	if err := mail.CheckConnectionBudget(nil, m.Limit()); err != nil {
		return err
	}
	if m.Host == "" || strings.ContainsAny(m.Host, "/\r\n \t") || m.Port < 1 || m.Port > 65535 {
		return fault.New("config_imap_address_invalid")
	}
	if strings.TrimSpace(m.Username) == "" {
		return fault.New("config_account_required")
	}
	return credential(m.Password)
}

type Bark struct {
	Endpoint string `json:"endpoint"`
	Key      string `json:"key"`
}
type Pushover struct {
	Token string `json:"token"`
	User  string `json:"user"`
}
type Webhook struct {
	URL    string `json:"url"`
	Secret string `json:"secret"`
}
type Delivery struct {
	NativePairingID string   `json:"native_pairing_id"`
	Revision        int64    `json:"-"`
	Channel         string   `json:"channel"`
	Preview         string   `json:"preview"`
	RetryCount      int      `json:"retry_count"`
	Language        string   `json:"language"`
	Bark            Bark     `json:"bark"`
	Pushover        Pushover `json:"pushover"`
	Webhook         Webhook  `json:"webhook"`
}
type DeliveryUpdate struct {
	NativePairingID *string `json:"native_pairing_id"`
	Revision        *int64  `json:"revision"`
	Channel         string  `json:"channel"`
	Preview         string  `json:"preview"`
	RetryCount      int     `json:"retry_count"`
	Language        string  `json:"language"`
	Bark            struct {
		Endpoint string  `json:"endpoint"`
		Key      *string `json:"key"`
	} `json:"bark"`
	Pushover struct {
		Token *string `json:"token"`
		User  *string `json:"user"`
	} `json:"pushover"`
	Webhook struct {
		URL    *string `json:"url"`
		Secret *string `json:"secret"`
	} `json:"webhook"`
}

func (d Delivery) validate() error {
	if !i18n.Supported(d.Language) {
		return fault.New("config_language_invalid")
	}
	if d.RetryCount < 0 || d.RetryCount > 9 {
		return fault.New("config_retry_count_invalid")
	}
	if d.Preview != "off" && d.Preview != "subject" {
		return fault.New("config_preview_invalid")
	}
	switch d.Channel {
	case "":
		return nil
	case "native":
		return nil
	case "bark":
		if !httpsEndpoint(d.Bark.Endpoint, false) {
			return fault.New("config_bark_endpoint_invalid")
		}
		return credential(d.Bark.Key)
	case "pushover":
		if err := credential(d.Pushover.Token); err != nil {
			return err
		}
		return credential(d.Pushover.User)
	case "webhook":
		if !httpsEndpoint(d.Webhook.URL, true) {
			return fault.New("config_webhook_url_invalid")
		}
		if len(d.Webhook.Secret) < 32 || strings.ContainsAny(d.Webhook.Secret, " \t\r\n") {
			return fault.New("config_webhook_secret_invalid")
		}
		return nil
	default:
		return fault.New("config_delivery_channel_invalid")
	}
}
func httpsEndpoint(raw string, allowQuery bool) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == "" && (allowQuery || u.RawQuery == "")
}
func credential(value string) error {
	if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n") {
		return fault.New("credential_required")
	}
	return nil
}

// Merge applies an update and validates the result.
func (old Mailbox) Merge(u MailboxUpdate) (Mailbox, error) {
	m := Mailbox{ID: old.ID, Revision: old.Revision, Label: old.Label, Host: u.Host, Port: u.Port, Username: u.Username, Password: old.Password, ConnectionLimit: old.Limit()}
	if u.Label != nil {
		m.Label = *u.Label
	}
	if strings.TrimSpace(m.Label) == "" {
		m.Label = m.Username
	}
	if u.ConnectionLimit != nil {
		m.ConnectionLimit = *u.ConnectionLimit
		if m.ConnectionLimit == 0 {
			return m, fault.New("connection_limit_invalid")
		}
	}
	if u.Password != nil {
		m.Password = *u.Password
	}
	return m, m.validate()
}

// Merge applies an update and validates the result.
func (old Delivery) Merge(u DeliveryUpdate) (Delivery, error) {
	d := old
	d.Channel = u.Channel
	d.Preview = u.Preview
	d.RetryCount = u.RetryCount
	if u.NativePairingID != nil {
		d.NativePairingID = *u.NativePairingID
	}
	d.Language = u.Language
	if d.Language == "" {
		d.Language = i18n.Default
	} else if locale, ok := i18n.Resolve(d.Language); ok {
		d.Language = locale
	}
	if u.Bark.Endpoint != "" {
		d.Bark.Endpoint = u.Bark.Endpoint
	}
	if d.Bark.Endpoint == "" {
		d.Bark.Endpoint = "https://api.day.app"
	}
	for _, p := range []struct {
		value  *string
		target *string
	}{{u.Bark.Key, &d.Bark.Key}, {u.Pushover.Token, &d.Pushover.Token}, {u.Pushover.User, &d.Pushover.User}, {u.Webhook.URL, &d.Webhook.URL}, {u.Webhook.Secret, &d.Webhook.Secret}} {
		if p.value != nil {
			*p.target = *p.value
		}
	}
	if d.Channel == "native" && d.NativePairingID == "" {
		return d, fault.New("native_target_required")
	}
	return d, d.validate()
}

func (m Mailbox) Limit() int {
	if m.ConnectionLimit == 0 {
		return mail.DefaultConnectionLimit
	}
	return m.ConnectionLimit
}

// Public views use explicit allowlists. URLs that may carry credentials stay write-only.
func configured(value string) map[string]bool { return map[string]bool{"configured": value != ""} }
func (m Mailbox) View(connections int) map[string]any {
	return map[string]any{"id": m.ID, "label": m.Label, "revision": m.Revision, "connection_limit": m.Limit(), "connections_in_use": connections, "host": m.Host, "port": m.Port, "username": m.Username, "password": configured(m.Password)}
}
func (d Delivery) View() map[string]any {
	return map[string]any{"native_pairing_id": d.NativePairingID, "revision": d.Revision, "channel": d.Channel, "preview": d.Preview, "retry_count": d.RetryCount, "language": d.Language, "bark": map[string]any{"endpoint": d.Bark.Endpoint, "key": configured(d.Bark.Key)}, "pushover": map[string]any{"token": configured(d.Pushover.Token), "user": configured(d.Pushover.User)}, "webhook": map[string]any{"url": configured(d.Webhook.URL), "secret": configured(d.Webhook.Secret)}}
}

func NewMailbox() Mailbox { return Mailbox{ID: "mbx_" + rand.Text()} }

func (d Delivery) RecordsPreview() bool { return d.Channel == "native" || d.Preview == "subject" }

package settings

// Public views allowlist fields and expose only the presence of write-only values.
type ConfiguredValue struct {
	Configured bool `json:"configured"`
}
type MailboxView struct {
	ID               string          `json:"id"`
	Label            string          `json:"label"`
	Revision         int64           `json:"revision"`
	ConnectionLimit  int             `json:"connection_limit"`
	ConnectionsInUse int             `json:"connections_in_use"`
	Host             string          `json:"host"`
	Port             int             `json:"port"`
	Username         string          `json:"username"`
	Password         ConfiguredValue `json:"password"`
}
type BarkView struct {
	Endpoint string          `json:"endpoint"`
	Key      ConfiguredValue `json:"key"`
}
type PushoverView struct {
	Token ConfiguredValue `json:"token"`
	User  ConfiguredValue `json:"user"`
}
type WebhookView struct {
	URL    ConfiguredValue `json:"url"`
	Secret ConfiguredValue `json:"secret"`
}
type DeliveryView struct {
	NativePairingID string       `json:"native_pairing_id"`
	NativeAvailable bool         `json:"native_available"`
	Revision        int64        `json:"revision"`
	Channel         string       `json:"channel"`
	Preview         string       `json:"preview"`
	RetryCount      int          `json:"retry_count"`
	Language        string       `json:"language"`
	Bark            BarkView     `json:"bark"`
	Pushover        PushoverView `json:"pushover"`
	Webhook         WebhookView  `json:"webhook"`
}

func configured(value string) ConfiguredValue { return ConfiguredValue{Configured: value != ""} }
func (m Mailbox) View(connections int) MailboxView {
	return MailboxView{ID: m.ID, Label: m.Label, Revision: m.Revision, ConnectionLimit: m.Limit(), ConnectionsInUse: connections, Host: m.Host, Port: m.Port, Username: m.Username, Password: configured(m.Password)}
}
func (d Delivery) View() DeliveryView {
	return DeliveryView{NativePairingID: d.NativePairingID, Revision: d.Revision, Channel: d.Channel, Preview: d.Preview, RetryCount: d.RetryCount, Language: d.Language, Bark: BarkView{Endpoint: d.Bark.Endpoint, Key: configured(d.Bark.Key)}, Pushover: PushoverView{Token: configured(d.Pushover.Token), User: configured(d.Pushover.User)}, Webhook: WebhookView{URL: configured(d.Webhook.URL), Secret: configured(d.Webhook.Secret)}}
}

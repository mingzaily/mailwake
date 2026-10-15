package settings

import (
	"testing"

	"github.com/mingzaily/mailwake/internal/fault"
)

func TestMailboxCredentialsStayBoundToConnection(t *testing.T) {
	old := Mailbox{ID: "box", Label: "Mail", Host: "imap.example.test", Port: 993, Username: "user", Password: "saved-password", ConnectionLimit: 10}
	for _, field := range []string{"host", "port", "username"} {
		for _, value := range []*string{nil, ptr(""), ptr("  "), ptr("invalid\nvalue"), ptr("replacement-password")} {
			t.Run(field+"/"+passwordCase(value), func(t *testing.T) {
				input := MailboxUpdate{Host: old.Host, Port: old.Port, Username: old.Username, Password: value}
				switch field {
				case "host":
					input.Host = "other.example.test"
				case "port":
					input.Port = 994
				case "username":
					input.Username = "other"
				}
				next, err := old.Merge(input)
				if value == nil || credential(*value) != nil {
					if fault.From(err, "").Code != "mailbox_password_required_for_change" {
						t.Fatal("credential reuse accepted", err)
					}
				} else if err != nil || next.Password != *value {
					t.Fatal("explicit replacement rejected", err)
				}
			})
		}
	}
	limit := 5
	next, err := old.Merge(MailboxUpdate{Host: old.Host, Port: old.Port, Username: old.Username, Label: ptr("Renamed"), ConnectionLimit: &limit})
	if err != nil || next.Password != old.Password || next.Label != "Renamed" || next.Limit() != limit {
		t.Fatal("same connection lost credentials", err)
	}
}

func TestBarkCredentialsStayBoundAcrossChannelChanges(t *testing.T) {
	old := Delivery{Channel: "bark", Preview: "off", Language: "en", Bark: Bark{Endpoint: "https://bark.example.test/prefix", Key: "saved-key"}}
	for _, channel := range []string{"bark", ""} {
		for _, endpoint := range []string{"https://other.example.test/prefix", "https://bark.example.test:8443/prefix", "https://bark.example.test/other", "https://bark.example.test/prefix/"} {
			for _, value := range []*string{nil, ptr(""), ptr("  "), ptr("invalid\nkey"), ptr("replacement-key")} {
				t.Run(channel+"/"+endpoint+"/"+passwordCase(value), func(t *testing.T) {
					input := DeliveryUpdate{Channel: channel, Preview: "off"}
					input.Bark.Endpoint = endpoint
					input.Bark.Key = value
					next, err := old.Merge(input)
					if value == nil || credential(*value) != nil {
						if fault.From(err, "").Code != "bark_key_required_for_change" {
							t.Fatal("credential reuse accepted", err)
						}
					} else if err != nil || next.Bark.Key != *value || next.Bark.Endpoint != endpoint {
						t.Fatal("explicit replacement rejected", err)
					}
				})
			}
		}
	}
	for _, endpoint := range []string{"", old.Bark.Endpoint} {
		input := DeliveryUpdate{Channel: "", Preview: "off"}
		input.Bark.Endpoint = endpoint
		next, err := old.Merge(input)
		if err != nil || next.Bark != old.Bark {
			t.Fatal("switching away lost Bark configuration", err)
		}
		input.Channel = "bark"
		next, err = next.Merge(input)
		if err != nil || next.Bark != old.Bark {
			t.Fatal("switching back lost Bark configuration", err)
		}
	}
}

func passwordCase(value *string) string {
	if value == nil {
		return "omitted"
	}
	if *value == "" {
		return "empty"
	}
	if credential(*value) != nil {
		return "invalid"
	}
	return "provided"
}

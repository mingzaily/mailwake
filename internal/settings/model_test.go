package settings

import (
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/i18n"
	"strings"
	"testing"
)

func ptr(s string) *string { return &s }

func TestChannelValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*DeliveryUpdate)
		valid  bool
	}{
		{"bark", func(d *DeliveryUpdate) {}, true},
		{"http", func(d *DeliveryUpdate) { d.Bark.Endpoint = "http://example.test" }, false},
		{"preview", func(d *DeliveryUpdate) { d.Preview = "content" }, false},
		{"retry", func(d *DeliveryUpdate) { d.RetryCount = 10 }, false},
		{"pushover", func(d *DeliveryUpdate) {
			d.Channel = "pushover"
			d.Pushover.Token = ptr("app")
			d.Pushover.User = ptr("user")
		}, true},
		{"missing pushover user", func(d *DeliveryUpdate) { d.Channel = "pushover"; d.Pushover.Token = ptr("app") }, false},
		{"weak webhook", func(d *DeliveryUpdate) {
			d.Channel = "webhook"
			d.Webhook.URL = ptr("https://example.test")
			d.Webhook.Secret = ptr("short")
		}, false},
		{"url credentials", func(d *DeliveryUpdate) {
			d.Channel = "webhook"
			d.Webhook.URL = ptr("https://user:pass@example.test")
			d.Webhook.Secret = ptr(strings.Repeat("s", 32))
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := DeliveryUpdate{Channel: "bark", Preview: "off"}
			d.Bark.Key = ptr("key")
			tc.change(&d)
			_, err := Delivery{}.Merge(d)
			if (err == nil) != tc.valid {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeSelectionIsPreservedWhenOmittedAndRequiredWhenCleared(t *testing.T) {
	current := Delivery{Channel: "native", Preview: "off", Language: "en", NativePairingID: "selected"}
	update := DeliveryUpdate{Channel: "native", Preview: "off", Language: "en"}
	next, err := current.Merge(update)
	if err != nil || next.NativePairingID != "selected" {
		t.Fatal(next, err)
	}
	empty := ""
	update.NativePairingID = &empty
	if _, err := current.Merge(update); err == nil {
		t.Fatal("empty native target accepted")
	}
	other := "other"
	update.NativePairingID = &other
	next, err = current.Merge(update)
	if err != nil || next.NativePairingID != other {
		t.Fatal("target change was not saved", err)
	}
}

func TestDeliveryLanguageNormalization(t *testing.T) {
	cases := map[string]string{"": "en", "zh-Hans": "zh-CN", "zh-TW": "zh-Hant", "fr-CA": "fr", "pt-PT": "pt-BR"}
	for _, language := range i18n.Languages() {
		cases[language.Code] = language.Code
	}
	for input, want := range cases {
		t.Run(input, func(t *testing.T) {
			update := DeliveryUpdate{Channel: "bark", Preview: "off", Language: input}
			update.Bark.Key = ptr("key")
			got, err := (Delivery{}).Merge(update)
			if err != nil || got.Language != want {
				t.Fatalf("got %q, %v; want %q", got.Language, err, want)
			}
		})
	}
	_, err := (Delivery{}).Merge(DeliveryUpdate{Channel: "bark", Preview: "off", Language: "it"})
	if fault.From(err, "unknown_error").Code != "config_language_invalid" {
		t.Fatalf("unsupported language: %v", err)
	}
}

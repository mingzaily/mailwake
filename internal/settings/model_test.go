package settings

import (
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

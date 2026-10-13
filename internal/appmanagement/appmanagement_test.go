package appmanagement

import (
	"strings"
	"testing"
)

func TestOriginAndScopes(t *testing.T) {
	for raw, want := range map[string]string{"https://Core.Example:443/": "https://core.example", "https://core.example:8443": "https://core.example:8443"} {
		if got, err := Origin(raw); err != nil || got != want {
			t.Fatal(raw, got, err)
		}
	}
	for _, raw := range []string{"http://core.example", "https://user@core.example", "https://core.example/path", "https://core.example?x=1"} {
		if _, err := Origin(raw); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	if got, err := Scopes([]string{"mailboxes", "channels"}); err != nil || strings.Join(got, ",") != "channels,mailboxes" {
		t.Fatal(got, err)
	}
	for _, bad := range [][]string{{}, {"mailboxes", "mailboxes"}, {"admin"}} {
		if _, err := Scopes(bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
}

package appmanagement

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestQualificationVectorSignature(t *testing.T) {
	raw, err := os.ReadFile("testdata/selfhost-pro-v2-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Qualification struct {
			Trust  Trust          `json:"trust"`
			Header map[string]any `json:"header"`
			Claims qualification  `json:"claims"`
			JWT    string         `json:"jwt"`
		} `json:"app_management_qualification"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	v := vectors.Qualification
	parts := strings.Split(v.JWT, ".")
	key := v.Trust.Keys[v.Header["kid"].(string)]
	if !verifyES256(key, parts[0]+"."+parts[1], parts[2]) {
		t.Fatal("vector signature does not verify")
	}
	if verifyES256(key, parts[0]+"."+parts[1]+"x", parts[2]) {
		t.Fatal("tampered input verified")
	}
	// The vector is long expired, so full verification must refuse it.
	if err := v.Trust.Verify(v.JWT, v.Claims.CoreID, v.Claims.DeviceID); err == nil {
		t.Fatal("expired qualification accepted")
	}
}

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

package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mingzaily/mailwake/internal/appmanagement"
)

func TestQualificationTrustInstallAndRotation(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	public := base64.RawURLEncoding.EncodeToString(der)
	trust := appmanagement.Trust{Issuer: "mailwake-platform", Environment: "sandbox", Keys: map[string]string{"old": public, "new": public}}
	raw, _ := json.Marshal(trust)
	path := filepath.Join(t.TempDir(), "trust.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAILWAKE_APP_MANAGEMENT_TRUST", "")
	t.Setenv("MAILWAKE_APP_MANAGEMENT_TRUST_FILE", path)
	c, err := Load()
	if err != nil || len(c.AppManagementTrust.Keys) != 2 {
		t.Fatal(c, err)
	}
	trust.Keys = map[string]string{"new": public}
	raw, _ = json.Marshal(trust)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if len(c.AppManagementTrust.Keys) != 2 {
		t.Fatal("running snapshot changed")
	}
	c, err = Load()
	if err != nil || len(c.AppManagementTrust.Keys) != 1 {
		t.Fatal(c, err)
	}
}

func TestQualificationTrustRejectsMalformedOrUntrustedInput(t *testing.T) {
	for _, raw := range []string{`{}`, `{"issuer":"unknown","environment":"sandbox","keys":{"key":"value"}}`, `{"issuer":"mailwake-platform","environment":"sandbox","keys":{}}`, `{"issuer":"mailwake-platform","environment":"sandbox","keys":{"k":"not-a-key"}}`, `not json`} {
		if _, err := parseTrust([]byte(raw)); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	t.Setenv("MAILWAKE_APP_MANAGEMENT_TRUST", "")
	t.Setenv("MAILWAKE_APP_MANAGEMENT_TRUST_FILE", filepath.Join(t.TempDir(), "missing.json"))
	if _, err := Load(); err == nil {
		t.Fatal("missing installed file")
	}
}

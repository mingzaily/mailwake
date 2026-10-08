// Package config reads process-level deployment settings.
package config

import (
	"encoding/json"
	"net"
	"os"

	"github.com/mingzaily/mailwake/internal/appmanagement"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/localtls"
	"github.com/mingzaily/mailwake/internal/native"
)

type Config struct {
	Listen, DataDir, RelayURL string
	AppManagementTrust        appmanagement.Trust
	LocalTestTLS              localtls.Trust
}

func Load() (Config, error) {
	c := Config{Listen: "127.0.0.1:8080", DataDir: "data"}
	if v := os.Getenv("MAILWAKE_LISTEN"); v != "" {
		c.Listen = v
	}
	if v := os.Getenv("MAILWAKE_DATA_DIR"); v != "" {
		c.DataDir = v
	}
	c.RelayURL = os.Getenv("MAILWAKE_RELAY_URL")
	var tlsErr error
	c.LocalTestTLS, tlsErr = localtls.Load(os.Getenv("MAILWAKE_LOCAL_TEST_TLS"), os.Getenv("MAILWAKE_LOCAL_TEST_IMAP_CA_FILE"), os.Getenv("MAILWAKE_LOCAL_TEST_RELAY_CA_FILE"), os.Getenv("MAILWAKE_LOCAL_TEST_DELIVERY_CA_FILE"))
	if tlsErr != nil {
		return c, tlsErr
	}
	value := os.Getenv("MAILWAKE_APP_MANAGEMENT_TRUST")
	if file := os.Getenv("MAILWAKE_APP_MANAGEMENT_TRUST_FILE"); file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return c, fault.New("app_management_trust_invalid")
		}
		value = string(raw)
	}
	if value != "" {
		var err error
		if c.AppManagementTrust, err = parseTrust([]byte(value)); err != nil {
			return c, err
		}
	}
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return c, fault.New("config_listen_invalid")
	}
	return c, nil
}

// parseTrust reads the operator-installed Platform qualification keys.
func parseTrust(raw []byte) (appmanagement.Trust, error) {
	var trust appmanagement.Trust
	if json.Unmarshal(raw, &trust) != nil || trust.Issuer != "mailwake-platform" || (trust.Environment != "sandbox" && trust.Environment != "production") || len(trust.Keys) == 0 {
		return trust, fault.New("app_management_trust_invalid")
	}
	for _, key := range trust.Keys {
		if _, err := native.KeyID(key); err != nil {
			return trust, fault.New("app_management_trust_invalid")
		}
	}
	return trust, nil
}

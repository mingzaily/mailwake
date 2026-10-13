// Package config reads process-level deployment settings.
package config

import (
	"net"
	"os"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/localtls"
)

type Config struct {
	Listen, DataDir, RelayURL string
	LocalTestTLS              localtls.Trust
}

func Load() (Config, error) {
	c := Config{Listen: "127.0.0.1:8080", DataDir: "data", RelayURL: "https://notify.mailwake.oritx.com"}
	if v := os.Getenv("MAILWAKE_LISTEN"); v != "" {
		c.Listen = v
	}
	if v := os.Getenv("MAILWAKE_DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if v := os.Getenv("MAILWAKE_RELAY_URL"); v != "" {
		c.RelayURL = v
	}
	var tlsErr error
	c.LocalTestTLS, tlsErr = localtls.Load(os.Getenv("MAILWAKE_LOCAL_TEST_TLS"), os.Getenv("MAILWAKE_LOCAL_TEST_IMAP_CA_FILE"), os.Getenv("MAILWAKE_LOCAL_TEST_RELAY_CA_FILE"), os.Getenv("MAILWAKE_LOCAL_TEST_DELIVERY_CA_FILE"))
	if tlsErr != nil {
		return c, tlsErr
	}
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return c, fault.New("config_listen_invalid")
	}
	return c, nil
}

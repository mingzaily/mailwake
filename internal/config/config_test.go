package config

import "testing"

func TestDeploymentSettings(t *testing.T) {
	t.Setenv("MAILWAKE_LISTEN", "")
	t.Setenv("MAILWAKE_DATA_DIR", "")
	t.Setenv("MAILWAKE_RELAY_URL", "")
	c, err := Load()
	if err != nil || c.Listen != "127.0.0.1:8080" || c.DataDir != "data" {
		t.Fatal(c, err)
	}
	t.Setenv("MAILWAKE_LISTEN", "0.0.0.0:9000")
	t.Setenv("MAILWAKE_DATA_DIR", t.TempDir())
	t.Setenv("MAILWAKE_RELAY_URL", "http://127.0.0.1:8787")
	c, err = Load()
	if err != nil || c.Listen != "0.0.0.0:9000" || c.RelayURL != "http://127.0.0.1:8787" {
		t.Fatal(c, err)
	}
	t.Setenv("MAILWAKE_LISTEN", "invalid")
	if _, err := Load(); err == nil {
		t.Fatal("invalid listen address")
	}
}

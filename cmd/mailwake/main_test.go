//go:build mailwake_test

package main

import (
	"os"
	"testing"

	"github.com/mingzaily/mailwake/internal/auth"
)

func TestMain(m *testing.M) {
	auth.UseTestPasswordHashParameters()
	os.Exit(m.Run())
}

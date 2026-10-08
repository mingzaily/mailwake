//go:build mailwake_test

package httpapi

import (
	"github.com/mingzaily/mailwake/internal/auth"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	auth.UseTestPasswordHashParameters()
	os.Exit(m.Run())
}

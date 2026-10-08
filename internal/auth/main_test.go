package auth

import (
	"os"
	"testing"
)

var productionHashParameters = passwordHashParameters

func TestMain(m *testing.M) {
	passwordHashParameters.Memory = 1024
	passwordHashParameters.Iterations = 1
	passwordHashParameters.Threads = 1
	os.Exit(m.Run())
}

//go:build mailwake_test

package auth

// UseTestPasswordHashParameters lowers the cost for integration tests. Call it
// from TestMain before any authentication work. Production builds omit this file.
func UseTestPasswordHashParameters() {
	passwordHashParameters.Memory = 1024
	passwordHashParameters.Iterations = 1
	passwordHashParameters.Threads = 1
}

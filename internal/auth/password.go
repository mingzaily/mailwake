package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mingzaily/mailwake/internal/fault"
	"golang.org/x/crypto/argon2"
)

// passwordHashParameters holds the production cost for new hashes. Tests adjust
// this private value before starting authentication work.
var passwordHashParameters = struct {
	Memory, Iterations uint32
	Threads            uint8
}{Memory: 64 * 1024, Iterations: 3, Threads: 2}

func HashPassword(password string) (string, error) {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 12 || len(password) > 1024 {
		return "", fault.New("password_invalid")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	cost := passwordHashParameters
	hash := argon2.IDKey([]byte(password), salt, cost.Iterations, cost.Memory, cost.Threads, 32)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", cost.Memory, cost.Iterations, cost.Threads, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}
func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || len(password) > 1024 {
		return false
	}
	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return false
	}
	if memory < 8*uint32(threads) || memory > 128*1024 || iterations < 1 || iterations > 10 || threads < 1 || threads > 8 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		return false
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(hash) != 32 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, threads, 32)
	return subtle.ConstantTimeCompare(hash, got) == 1
}

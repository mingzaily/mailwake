package auth

import (
	"strings"
	"testing"
)

func TestPasswordHash(t *testing.T) {
	password := "a synthetic password"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, password) || VerifyPassword(hash, "incorrect") {
		t.Fatal("password verification")
	}
	other, err := HashPassword(password)
	if err != nil || hash == other {
		t.Fatal("salt reuse", err)
	}
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("short accepted")
	}
	for _, encoded := range []string{"", hash + "$bad", "$argon2id$v=19$m=9999999,t=3,p=2$x$y"} {
		if VerifyPassword(encoded, password) {
			t.Fatal("invalid hash accepted")
		}
	}
}

func TestProductionHashParametersAndLegacyHash(t *testing.T) {
	if productionHashParameters.Memory != 64*1024 || productionHashParameters.Iterations != 3 || productionHashParameters.Threads != 2 {
		t.Fatalf("production defaults changed: %+v", productionHashParameters)
	}
	hash, err := HashPassword("new synthetic password")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hash, "$m=1024,t=1,p=1$") {
		t.Fatal("hash does not encode configured cost")
	}
	if !VerifyPassword(hash, "new synthetic password") {
		t.Fatal("new hash rejected")
	}
	const legacy = "$argon2id$v=19$m=65536,t=3,p=2$jmCxoYln42xA89zp+pv7AQ$TQ+oOHkxucCp7UzcYCYYoqnZ6DogmSb9buzD+njShUY"
	if !VerifyPassword(legacy, "legacy synthetic password") || VerifyPassword(legacy, "incorrect") {
		t.Fatal("legacy hash verification changed")
	}
}

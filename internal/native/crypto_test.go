package native

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/mingzaily/mailwake/internal/coreidentity"
)

type vectorKey struct {
	Scalar    string `json:"scalar"`
	PublicKey string `json:"public_key"`
	ID        string `json:"id"`
}
type testVector struct {
	Audience                            string `json:"audience"`
	PairingID                           string `json:"pairing_id"`
	EventID                             string `json:"event_id"`
	ExpiresAt                           int64  `json:"expires_at"`
	Core, Device, Encryption, Ephemeral vectorKey
	Token                               string   `json:"token"`
	TokenHash                           string   `json:"token_hash"`
	ConsentText                         string   `json:"consent_text"`
	DeviceSignature                     string   `json:"device_signature"`
	Plaintext                           string   `json:"plaintext"`
	Envelope                            Envelope `json:"envelope"`
	ContentText                         string   `json:"content_text"`
	ContentSignature                    string   `json:"content_signature"`
	Request                             struct{ Role, Method, Path, Timestamp, Nonce, Authorization, Body, Text, Signature string }
}

func vector(t *testing.T) testVector {
	t.Helper()
	b, err := os.ReadFile("testdata/test-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v testVector
	if err = json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func fixedKey(t *testing.T, k vectorKey) *ecdsa.PrivateKey {
	t.Helper()
	scalar, err := decode(k.Scalar, 32)
	if err != nil {
		t.Fatal(err)
	}
	x, y := elliptic.P256().ScalarBaseMult(scalar)
	return &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, D: new(big.Int).SetBytes(scalar)}
}
func TestSharedVectors(t *testing.T) {
	v := vector(t)
	for _, k := range []vectorKey{v.Core, v.Device, v.Encryption} {
		id, err := coreidentity.KeyID(k.PublicKey)
		if err != nil || id != k.ID {
			t.Fatalf("identity mismatch: %v", err)
		}
	}
	r := v.Request
	if requestText(v.Audience, r.Role, v.Core.ID, r.Method, r.Path, r.Timestamp, r.Nonce, []byte(r.Body), r.Authorization) != r.Text {
		t.Fatal("request canonical text differs")
	}
	if consentText(v.Audience, v.PairingID, v.Core.ID, v.Device.ID, v.Encryption.ID, v.TokenHash, "1790000300") != v.ConsentText {
		t.Fatal("consent canonical text differs")
	}
	if contentText(v.Audience, v.PairingID, v.EventID, v.Envelope) != v.ContentText {
		t.Fatal("content canonical text differs")
	}
	for _, sample := range []struct{ key, signature, text string }{{v.Core.PublicKey, r.Signature, r.Text}, {v.Device.PublicKey, v.DeviceSignature, v.ConsentText}, {v.Core.PublicKey, v.ContentSignature, v.ContentText}} {
		if err := coreidentity.Verify(sample.key, sample.signature, sample.text); err != nil {
			t.Fatal(err)
		}
		if coreidentity.Verify(sample.key, sample.signature, sample.text+"\n") == nil {
			t.Fatal("modified signature accepted")
		}
	}
	sig, err := coreidentity.Sign(fixedKey(t, v.Core), r.Text)
	if err != nil {
		t.Fatal(err)
	}
	if err = coreidentity.Verify(v.Core.PublicKey, sig, r.Text); err != nil {
		t.Fatal(err)
	}
	raw, err := decode(v.Ephemeral.Scalar, 32)
	if err != nil {
		t.Fatal(err)
	}
	ephemeral, err := ecdh.P256().NewPrivateKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	salt, _ := decode(v.Envelope.Salt, 32)
	nonce, _ := decode(v.Envelope.Nonce, 12)
	envelope, err := encryptWith(v.Audience, v.PairingID, v.EventID, v.Encryption.PublicKey, []byte(v.Plaintext), ephemeral, salt, nonce)
	if err != nil || envelope != v.Envelope {
		t.Fatalf("Go encryption differs from Node / Workers vector: %v", err)
	}
}

// Package native implements the Relay v1 protocol and encrypted device delivery.
package native

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"strings"

	"github.com/mingzaily/mailwake/internal/fault"
	"golang.org/x/crypto/hkdf"
)

var encoding = base64.RawURLEncoding

func hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func decode(value string, size int) ([]byte, error) {
	b, err := encoding.Strict().DecodeString(value)
	if err != nil || len(b) != size || encoding.EncodeToString(b) != value {
		return nil, fault.New("native_protocol_invalid")
	}
	return b, nil
}
func PublicKey(value string) (*ecdsa.PublicKey, error) {
	b, err := decode(value, 91)
	if err != nil {
		return nil, err
	}
	p, err := x509.ParsePKIXPublicKey(b)
	if err != nil {
		return nil, fault.New("native_protocol_invalid")
	}
	key, ok := p.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, fault.New("native_protocol_invalid")
	}
	canonical, err := x509.MarshalPKIXPublicKey(key)
	if err != nil || encoding.EncodeToString(canonical) != value {
		return nil, fault.New("native_protocol_invalid")
	}
	return key, nil
}
func KeyID(value string) (string, error) {
	if _, err := PublicKey(value); err != nil {
		return "", err
	}
	b, _ := decode(value, 91)
	return hash(b), nil
}
func sign(key *ecdsa.PrivateKey, text string) (string, error) {
	digest := sha256.Sum256([]byte(text))
	der, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		return "", err
	}
	var pair struct{ R, S *big.Int }
	if _, err = asn1.Unmarshal(der, &pair); err != nil {
		return "", err
	}
	raw := make([]byte, 64)
	pair.R.FillBytes(raw[:32])
	pair.S.FillBytes(raw[32:])
	return encoding.EncodeToString(raw), nil
}
func verify(key, signature, text string) error {
	pub, err := PublicKey(key)
	if err != nil {
		return err
	}
	raw, err := decode(signature, 64)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(text))
	if !ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(raw[:32]), new(big.Int).SetBytes(raw[32:])) {
		return fault.New("native_signature_invalid")
	}
	return nil
}
func requestText(audience, role, id, method, path, timestamp, nonce string, body []byte, authorization string) string {
	return strings.Join([]string{"mailwake-request-v1", audience, role, id, method, path, timestamp, nonce, hash(body), hash([]byte(authorization))}, "\n")
}
func consentText(audience, pairingID, coreID, deviceID, encryptionHash, tokenHash, expires string) string {
	return strings.Join([]string{"mailwake-pairing-v1", audience, pairingID, coreID, deviceID, encryptionHash, tokenHash, expires}, "\n")
}

type Envelope struct {
	V                  int    `json:"v"`
	Algorithm          string `json:"algorithm"`
	EphemeralPublicKey string `json:"ephemeral_public_key"`
	Salt               string `json:"salt"`
	Nonce              string `json:"nonce"`
	Ciphertext         string `json:"ciphertext"`
}

func contentText(audience, pairingID, eventID string, e Envelope) string {
	b, _ := json.Marshal(e)
	return strings.Join([]string{"mailwake-content-v1", audience, pairingID, eventID, string(b)}, "\n")
}
func encrypt(audience, pairingID, eventID, key string, plain []byte) (Envelope, error) {
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return Envelope{}, err
	}
	salt := make([]byte, 32)
	nonce := make([]byte, 12)
	if _, err = rand.Read(salt); err != nil {
		return Envelope{}, err
	}
	if _, err = rand.Read(nonce); err != nil {
		return Envelope{}, err
	}
	return encryptWith(audience, pairingID, eventID, key, plain, ephemeral, salt, nonce)
}
func encryptWith(audience, pairingID, eventID, key string, plain []byte, ephemeral *ecdh.PrivateKey, salt, nonce []byte) (Envelope, error) {
	if len(plain) == 0 || len(plain) > 2032 {
		return Envelope{}, fault.New("native_payload_too_large")
	}
	pub, err := PublicKey(key)
	if err != nil {
		return Envelope{}, err
	}
	ecdhKey, err := pub.ECDH()
	if err != nil {
		return Envelope{}, err
	}
	shared, err := ephemeral.ECDH(ecdhKey)
	if err != nil {
		return Envelope{}, err
	}
	context := []byte(strings.Join([]string{"mailwake-e2ee-v1", audience, pairingID, eventID}, "\n"))
	aesKey := make([]byte, 32)
	if _, err = io.ReadFull(hkdf.New(sha256.New, shared, salt, context), aesKey); err != nil {
		return Envelope{}, err
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return Envelope{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return Envelope{}, err
	}
	der, err := x509.MarshalPKIXPublicKey(ephemeral.PublicKey())
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{1, "P256-HKDF-SHA256-A256GCM", encoding.EncodeToString(der), encoding.EncodeToString(salt), encoding.EncodeToString(nonce), encoding.EncodeToString(aead.Seal(nil, nonce, plain, context))}, nil
}

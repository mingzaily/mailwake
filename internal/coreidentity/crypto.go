// Package coreidentity owns the local Core identity and P-256 signatures.
package coreidentity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"math/big"

	"github.com/mingzaily/mailwake/internal/fault"
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
func Sign(key *ecdsa.PrivateKey, text string) (string, error) {
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
func Verify(key, signature, text string) error {
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

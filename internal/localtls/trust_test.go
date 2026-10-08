package localtls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func rootFixture(t *testing.T) (string, []byte, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	raw := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	path := filepath.Join(t.TempDir(), "test-ca.pem")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path, raw, cert
}

func TestExplicitTLSRootEnablementIsolationAndStrictInput(t *testing.T) {
	imapPath, raw, imapCert := rootFixture(t)
	relayPath, _, relayCert := rootFixture(t)
	deliveryPath, _, deliveryCert := rootFixture(t)
	for _, input := range []struct{ enabled, imap, relay, delivery string }{{"", imapPath, "", ""}, {"0", "", relayPath, ""}, {"", "", "", deliveryPath}, {"true", imapPath, "", ""}, {"1", "", "", ""}, {"1", "/missing-ca", "", ""}, {"1", "", "", "relative-ca.pem"}} {
		if _, err := Load(input.enabled, input.imap, input.relay, input.delivery); err == nil {
			t.Fatal("accepted invalid local trust configuration", input)
		}
	}
	trust, err := Load("1", imapPath, relayPath, deliveryPath)
	if err != nil {
		t.Fatal(err)
	}
	for i, cert := range []*x509.Certificate{imapCert, relayCert, deliveryCert} {
		for j, pool := range []*x509.CertPool{trust.IMAP, trust.Relay, trust.Delivery} {
			_, verifyErr := cert.Verify(x509.VerifyOptions{Roots: pool})
			if (i == j) != (verifyErr == nil) {
				t.Fatalf("root %d trusted by pool %d: %v", i, j, verifyErr)
			}
		}
	}
	for _, host := range []string{"localhost", "127.0.0.1", "::1", "imap.stage35.test"} {
		if Roots(host, trust.IMAP) == nil {
			t.Fatal(host)
		}
	}
	for _, host := range []string{"imap.gmail.com", "relay.example.org", "localhost.example.org", "192.168.1.1", "test"} {
		if Roots(host, trust.IMAP) != nil {
			t.Fatal("test roots reached public host", host)
		}
	}
	for _, data := range [][]byte{[]byte("garbage"), append([]byte("prefix garbage\n"), raw...), append(append([]byte{}, raw...), []byte("suffix garbage")...), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("fixture")})} {
		path := filepath.Join(t.TempDir(), "invalid.pem")
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = Load("1", "", "", path); err == nil {
			t.Fatal("invalid PEM accepted")
		}
	}
	link := filepath.Join(t.TempDir(), "symlink.pem")
	if err = os.Symlink(imapPath, link); err != nil {
		t.Fatal(err)
	}
	if _, err = Load("1", link, "", ""); err == nil {
		t.Fatal("symlink trust accepted")
	}
	defaults, err := Load("", "", "", "")
	if err != nil || defaults.IMAP != nil || defaults.Relay != nil || defaults.Delivery != nil {
		t.Fatal("default trust changed", err)
	}
}

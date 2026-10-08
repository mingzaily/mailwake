// Package localtls loads isolated trust roots for explicit local test endpoints.
package localtls

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/mingzaily/mailwake/internal/fault"
)

type Trust struct{ IMAP, Relay, Delivery *x509.CertPool }

func TestHost(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	host = strings.ToLower(host)
	return host == "localhost" || strings.HasSuffix(host, ".test")
}

// Roots returns a fresh pool only for local test names. Public endpoints retain
// system trust even when the process also connects to a local fixture.
func Roots(host string, pool *x509.CertPool) *x509.CertPool {
	if pool == nil || !TestHost(host) {
		return nil
	}
	return pool.Clone()
}

func Load(enabled, imapFile, relayFile, deliveryFile string) (Trust, error) {
	var trust Trust
	if enabled != "" && enabled != "0" && enabled != "1" {
		return trust, fault.New("config_local_test_tls_invalid")
	}
	if enabled != "1" {
		if imapFile != "" || relayFile != "" || deliveryFile != "" {
			return trust, fault.New("config_local_test_tls_invalid")
		}
		return trust, nil
	}
	if imapFile == "" && relayFile == "" && deliveryFile == "" {
		return trust, fault.New("config_local_test_tls_invalid")
	}
	var err error
	if imapFile != "" {
		trust.IMAP, err = loadPool(imapFile)
		if err != nil {
			return Trust{}, err
		}
	}
	if relayFile != "" {
		trust.Relay, err = loadPool(relayFile)
		if err != nil {
			return Trust{}, err
		}
	}
	if deliveryFile != "" {
		trust.Delivery, err = loadPool(deliveryFile)
		if err != nil {
			return Trust{}, err
		}
	}
	return trust, nil
}

func loadPool(path string) (*x509.CertPool, error) {
	if !filepath.IsAbs(path) {
		return nil, fault.New("config_local_test_tls_invalid")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 64<<10 {
		return nil, fault.New("config_local_test_tls_invalid")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 64<<10 {
		return nil, fault.New("config_local_test_tls_invalid")
	}
	pool := x509.NewCertPool()
	count := 0
	for len(strings.TrimSpace(string(raw))) > 0 {
		raw = []byte(strings.TrimSpace(string(raw)))
		if !strings.HasPrefix(string(raw), "-----BEGIN CERTIFICATE-----") {
			return nil, fault.New("config_local_test_tls_invalid")
		}
		block, rest := pem.Decode(raw)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) > 0 {
			return nil, fault.New("config_local_test_tls_invalid")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !cert.IsCA || !cert.BasicConstraintsValid {
			return nil, fault.New("config_local_test_tls_invalid")
		}
		pool.AddCert(cert)
		count++
		raw = rest
	}
	if count == 0 {
		return nil, fault.New("config_local_test_tls_invalid")
	}
	return pool, nil
}

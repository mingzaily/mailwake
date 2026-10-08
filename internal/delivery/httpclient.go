package delivery

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/mingzaily/mailwake/internal/localtls"
)

// NewHTTPClient returns a client that never follows redirects, so credentials in a
// request are never forwarded to another host.
func NewHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// NewHTTPClientWithLocalTestRoots applies an isolated pool to HTTPS test hosts.
// Target validation remains in the channel configuration; public and HTTP
// targets retain the default transport and its system trust.
func NewHTTPClientWithLocalTestRoots(rawURL string, roots *x509.CertPool) *http.Client {
	client := NewHTTPClient()
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || roots == nil || !localtls.TestHost(u.Hostname()) {
		return client
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots.Clone(), ServerName: u.Hostname()}
	client.Transport = transport
	return client
}

// HTTPFailure classifies a non-2xx response: 429 and 5xx are temporary, other statuses
// are permanent. The response body is discarded so provider messages never reach storage.
func HTTPFailure(channel string, resp *http.Response) *Failure {
	io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
	return &Failure{
		HTTPStatus: resp.StatusCode,
		Code:       channel + "_http_error",
		Params:     map[string]string{"status": strconv.Itoa(resp.StatusCode)},
		Retryable:  resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500,
		RetryAfter: ParseRetryAfter(resp.Header.Get("Retry-After")),
	}
}

// ParseRetryAfter reads delay-seconds or an HTTP date; invalid values mean no delay.
func ParseRetryAfter(value string) time.Duration {
	const longest = time.Duration(1<<63 - 1)
	if seconds, err := strconv.ParseUint(value, 10, 64); err == nil {
		if seconds > uint64(longest/time.Second) {
			return longest
		}
		return time.Duration(seconds) * time.Second
	} else if errors.Is(err, strconv.ErrRange) {
		return longest
	}
	if when, err := http.ParseTime(value); err == nil {
		return max(time.Until(when), 0)
	}
	return 0
}

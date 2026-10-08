package native

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/localtls"
)

type Info struct {
	Audience    string `json:"audience"`
	Environment string `json:"environment"`
	Version     int    `json:"version"`
}

func (i Info) sameIdentity(other Info) bool {
	return i.Audience == other.Audience && i.Environment == other.Environment && i.Version == other.Version
}

type relayClock struct {
	local  time.Time
	offset time.Duration
}

type Client struct {
	clock atomic.Pointer[relayClock]
	url   string
	http  *http.Client
}

type ClientOptions struct{ RootCAs *x509.CertPool }

func NewClient(raw string) (*Client, error) { return NewClientWithOptions(raw, ClientOptions{}) }
func NewClientWithOptions(raw string, options ClientOptions) (*Client, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fault.New("native_relay_url_invalid")
	}
	loopback := u.Hostname() == "localhost"
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		loopback = ip.IsLoopback()
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return nil, fault.New("native_relay_url_invalid")
	}
	client := delivery.NewHTTPClient()
	if options.RootCAs != nil {
		if u.Scheme != "https" || !localtls.TestHost(u.Hostname()) {
			return nil, fault.New("config_local_test_tls_invalid")
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: options.RootCAs.Clone(), ServerName: u.Hostname()}
		client.Transport = transport
	}
	return &Client{url: strings.TrimSuffix(raw, "/"), http: client}, nil
}

var audiencePattern = regexp.MustCompile(`^[a-zA-Z0-9._:-]{1,128}$`)
var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func (c *Client) Info(ctx context.Context) (Info, error) {
	return c.info(ctx, false)
}

const pairingClockTolerance = 120 * time.Second

func (c *Client) info(ctx context.Context, checkClock bool) (Info, error) {
	var info Info
	var headers http.Header
	started := time.Now()
	err := c.call(ctx, nil, "", "GET", "/v1/info", nil, &info, 200, &headers)
	if err == nil && (info.Version != 2 || !audiencePattern.MatchString(info.Audience) || (info.Environment != "sandbox" && info.Environment != "production")) {
		err = fault.New("native_protocol_invalid")
	}
	if err == nil {
		relayTime, parseErr := http.ParseTime(headers.Get("Date"))
		if parseErr != nil {
			if checkClock {
				return info, fault.New("native_protocol_invalid")
			}
			return info, nil
		}
		// HTTP Date has one-second precision; compare it with the request midpoint.
		midpoint := started.Add(time.Since(started) / 2)
		offset := relayTime.Sub(midpoint.Truncate(time.Second))
		if checkClock && offset.Abs() > pairingClockTolerance {
			return info, &fault.Error{Code: "clock_skew", Params: map[string]string{"seconds": strconv.FormatInt(int64(offset.Abs()/time.Second), 10)}}
		}
		if offset.Abs() <= pairingClockTolerance {
			c.clock.Store(&relayClock{local: midpoint, offset: offset})
		}
	}
	return info, err
}
func (c *Client) call(ctx context.Context, identity *Identity, audience, method, path string, body any, result any, status int, headers ...*http.Header) error {
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url+path, bytes.NewReader(raw))
	if err != nil {
		return fault.New("native_protocol_invalid")
	}
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "")
	if identity != nil {
		nonce := make([]byte, 32)
		if _, err = rand.Read(nonce); err != nil {
			return err
		}
		n := encoding.EncodeToString(nonce)
		now := time.Now()
		if clock := c.clock.Load(); clock != nil {
			// The monotonic sample remains valid if NTP adjusts the Core wall clock.
			now = clock.local.Add(time.Since(clock.local)).Add(clock.offset)
		}
		timestamp := strconv.FormatInt(now.Unix(), 10)
		signature, err := sign(identity.Key, requestText(audience, "core", identity.ID, method, path, timestamp, n, raw, ""))
		if err != nil {
			return err
		}
		for k, v := range map[string]string{"X-Relay-Role": "core", "X-Relay-ID": identity.ID, "X-Relay-Timestamp": timestamp, "X-Relay-Nonce": n, "X-Relay-Signature": signature} {
			req.Header.Set(k, v)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return &delivery.Failure{Code: "native_relay_unavailable", Retryable: true}
	}
	defer resp.Body.Close()
	for _, target := range headers {
		*target = resp.Header.Clone()
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32769))
	if err != nil || len(data) > 32768 {
		return &delivery.Failure{Code: "native_protocol_invalid", Retryable: true}
	}
	if resp.StatusCode != status {
		failure := &delivery.Failure{Code: "native_relay_error", HTTPStatus: resp.StatusCode, Retryable: resp.StatusCode == 429 || resp.StatusCode >= 500, RetryAfter: delivery.ParseRetryAfter(resp.Header.Get("Retry-After"))}
		var value struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &value) == nil && codePattern.MatchString(value.Error.Code) {
			failure.Params = map[string]string{"relay_code": value.Error.Code}
		}
		return failure
	}
	if result != nil && json.Unmarshal(data, result) != nil {
		return &delivery.Failure{Code: "native_protocol_invalid", Retryable: true}
	}
	return nil
}

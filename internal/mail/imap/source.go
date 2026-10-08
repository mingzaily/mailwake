// Package imap confines go-imap types and UID semantics to the IMAP adapter.
package imap

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"slices"
	"sort"
	"strconv"
	"time"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
	"github.com/mingzaily/mailwake/internal/buildinfo"
	"github.com/mingzaily/mailwake/internal/content"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
)

// headerDecoder decodes subjects and sender names in legacy charsets such as GBK,
// Big5 and Shift_JIS. Servers return envelope fields as raw RFC 2047 encoded words.
var headerDecoder = &mime.WordDecoder{CharsetReader: charset.Reader}

type Source struct {
	address, username, password string
	tlsConfig                   *tls.Config
	timeout                     time.Duration
	log                         *slog.Logger
	dial                        func(context.Context, string, string) (net.Conn, error)
	startTLS                    bool
}

// Options configures the transport, STARTTLS and verified TLS roots.
type Options struct {
	DialContext func(context.Context, string, string) (net.Conn, error)
	StartTLS    bool
	RootCAs     *x509.CertPool
}

func NewWithOptions(host string, port int, username, password string, log *slog.Logger, options Options) *Source {
	s := New(host, port, username, password, log)
	s.dial, s.startTLS, s.tlsConfig.RootCAs = options.DialContext, options.StartTLS, options.RootCAs
	return s
}

func New(host string, port int, username, password string, log *slog.Logger) *Source {
	return &Source{address: net.JoinHostPort(host, strconv.Itoa(port)), username: username, password: password,
		tlsConfig: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}, timeout: commandTimeout, log: log}
}

type connection struct {
	conn    net.Conn
	timeout time.Duration
	client  *imapclient.Client
	stop    func() bool
	updates chan struct{}
	caps    protocol.CapSet
}

func (s *Source) connect(ctx context.Context) (*connection, error) {
	d := tls.Dialer{NetDialer: &net.Dialer{Timeout: 15 * time.Second}, Config: s.tlsConfig}
	var conn net.Conn
	var err error
	if s.dial != nil || s.startTLS {
		dial := s.dial
		if dial == nil {
			dial = (&net.Dialer{Timeout: 15 * time.Second}).DialContext
		}
		conn, err = dial(ctx, "tcp", s.address)
		if err == nil && !s.startTLS {
			secure := tls.Client(conn, s.tlsConfig)
			err = secure.HandshakeContext(ctx)
			if err != nil {
				conn.Close()
			} else {
				conn = secure
			}
		}
	} else {
		conn, err = d.DialContext(ctx, "tcp", s.address)
	}
	if err != nil {
		return nil, fault.New("imap_connection_failed")
	}
	x := &connection{conn: conn, timeout: s.timeout, updates: make(chan struct{}, 1)}
	options := &imapclient.Options{
		TLSConfig:   s.tlsConfig,
		WordDecoder: headerDecoder,
		UnilateralDataHandler: &imapclient.UnilateralDataHandler{
			Mailbox: func(data *imapclient.UnilateralDataMailbox) {
				if data.NumMessages != nil {
					select {
					case x.updates <- struct{}{}:
					default:
					}
				}
			},
		}}
	x.stop = context.AfterFunc(ctx, func() { conn.Close() })
	if s.startTLS {
		conn.SetDeadline(time.Now().Add(s.timeout))
		x.client, err = imapclient.NewStartTLS(conn, options)
		conn.SetDeadline(time.Time{})
		if err != nil {
			x.stop()
			conn.Close()
			return nil, fault.New("imap_connection_failed")
		}
	} else {
		x.client = imapclient.New(conn, options)
	}
	if err := x.command(ctx, "imap_login_failed", func() error { return loginError(x.client.Login(s.username, s.password).Wait()) }); err != nil {
		x.Close()
		return nil, err
	}
	// beta.8 completes Login before invalidating its capability cache. A NOOP
	// response is a protocol barrier: the login handler has finished, so Caps
	// joins the automatic refresh (or uses capabilities included in LOGIN).
	if err := x.command(ctx, "imap_capability_failed", func() error {
		if err := x.client.Noop().Wait(); err != nil {
			return err
		}
		x.caps = x.client.Caps()
		if x.caps == nil {
			return fault.New("imap_capability_failed")
		}
		return nil
	}); err != nil {
		x.Close()
		return nil, err
	}
	if x.caps.Has(protocol.CapID) {
		version, _ := buildinfo.Current()
		var rejected *protocol.Error
		err := x.command(ctx, "imap_id_failed", func() error {
			_, err := x.client.ID(&protocol.IDData{
				Name: "Mailwake", Version: version, Vendor: "Mailwake",
				SupportURL: "https://github.com/mingzaily/mailwake",
			}).Wait()
			// A rejected optional ID command leaves the connection usable.
			if errors.As(err, &rejected) {
				return nil
			}
			return err
		})
		if err != nil {
			s.log.Debug("IMAP ID exchange did not complete", "code", fault.From(err, "imap_id_failed").Code)
			x.Close()
			return nil, err
		}
		if rejected != nil {
			s.log.Debug("IMAP ID command was rejected")
		}
	}
	return x, nil
}

func (x *connection) Close() error { x.stop(); return x.client.Close() }

// loginError separates a server rejection of the credentials from transport failures.
func loginError(err error) error {
	var rejected *protocol.Error
	if errors.As(err, &rejected) && rejected.Type == protocol.StatusResponseTypeNo {
		return fault.New(mail.CodeAuthFailed)
	}
	return err
}

func (s *Source) Folders(ctx context.Context) ([]string, error) {
	x, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer x.Close()
	folders := []string{}
	err = x.command(ctx, "imap_discovery_failed", func() error {
		cmd := x.client.List("", "*", nil)
		defer cmd.Close()
		for item := cmd.Next(); item != nil; item = cmd.Next() {
			selectable := true
			for _, attr := range item.Attrs {
				if attr == protocol.MailboxAttrNoSelect {
					selectable = false
				}
			}
			if selectable {
				folders = append(folders, item.Mailbox)
			}
			if len(folders) > 2000 {
				x.conn.Close()
				return fault.New("imap_folder_limit")
			}
		}
		return cmd.Close()
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(folders)
	return folders, nil
}

func (s *Source) Open(ctx context.Context, folder string) (mail.Session, error) {
	x, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	session, err := x.Select(ctx, folder)
	if err != nil {
		x.Close()
		return nil, err
	}
	return session, nil
}
func (s *Source) OpenScheduled(ctx context.Context) (mail.FolderConnection, error) {
	return s.connect(ctx)
}
func (x *connection) Select(ctx context.Context, folder string) (mail.Session, error) {
	var rejected *protocol.Error
	var selected *protocol.SelectData
	err := x.command(ctx, "imap_folder_open_failed", func() error {
		var err error
		selected, err = x.client.Select(folder, &protocol.SelectOptions{ReadOnly: true}).Wait()
		if errors.As(err, &rejected) {
			if code := responseCode(string(rejected.Code)); code != "" {
				return &fault.Error{Code: "imap_folder_rejected", Params: map[string]string{"response_code": code}}
			}
		}
		return err
	})
	if err != nil {
		if rejected != nil {
			return nil, &mail.FolderError{Err: err}
		}
		return nil, err
	}
	if selected.UIDValidity == 0 {
		return nil, &mail.FolderError{Err: fault.New("imap_uid_invalid")}
	}
	return &session{connection: x, folder: folder, validity: selected.UIDValidity, uidNext: uint32(selected.UIDNext), idle: x.caps.Has(protocol.CapIdle)}, nil
}

// responseCode keeps a bounded protocol identifier and discards server-controlled punctuation.
func responseCode(raw string) string {
	code := make([]byte, 0, 32)
	for i := 0; i < len(raw) && len(code) < 32; i++ {
		c := raw[i]
		if c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '/' {
			code = append(code, c)
		}
	}
	return string(code)
}

type session struct {
	*connection
	folder   string
	validity uint32
	// uidNext is the UIDNEXT reported by SELECT, used as the baseline of a new checkpoint.
	uidNext uint32
	idle    bool
}

type cursor struct {
	Validity uint32 `json:"validity"`
	Next     uint32 `json:"next"`
}

func (s *session) Mode() string {
	if s.idle {
		return "idle"
	}
	return "poll"
}

// Poll finds mail that arrived at or after the checkpoint's next UID. It avoids STATUS on
// the selected mailbox, which servers may answer from a cache: NOOP lets the server report
// changes to this session, then UID SEARCH lists the new messages.
func (s *session) Poll(ctx context.Context, checkpoint string, detectCodes bool) (mail.Batch, error) {
	var previous cursor
	if checkpoint != "" {
		if err := json.Unmarshal([]byte(checkpoint), &previous); err != nil || previous.Validity == 0 || previous.Next == 0 {
			return mail.Batch{}, fault.New("imap_checkpoint_invalid")
		}
	}
	if checkpoint == "" || previous.Validity != s.validity {
		next, err := s.baseline(ctx)
		if err != nil {
			return mail.Batch{}, err
		}
		b, _ := json.Marshal(cursor{Validity: s.validity, Next: next})
		return mail.Batch{Checkpoint: string(b), Reset: checkpoint != ""}, nil
	}
	if err := s.command(ctx, "imap_status_failed", func() error { return s.client.Noop().Wait() }); err != nil {
		return mail.Batch{}, err
	}
	// The search below covers every change reported so far; later reports wake the next Wait.
	select {
	case <-s.updates:
	default:
	}
	uids, err := s.search(ctx, protocol.UIDSet{{Start: protocol.UID(previous.Next), Stop: 0}})
	if err != nil {
		return mail.Batch{}, err
	}
	// "n:*" also matches the highest UID when every message is below n.
	uids = slices.DeleteFunc(uids, func(uid protocol.UID) bool { return uint32(uid) < previous.Next })
	if len(uids) == 0 {
		return mail.Batch{Checkpoint: checkpoint}, nil
	}
	limit := maxBatch
	if detectCodes {
		limit = maxCodeBatch
	}
	batch := mail.Batch{More: len(uids) > limit}
	uids = uids[:min(len(uids), limit)]
	var set protocol.UIDSet
	set.AddNum(uids...)
	options := &protocol.FetchOptions{UID: true, Envelope: true, InternalDate: true}
	section := &protocol.FetchItemBodySection{Peek: true, Partial: &protocol.SectionPartial{Size: maxCodeMessageBytes}}
	if detectCodes {
		options.BodySection = []*protocol.FetchItemBodySection{section}
		options.RFC822Size = true
	}
	messages := make(map[protocol.UID]mail.Message, len(uids))
	err = s.command(ctx, "imap_metadata_failed", func() error {
		cmd := s.client.Fetch(set, options)
		defer cmd.Close()
		for data := cmd.Next(); data != nil; data = cmd.Next() {
			item, err := data.Collect()
			if err != nil {
				return err
			}
			msg := mail.Message{Location: &mail.Location{UIDValidity: s.validity, UID: uint32(item.UID)}, Key: fmt.Sprintf("%d:%d", s.validity, item.UID), ReceivedAt: item.InternalDate}
			if item.Envelope != nil {
				msg.Subject = item.Envelope.Subject
				if len(item.Envelope.From) > 0 {
					msg.Sender = item.Envelope.From[0].Addr()
				}
			}
			if detectCodes {
				raw := item.FindBodySection(section)
				// Parse complete messages only; partial MIME can yield incomplete codes.
				if int64(len(raw)) == item.RFC822Size && item.RFC822Size <= maxCodeMessageBytes {
					if parsed, err := content.Extract(bytes.NewReader(raw), msg.Subject); err == nil {
						msg.Code = parsed.Code
					}
				}
			}
			// Retain only notification fields as the next message arrives.
			messages[item.UID] = msg
		}
		return cmd.Close()
	})
	if err != nil {
		return mail.Batch{}, err
	}
	// Keep UID order and skip messages expunged between SEARCH and FETCH.
	for _, uid := range uids {
		if msg, ok := messages[uid]; ok {
			batch.Messages = append(batch.Messages, msg)
		}
	}
	last := uids[len(uids)-1]
	b, _ := json.Marshal(cursor{Validity: s.validity, Next: uint32(last) + 1})
	batch.Checkpoint = string(b)
	return batch, nil
}

const maxBatch = 500

// Native batches transfer at most 2 MiB of message content within one FETCH deadline.
const maxCodeBatch = 8
const maxCodeMessageBytes = 256 << 10

// baseline returns the next UID that counts as new mail: UIDNEXT from SELECT, or one past
// the highest existing UID when the server omits UIDNEXT.
func (s *session) baseline(ctx context.Context) (uint32, error) {
	if s.uidNext != 0 {
		return s.uidNext, nil
	}
	uids, err := s.search(ctx, protocol.UIDSet{{Start: 1, Stop: 0}})
	if err != nil {
		return 0, err
	}
	next := uint32(1)
	for _, uid := range uids {
		next = max(next, uint32(uid)+1)
	}
	return next, nil
}

func (s *session) search(ctx context.Context, set protocol.UIDSet) ([]protocol.UID, error) {
	var uids []protocol.UID
	err := s.command(ctx, "imap_status_failed", func() error {
		data, err := s.client.UIDSearch(&protocol.SearchCriteria{UID: []protocol.UIDSet{set}}, nil).Wait()
		if err != nil {
			return err
		}
		uids = data.AllUIDs()
		return nil
	})
	slices.Sort(uids)
	return uids, err
}

func (s *session) Wait(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	if !s.idle {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.client.Closed():
			return fault.New("imap_connection_closed")
		case <-timer.C:
			return nil
		}
	}
	var cmd *imapclient.IdleCommand
	err := s.command(ctx, "imap_idle_start_failed", func() error {
		var err error
		cmd, err = s.client.Idle()
		return err
	})
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
	case <-s.client.Closed():
	case <-s.updates:
	case <-timer.C:
	}
	return s.command(ctx, "imap_idle_response_failed", func() error {
		if err := cmd.Close(); err != nil {
			return fault.New("imap_idle_stop_failed")
		}
		return cmd.Wait()
	})
}

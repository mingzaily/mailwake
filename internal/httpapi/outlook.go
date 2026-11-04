package httpapi

import (
	"context"
	"crypto/rand"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/outlook"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
	"golang.org/x/oauth2"
)

// The single-owner console keeps one device sign-in in memory. Starting another
// cancels the first. Only the originating session and mailbox can use the grant.
type outlookSignIn struct {
	mu      sync.Mutex
	current *outlookAttempt
}
type outlookAttempt struct {
	id, session, mailbox, username string
	expires                        time.Time
	cancel                         context.CancelFunc
	token                          *oauth2.Token
	err                            error
}

func (s *outlookSignIn) start(c *gin.Context) {
	session, ok := c.Get("session")
	if !ok {
		respondError(c, 403, fault.New("session_required"))
		return
	}
	var input struct {
		Mailbox  string `json:"mailbox_id"`
		Username string `json:"username"`
	}
	if !decodeRequest(c, &input) {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if input.Username == "" || strings.ContainsAny(input.Username, "\x00\x01\r\n") {
		respondFault(c, fault.New("outlook_account_invalid"))
		return
	}
	device, err := outlook.Start(c.Request.Context())
	if err != nil {
		respondFault(c, err)
		return
	}
	ctx, cancel := context.WithDeadline(context.Background(), device.Expiry)
	a := &outlookAttempt{id: rand.Text(), session: session.(*storage.Session).Hash, mailbox: input.Mailbox, username: input.Username, expires: device.Expiry, cancel: cancel}
	s.mu.Lock()
	if s.current != nil {
		s.current.cancel()
	}
	s.current = a
	s.mu.Unlock()
	context.AfterFunc(ctx, func() { s.clear(a.id) })
	go func() {
		token, err := outlook.Authorize(ctx, device)
		s.mu.Lock()
		defer s.mu.Unlock()
		a.token, a.err = token, err
	}()
	c.JSON(200, gin.H{"authorization_id": a.id, "user_code": device.UserCode, "verification_uri": device.VerificationURI})
}

func (s *outlookSignIn) clear(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil && s.current.id == id {
		s.current.cancel()
		s.current = nil
	}
}

func (h mailboxHandlers) useAuthorization(c *gin.Context, id string, input *settings.MailboxUpdate) bool {
	if id == "" {
		return true
	}
	session, ok := c.Get("session")
	if h.outlook == nil || !ok {
		respondError(c, 403, fault.New("forbidden"))
		return false
	}
	if input.Password != nil {
		respondFault(c, fault.New("request_invalid"))
		return false
	}
	token, err := h.outlook.credential(id, session.(*storage.Session).Hash, c.Param("id"), input.Username)
	if err != nil {
		respondFault(c, err)
		return false
	}
	input.Outlook = token
	return true
}

func (s *outlookSignIn) credential(id, session, mailbox, username string) (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.current
	if a == nil || a.id != id || a.session != session || a.mailbox != mailbox || a.username != username || !time.Now().Before(a.expires) {
		return nil, fault.New(mail.OutlookReauthorize)
	}
	if a.err != nil {
		return nil, a.err
	}
	if a.token == nil {
		return nil, fault.New("outlook_pending")
	}
	return a.token, nil
}

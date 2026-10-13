package runtime

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/native"
	"github.com/mingzaily/mailwake/internal/settings"
)

type contentSource struct {
	multiSource
	text string
	read func(context.Context) (mail.Body, error)
}

func (s *contentSource) ReadContent(ctx context.Context, _ string, _ mail.Location) (mail.Body, error) {
	if s.read != nil {
		return s.read(ctx)
	}
	return mail.Body{Text: s.text}, nil
}

func TestContentReferenceFollowsOriginalAccount(t *testing.T) {
	for _, change := range []string{"label", "password", "host", "port", "username"} {
		t.Run(change, func(t *testing.T) {
			m, _, _ := managerFixture(t, func(config settings.Mailbox) mail.Source { return &contentSource{text: config.Identity()} })
			id, input := addMailbox(t, m, "imap.original.test", 3)
			subscribeMailbox(t, m, id)
			accountID := (settings.Mailbox{ID: id, Host: input.Host, Port: input.Port, Username: input.Username}).Identity()
			reference := signedContentReference(t, m, id, accountID)
			body, err := m.ReadContent(t.Context(), reference)
			if err != nil || body.Text != accountID {
				t.Fatalf("original body: %+v, %v", body, err)
			}
			input.Password = nil
			switch change {
			case "label":
				input.Label = ptr("Renamed")
			case "password":
				input.Password = ptr("new-password")
			case "host":
				input.Host = "imap.replacement.test"
			case "port":
				input.Port = 1993
			case "username":
				input.Username = "replacement@example.test"
			}
			if err := m.UpdateMailbox(t.Context(), id, input); err != nil {
				t.Fatal(err)
			}
			body, err = m.ReadContent(t.Context(), reference)
			if change == "label" || change == "password" {
				if err != nil || body.Text != accountID {
					t.Fatalf("same account body: %+v, %v", body, err)
				}
			} else if err == nil || fault.From(err, "").Code != "message_not_found" || body.Text != "" {
				t.Fatalf("replacement accepted old reference: %+v, %v", body, err)
			}
		})
	}
}

func signedContentReference(t *testing.T, m *Manager, id, accountID string) native.MessageReference {
	t.Helper()
	identity, err := m.native.CoreIdentity(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	reference := native.MessageReference{CoreID: identity.ID, MailboxID: id, AccountID: accountID, Folder: "INBOX", Location: mail.Location{UIDValidity: 1, UID: 7}}
	raw, _ := json.Marshal([]string{"mailwake-message-v1", reference.CoreID, id, accountID, "INBOX", "1", "7"})
	digest := sha256.Sum256(raw)
	r, s, err := ecdsa.Sign(rand.Reader, identity.Key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	reference.Signature = base64.RawURLEncoding.EncodeToString(signature)
	return reference
}

func TestContentReadDeadlineAndParentCancellation(t *testing.T) {
	for _, parentTimeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "twenty second read budget", true: "parent deadline"}[parentTimeout], func(t *testing.T) {
			m, _, _ := managerFixture(t, func(settings.Mailbox) mail.Source {
				return &contentSource{read: func(ctx context.Context) (mail.Body, error) {
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) > 20*time.Second {
						t.Fatal("read deadline missing")
					}
					if parentTimeout {
						<-ctx.Done()
						return mail.Body{}, ctx.Err()
					}
					if time.Until(deadline) < 19*time.Second {
						t.Fatal("read deadline unexpectedly short")
					}
					return mail.Body{}, fault.New("imap_command_timeout")
				}}
			})
			id, input := addMailbox(t, m, "imap.deadline.test", 3)
			subscribeMailbox(t, m, id)
			ref := signedContentReference(t, m, id, (settings.Mailbox{ID: id, Host: input.Host, Port: input.Port, Username: input.Username}).Identity())
			ctx := t.Context()
			if parentTimeout {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 30*time.Millisecond)
				defer cancel()
			}
			if _, err := m.ReadContent(ctx, ref); fault.From(err, "").Code != "message_timeout" {
				t.Fatalf("timeout: %v", err)
			}
		})
	}
}

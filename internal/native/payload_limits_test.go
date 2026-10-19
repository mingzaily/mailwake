package native

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mingzaily/mailwake/internal/coreidentity"
	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/mail"
)

func TestPayloadLimitsPreserveSenderAndSignedReference(t *testing.T) {
	longName := strings.Repeat("通知\"\\<😀", 1000)
	longSubject := strings.Repeat("账单\"\\<😀", 1000)
	for _, tc := range []struct {
		name       string
		senderName string
		subject    string
		oversized  bool
	}{
		{"long subject", "通知中心", longSubject, false},
		{"long name", longName, "账单", false},
		{"long name and subject", longName, longSubject, false},
		{"oversized fixed sender", "通知中心", "账单", true},
	} {
		for _, kind := range []string{"notification", "activity"} {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				v := vector(t)
				service, store := testService(t, func(w http.ResponseWriter, r *http.Request) {
					t.Error("payload construction contacted Relay")
					w.WriteHeader(500)
				})
				pairing := addDevice(t, store, "phone", v.Encryption.PublicKey)
				devices, err := store.NativeDevices(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				identity, err := service.identity.Load(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				n := event.Notification{
					ID: "mail-event", MailboxID: "mailbox", AccountID: "account-identity", Account: "工作", Folder: "Clients",
					Sender: "sender@example.org", SenderName: tc.senderName, Subject: tc.subject, Code: "482913",
					Location: &mail.Location{UIDValidity: 7, UID: 9}, ReceivedAt: time.Now(),
				}
				if tc.oversized {
					n.Sender = strings.Repeat("x", 2040) + "@example.org"
				}
				var raw []byte
				var decoded plaintext
				if kind == "notification" {
					ref, refErr := identity.MessageReference(n)
					if refErr != nil {
						t.Fatal(refErr)
					}
					raw, err = notificationPlainWithReference(n, ref)
					if err == nil {
						if decodeErr := json.Unmarshal(raw, &decoded); decodeErr != nil {
							t.Fatal(decodeErr)
						}
					}
				} else {
					activities, planErr := service.planActivities(n, devices, identity, v.Audience)
					err = planErr
					if err == nil {
						if len(activities) != 1 {
							t.Fatalf("activity count: %d", len(activities))
						}
						var request activityRequest
						if decodeErr := json.Unmarshal([]byte(activities[0].Request), &request); decodeErr != nil {
							t.Fatal(decodeErr)
						}
						context, marshalErr := json.Marshal([]any{"mailwake-activity-v1", pairing, n.ID, "start", n.ID, request.ReceivedAt, request.ExpiresAt, request.UpdatedAt})
						if marshalErr != nil {
							t.Fatal(marshalErr)
						}
						contextID := hash(context)
						if verifyErr := coreidentity.Verify(identity.PublicKey, request.Signature, contentText(v.Audience, pairing, contextID, request.Envelope)); verifyErr != nil {
							t.Fatal(verifyErr)
						}
						raw = decryptNotification(t, v, pairing, contextID, request.Envelope)
						var content activityContent
						if decodeErr := json.Unmarshal(raw, &content); decodeErr != nil {
							t.Fatal(decodeErr)
						}
						decoded = content.Notification
					}
				}
				if tc.oversized {
					var failure *delivery.Failure
					if !errors.As(err, &failure) || failure.Code != "native_payload_too_large" || failure.Retryable {
						t.Fatalf("fixed-field failure: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(raw) > 2032 || !utf8.Valid(raw) {
					t.Fatalf("payload budget or encoding: %d bytes", len(raw))
				}
				if decoded.V != 1 || decoded.Sender != n.Sender || decoded.MailboxID != n.MailboxID || decoded.Account != n.Account || decoded.Folder != n.Folder || decoded.Code != n.Code || decoded.ReceivedAt != n.ReceivedAt.UTC().Format(time.RFC3339Nano) {
					t.Fatalf("fixed fields changed: %+v", decoded)
				}
				ref := decoded.MessageRef
				if ref == nil || ref.CoreID != identity.ID || ref.MailboxID != n.MailboxID || ref.AccountID != n.AccountID || ref.Folder != n.Folder || ref.Location != *n.Location {
					t.Fatalf("message reference changed: %+v", ref)
				}
				if err := service.identity.VerifyMessageReference(t.Context(), *ref); err != nil {
					t.Fatal(err)
				}
				if decoded.SenderName == "" || !strings.HasPrefix(n.SenderName, decoded.SenderName) || !strings.HasPrefix(n.Subject, decoded.Subject) {
					t.Fatalf("truncated text is not an original prefix: %+v", decoded)
				}
				if tc.senderName == longName {
					if decoded.Subject != "" || len(decoded.SenderName) >= len(n.SenderName) {
						t.Fatal("long name must shorten after exhausting the subject")
					}
				} else if decoded.SenderName != n.SenderName || decoded.Subject == "" || len(decoded.Subject) >= len(n.Subject) {
					t.Fatal("long subject must shorten while preserving the name")
				}
			})
		}
	}
}

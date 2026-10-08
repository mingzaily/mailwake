package event

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/mingzaily/mailwake/internal/mail"
	"time"
)

type Notification struct {
	Location   *mail.Location `json:"location,omitempty"`
	Test       bool           `json:"test,omitempty"`
	ID         string         `json:"id"`
	MailboxID  string         `json:"mailbox_id"`
	Account    string         `json:"account"`
	Folder     string         `json:"folder"`
	Sender     string         `json:"sender"`
	Subject    string         `json:"subject"`
	Code       string         `json:"code,omitempty"`
	ReceivedAt time.Time      `json:"received_at"`
}

func ID(account, folder, messageKey string) string {
	b, _ := json.Marshal([3]string{account, folder, messageKey})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// TestID binds one preallocated App test to its controller and durable operation.
func TestID(controller, operation string) string {
	return "test_" + ID(controller, "mailwake-delivery-test-v1", operation)
}

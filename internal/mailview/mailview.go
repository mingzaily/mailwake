// Package mailview defines the minimal device-facing mail read contract.
package mailview

import (
	"time"

	"github.com/mingzaily/mailwake/internal/coreidentity"
)

const MaxPageSize = 100
const DefaultPageSize = 50
const MaxResponseBytes = 256 << 10

type Reader struct{ ControllerID, DeviceID string }

type Capabilities struct {
	CoreID      string            `json:"core_id"`
	MailHistory HistoryCapability `json:"mail_history"`
	MailHealth  HealthCapability  `json:"mail_health"`
	Permissions Permissions       `json:"permissions"`
}
type HistoryCapability struct {
	Version     int `json:"version"`
	WindowDays  int `json:"window_days"`
	MaxPageSize int `json:"max_page_size"`
}
type HealthCapability struct {
	Version int `json:"version"`
}
type Permissions struct {
	Content bool `json:"content"`
}

type Folder struct {
	MailboxID            string `json:"mailbox_id"`
	AccountID            string `json:"account_id"`
	MailboxLabel         string `json:"mailbox_label"`
	Folder               string `json:"folder"`
	FolderName           string `json:"folder_name,omitempty"`
	FolderRole           string `json:"folder_role,omitempty"`
	SubscriptionRevision int64  `json:"subscription_revision"`
}

type Page[T any] struct {
	CoreID     string     `json:"core_id"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
	Items      []T        `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
	Complete   bool       `json:"complete"`
}

type Health struct {
	Folder
	State         string     `json:"state"`
	ReasonCode    string     `json:"reason_code,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
	Check         string     `json:"check"`
	LastCheckedAt *time.Time `json:"last_checked_at,omitempty"`
}

type MessageRequest struct {
	MailboxID string `json:"mailbox_id"`
	AccountID string `json:"account_id"`
	Folder    string `json:"folder"`
	Limit     int    `json:"limit"`
	Cursor    string `json:"cursor,omitempty"`
}

type Message struct {
	Reference  *coreidentity.MessageReference `json:"message_ref"`
	Sender     string                         `json:"sender"`
	SenderName string                         `json:"sender_name,omitempty"`
	Subject    string                         `json:"subject"`
	ReceivedAt time.Time                      `json:"received_at"`
	IsRead     bool                           `json:"is_read"`
}

type Messages struct {
	Page[Message]
	MailboxID            string    `json:"mailbox_id"`
	AccountID            string    `json:"account_id"`
	Folder               string    `json:"folder"`
	SubscriptionRevision int64     `json:"subscription_revision"`
	WindowStart          time.Time `json:"window_start"`
	WindowEnd            time.Time `json:"window_end"`
	UIDValidity          uint32    `json:"uid_validity"`
	SnapshotMaxUID       uint32    `json:"snapshot_max_uid"`
}

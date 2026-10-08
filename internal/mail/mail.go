// Package mail defines the provider boundary used by the monitoring engine.
package mail

import (
	"context"
	"time"
)

// CodeAuthFailed marks credentials the provider rejected. Retrying with the same
// credentials cannot succeed soon and risks locking the account.
const CodeAuthFailed = "mail_auth_failed"

// Location is the provider identity within one subscribed folder.
type Location struct {
	UIDValidity uint32 `json:"uid_validity"`
	UID         uint32 `json:"uid"`
}

type Message struct {
	Location   *Location
	Key        string
	Sender     string
	Subject    string
	Code       string
	ReceivedAt time.Time
}

// Batch includes the checkpoint that becomes valid after Messages are persisted.
// Checkpoints and message keys are opaque to the engine and owned by the provider.
type Batch struct {
	Checkpoint string
	Messages   []Message
	More       bool
	Reset      bool
}

type Source interface {
	ReadContent(context.Context, string, Location) (Body, error)
	Folders(context.Context) ([]string, error)
	Open(context.Context, string) (Session, error)
}

// A session belongs to one folder and one monitoring goroutine.
type Session interface {
	Poll(context.Context, string, bool) (Batch, error)
	Wait(context.Context, time.Duration) error
	Mode() string
	Close() error
}

// ScheduledSource opens one connection for a round of sequential folder checks.
type ScheduledSource interface {
	OpenScheduled(context.Context) (FolderConnection, error)
}

// Select lends a session until the next Select. The caller closes the connection,
// rather than the individual borrowed sessions.
type FolderConnection interface {
	Select(context.Context, string) (Session, error)
	Close() error
}

// FolderError rejects one folder while leaving the shared connection usable.
type FolderError struct{ Err error }

func (e *FolderError) Error() string { return e.Err.Error() }
func (e *FolderError) Unwrap() error { return e.Err }

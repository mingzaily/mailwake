package imap

import (
	"context"
	"errors"
	"slices"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/logging"
	"github.com/mingzaily/mailwake/internal/mail"
)

// MarkRead verifies the current flags on an independent writable connection.
// Adding Seen preserves other flags and leaves watcher progress untouched.
func (s *Source) MarkRead(ctx context.Context, folder string, location mail.Location) error {
	logging.RequestStage(ctx, "imap_connect")
	x, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer x.Close()
	logging.RequestStage(ctx, "imap_select")
	mailbox, err := x.selectFolder(ctx, folder, false)
	if err != nil {
		return messageFolderError(err)
	}
	logging.RequestStage(ctx, "uidvalidity")
	if mailbox.validity != location.UIDValidity {
		return fault.New("message_not_found")
	}
	logging.RequestStage(ctx, "read_flags")
	seen, err := x.messageSeen(ctx, location.UID)
	if err != nil {
		return err
	}
	if seen {
		logging.RequestOutcome(ctx, "already_read")
		return nil
	}
	logging.RequestStage(ctx, "write_permission")
	// RFC 3501 permits all flags when PERMANENTFLAGS is omitted.
	if mailbox.permanentFlags != nil && !slices.Contains(mailbox.permanentFlags, protocol.FlagSeen) {
		return fault.New("message_unavailable")
	}
	logging.RequestStage(ctx, "store_seen")
	err = x.command(ctx, "message_unavailable", func() error {
		return x.client.Store(protocol.UIDSetNum(protocol.UID(location.UID)), &protocol.StoreFlags{Op: protocol.StoreFlagsAdd, Silent: true, Flags: []protocol.Flag{protocol.FlagSeen}}, nil).Close()
	})
	if err != nil {
		return err
	}
	// UID STORE may ignore a vanished UID and still return OK. FETCH establishes
	// both existence and the resulting flag independently of STORE update records.
	logging.RequestStage(ctx, "verify_seen")
	seen, err = x.messageSeen(ctx, location.UID)
	if err != nil {
		return err
	}
	if !seen {
		return fault.New("message_unavailable")
	}
	logging.RequestOutcome(ctx, "marked_read")
	return nil
}

func (x *connection) messageSeen(ctx context.Context, uid uint32) (bool, error) {
	target := protocol.UID(uid)
	messages, err := x.fetch(ctx, "message_unavailable", protocol.UIDSetNum(target), &protocol.FetchOptions{UID: true, Flags: true}, 0)
	if err != nil {
		return false, err
	}
	message := messages[target]
	if message == nil {
		return false, fault.New("message_not_found")
	}
	if message.Flags == nil {
		return false, fault.New("message_unavailable")
	}
	return slices.Contains(message.Flags, protocol.FlagSeen), nil
}

func messageFolderError(err error) error {
	var folderError *mail.FolderError
	if errors.As(err, &folderError) && folderError.Missing {
		return fault.New("message_not_found")
	}
	// Retain timeout/cancellation for the runtime's 504 mapping.
	return err
}

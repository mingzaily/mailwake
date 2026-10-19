package imap

import (
	"context"
	"errors"
	"slices"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
)

// MarkRead uses an independent writable connection and preserves all other flags.
func (s *Source) MarkRead(ctx context.Context, folder string, location mail.Location) error {
	x, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer x.Close()
	mailbox, err := x.selectFolder(ctx, folder, false)
	if err != nil {
		var folderError *mail.FolderError
		if errors.As(err, &folderError) {
			return fault.New("message_not_found")
		}
		return err
	}
	if mailbox.validity != location.UIDValidity {
		return fault.New("message_not_found")
	}
	// RFC 3501 section 6.3.1 permits all flags when PERMANENTFLAGS is omitted.
	if mailbox.permanentFlags != nil && !slices.Contains(mailbox.permanentFlags, protocol.FlagSeen) {
		return fault.New("message_unavailable")
	}
	found := false
	err = x.command(ctx, "message_unavailable", func() error {
		messages, err := x.client.Store(protocol.UIDSetNum(protocol.UID(location.UID)), &protocol.StoreFlags{Op: protocol.StoreFlagsAdd, Flags: []protocol.Flag{protocol.FlagSeen}}, nil).Collect()
		for _, message := range messages {
			if uint32(message.UID) == location.UID {
				found = true
			}
		}
		return err
	})
	if err != nil {
		return err
	}
	if !found {
		return fault.New("message_not_found")
	}
	return nil
}

package imap

import (
	"context"
	"errors"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/mingzaily/mailwake/internal/content"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
)

// ReadContent selects MIME representations and reads their parts within one shared budget.
func (s *Source) ReadContent(ctx context.Context, folder string, location mail.Location) (mail.Body, error) {
	x, err := s.connect(ctx)
	if err != nil {
		return mail.Body{}, err
	}
	defer x.Close()
	selected, err := x.Select(ctx, folder)
	if err != nil {
		var folderError *mail.FolderError
		if errors.As(err, &folderError) {
			return mail.Body{}, fault.New("message_not_found")
		}
		return mail.Body{}, err
	}
	if selected.(*session).validity != location.UIDValidity {
		return mail.Body{}, fault.New("message_not_found")
	}
	uid := protocol.UID(location.UID)
	messages, err := x.fetch(ctx, "message_unavailable", protocol.UIDSetNum(uid), &protocol.FetchOptions{BodyStructure: &protocol.FetchItemBodyStructure{Extended: true}}, 0)
	if err != nil {
		return mail.Body{}, err
	}
	item := messages[uid]
	if item == nil {
		return mail.Body{}, fault.New("message_not_found")
	}
	if item.BodyStructure == nil {
		return mail.Body{}, fault.New("message_unavailable")
	}
	structure := item.BodyStructure
	reader := contentParts{connection: x, uid: protocol.UID(location.UID), remaining: content.MaxReadBytes}
	root, err := reader.tree(ctx, structure, nil, 0)
	if err != nil {
		return mail.Body{}, err
	}
	body, err := content.ReadParts(ctx, root)
	if err != nil {
		return mail.Body{}, fault.From(err, "message_content_invalid")
	}
	return body, nil
}

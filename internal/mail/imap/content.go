package imap

import (
	"context"
	"errors"
	"io"
	"mime"
	"strings"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/mingzaily/mailwake/internal/content"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
)

// ReadContent owns a short-lived connection and fetches only the selected text part.
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
	var structure protocol.BodyStructure
	err = x.command(ctx, "message_unavailable", func() error {
		messages, err := x.client.Fetch(protocol.UIDSetNum(protocol.UID(location.UID)), &protocol.FetchOptions{BodyStructure: &protocol.FetchItemBodyStructure{Extended: true}, RFC822Size: true}).Collect()
		if err != nil {
			return err
		}
		if len(messages) == 0 {
			return fault.New("message_not_found")
		}
		if messages[0].RFC822Size > content.MaxMessageBytes {
			return fault.New("message_too_large")
		}
		structure = messages[0].BodyStructure
		return nil
	})
	if err != nil {
		return mail.Body{}, err
	}
	if structure == nil {
		return mail.Body{}, fault.New("message_content_invalid")
	}
	var part *protocol.BodyStructureSinglePart
	var path []int
	structure.Walk(func(candidatePath []int, node protocol.BodyStructure) bool {
		if d := node.Disposition(); d != nil && strings.EqualFold(d.Value, "attachment") {
			return false
		}
		single, ok := node.(*protocol.BodyStructureSinglePart)
		if !ok {
			return true
		}
		if single.Filename() != "" {
			return false
		}
		media := single.MediaType()
		if media == "text/plain" && (part == nil || part.MediaType() != "text/plain") || media == "text/html" && part == nil {
			part = single
			path = append([]int{}, candidatePath...)
		}
		return false
	})
	if part == nil {
		return mail.Body{}, nil
	}
	section := &protocol.FetchItemBodySection{Part: path, Peek: true, Partial: &protocol.SectionPartial{Size: content.MaxMessageBytes}}
	var raw []byte
	found := false
	err = x.command(ctx, "message_unavailable", func() error {
		cmd := x.client.Fetch(protocol.UIDSetNum(protocol.UID(location.UID)), &protocol.FetchOptions{BodySection: []*protocol.FetchItemBodySection{section}})
		defer cmd.Close()
		for data := cmd.Next(); data != nil; data = cmd.Next() {
			for item := data.Next(); item != nil; item = data.Next() {
				if body, ok := item.(imapclient.FetchItemDataBodySection); ok {
					var err error
					raw, err = io.ReadAll(io.LimitReader(body.Literal, content.MaxMessageBytes+1))
					if err != nil {
						return err
					}
					if len(raw) > content.MaxMessageBytes {
						x.conn.Close()
						return fault.New("message_too_large")
					}
					found = true
				}
			}
		}
		return cmd.Close()
	})
	if err != nil {
		return mail.Body{}, err
	}
	if !found {
		return mail.Body{}, fault.New("message_not_found")
	}
	encoding := strings.ToLower(part.Encoding)
	switch encoding {
	case "", "7bit", "8bit", "binary", "base64", "quoted-printable":
	default:
		return mail.Body{}, fault.New("message_content_invalid")
	}
	header := "Content-Type: " + mime.FormatMediaType(part.MediaType(), part.Params) + "\r\nContent-Transfer-Encoding: " + encoding + "\r\n\r\n"
	parsed, err := content.Extract(io.MultiReader(strings.NewReader(header), strings.NewReader(string(raw))), "")
	if err != nil {
		return mail.Body{}, fault.New("message_content_invalid")
	}
	return mail.Body{Text: parsed.Text, Truncated: parsed.Truncated}, nil
}

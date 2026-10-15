package imap

import (
	"context"
	"io"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/mingzaily/mailwake/internal/fault"
)

// fetch combines all responses for each UID through tagged command completion.
// Requested literals share one byte budget; unrelated body sections are drained.
func (x *connection) fetch(ctx context.Context, code string, set protocol.UIDSet, options *protocol.FetchOptions, budget int64) (map[protocol.UID]*imapclient.FetchMessageBuffer, error) {
	messages := make(map[protocol.UID]*imapclient.FetchMessageBuffer)
	err := x.command(ctx, code, func() error {
		cmd := x.client.Fetch(set, options)
		defer cmd.Close()
		for data := cmd.Next(); data != nil; data = cmd.Next() {
			part := &imapclient.FetchMessageBuffer{SeqNum: data.SeqNum}
			for item := data.Next(); item != nil; item = data.Next() {
				switch item := item.(type) {
				case imapclient.FetchItemDataUID:
					part.UID = item.UID
				case imapclient.FetchItemDataEnvelope:
					part.Envelope = item.Envelope
				case imapclient.FetchItemDataInternalDate:
					part.InternalDate = item.Time
				case imapclient.FetchItemDataRFC822Size:
					part.RFC822Size = item.Size
				case imapclient.FetchItemDataBodyStructure:
					part.BodyStructure = item.BodyStructure
				case imapclient.FetchItemDataBodySection:
					for _, section := range options.BodySection {
						if !item.MatchCommand(section) {
							continue
						}
						limit := budget
						if section.Partial != nil {
							limit = min(limit, section.Partial.Size)
						}
						if item.Literal != nil && item.Literal.Size() > limit {
							x.conn.Close()
							return fault.New("message_too_large")
						}
						var raw []byte
						if item.Literal != nil {
							var err error
							raw, err = io.ReadAll(io.LimitReader(item.Literal, limit+1))
							if err != nil {
								return err
							}
						}
						if int64(len(raw)) > limit {
							x.conn.Close()
							return fault.New("message_too_large")
						}
						budget -= int64(len(raw))
						part.BodySection = append(part.BodySection, imapclient.FetchBodySectionBuffer{Section: item.Section, Bytes: raw})
						break
					}
				}
			}
			if !set.Contains(part.UID) {
				return fault.New(code)
			}
			current := messages[part.UID]
			if current == nil {
				messages[part.UID] = part
				continue
			}
			if part.Envelope != nil {
				current.Envelope = part.Envelope
			}
			if !part.InternalDate.IsZero() {
				current.InternalDate = part.InternalDate
			}
			if part.RFC822Size != 0 {
				current.RFC822Size = part.RFC822Size
			}
			if part.BodyStructure != nil {
				current.BodyStructure = part.BodyStructure
			}
			current.BodySection = append(current.BodySection, part.BodySection...)
		}
		return cmd.Close()
	})
	if err != nil {
		return nil, err
	}
	return messages, nil
}

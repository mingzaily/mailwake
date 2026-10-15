package imap

import (
	"bytes"
	"context"
	"mime"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-message"
	"github.com/mingzaily/mailwake/internal/content"
	"github.com/mingzaily/mailwake/internal/fault"
)

// contentParts owns the current UID and a cumulative transfer-encoded read budget.
type contentParts struct {
	connection *connection
	uid        protocol.UID
	remaining  int
	nodes      int
}

func (r *contentParts) tree(ctx context.Context, structure protocol.BodyStructure, path []int, depth int) (*content.Part, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.nodes++
	if structure == nil || depth > 32 || r.nodes > 256 {
		return nil, fault.New("message_content_invalid")
	}
	part := &content.Part{}
	if disposition := structure.Disposition(); disposition != nil {
		if err := partHeader(part, "Content-Disposition", disposition.Value, disposition.Params); err != nil {
			return nil, err
		}
	}
	switch node := structure.(type) {
	case *protocol.BodyStructureSinglePart:
		if err := partHeader(part, "Content-Type", node.MediaType(), node.Params); err != nil {
			return nil, err
		}
		part.Header.Set("Content-ID", node.ID)
		part.Header.Set("Content-Transfer-Encoding", node.Encoding)
		if len(path) == 0 {
			path = []int{1}
		}
		sectionPath := append([]int(nil), path...)
		if node.Size > 0 {
			part.Read = func(ctx context.Context) ([]byte, error) {
				if uint64(node.Size) > uint64(r.remaining) {
					return nil, fault.New("message_too_large")
				}
				return r.fetch(ctx, &protocol.FetchItemBodySection{Part: sectionPath, Peek: true}, r.remaining)
			}
		}
	case *protocol.BodyStructureMultiPart:
		var params map[string]string
		if node.Extended != nil {
			params = node.Extended.Params
		}
		if err := partHeader(part, "Content-Type", node.MediaType(), params); err != nil {
			return nil, err
		}
		for i, child := range node.Children {
			childPath := append(append([]int(nil), path...), i+1)
			parsed, err := r.tree(ctx, child, childPath, depth+1)
			if err != nil {
				return nil, err
			}
			// BODYSTRUCTURE exposes IDs for single parts. Multipart related roots
			// carry their ID only in the MIME header, needed when start selects one.
			if node.MediaType() == "multipart/related" && params["start"] != "" {
				if _, multipart := child.(*protocol.BodyStructureMultiPart); multipart {
					header, err := r.fetch(ctx, &protocol.FetchItemBodySection{Part: childPath, Specifier: protocol.PartSpecifierMIME, Peek: true}, min(r.remaining, 64<<10))
					if err != nil {
						return nil, err
					}
					entity, err := message.Read(bytes.NewReader(header))
					if err != nil {
						return nil, fault.New("message_content_invalid")
					}
					parsed.Header.Set("Content-ID", entity.Header.Get("Content-ID"))
				}
			}
			part.Children = append(part.Children, parsed)
		}
	default:
		return nil, fault.New("message_content_invalid")
	}
	return part, nil
}

func (r *contentParts) fetch(ctx context.Context, section *protocol.FetchItemBodySection, limit int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	section.Partial = &protocol.SectionPartial{Size: int64(limit) + 1}
	items, err := r.connection.fetch(ctx, "message_unavailable", protocol.UIDSetNum(r.uid), &protocol.FetchOptions{BodySection: []*protocol.FetchItemBodySection{section}}, int64(limit)+1)
	if err != nil {
		return nil, err
	}
	item := items[r.uid]
	if item == nil {
		return nil, fault.New("message_not_found")
	}
	raw := item.FindBodySection(section)
	if raw == nil {
		return nil, fault.New("message_unavailable")
	}
	if len(raw) > limit {
		return nil, fault.New("message_too_large")
	}
	r.remaining -= len(raw)
	return raw, nil
}

// Format server-provided MIME metadata once, preserving format failures as errors.
func partHeader(part *content.Part, field, value string, params map[string]string) error {
	formatted := mime.FormatMediaType(value, params)
	if formatted == "" {
		return fault.New("message_content_invalid")
	}
	part.Header.Set(field, formatted)
	return nil
}

package content

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	mimeMessage "github.com/emersion/go-message"
	"github.com/mingzaily/mailwake/internal/mail"
)

const (
	MaxReadBytes     = 1 << 20
	MaxHTMLBytes     = 256 << 10
	MaxResponseBytes = 512 << 10
)

var errInvalidBody = errors.New("message_content_invalid")

// bodyPart preserves MIME relationships so related resources stay with their presentation.
type bodyPart struct {
	media, id, start string
	attachment       bool
	data             []byte
	children         []*bodyPart
	load             func(context.Context) error
}

type presentation struct {
	plain, html *bodyPart
	resources   map[string]*bodyPart
}

// ReadBody parses a bounded in-memory RFC 5322 input. IMAP uses ReadParts;
// notification extraction uses Extract.
// Every reader is supplied by the caller; image references never cause I/O.
func ReadBody(ctx context.Context, raw io.Reader) (mail.Body, error) {
	data, err := io.ReadAll(io.LimitReader(raw, MaxMessageBytes+1))
	if err != nil {
		return mail.Body{}, err
	}
	if len(data) > MaxMessageBytes {
		return mail.Body{}, errors.New("message_too_large")
	}
	entity, decodingError := mimeMessage.Read(bytes.NewReader(data))
	count := 0
	root, err := readBodyPart(ctx, entity, decodingError, 0, &count)
	if err != nil {
		return mail.Body{}, err
	}
	return renderBody(ctx, root)
}

func renderBody(ctx context.Context, root *bodyPart) (mail.Body, error) {
	display := root.presentation()
	for _, part := range []*bodyPart{display.plain, display.html} {
		if part != nil && part.load != nil {
			if err := part.load(ctx); err != nil {
				return mail.Body{}, err
			}
		}
	}
	plain, source := "", ""
	if display.plain != nil {
		plain = string(display.plain.data)
	}
	if display.html != nil {
		source = string(display.html.data)
	}

	cleaned, remote, oversized, err := sanitizeBody(ctx, source, display.resources)
	if err != nil {
		return mail.Body{}, err
	}
	text := plain
	if strings.TrimSpace(text) == "" {
		// The tokenizer also works when HTML exceeds its presentation budget.
		text = htmlToText(source)
	}
	text = normalize(text)
	body := mail.Body{Text: clip(text, MaxTextBytes), HTML: cleaned, HasRemoteImages: remote, Truncated: len(text) > MaxTextBytes || oversized}
	encoded, err := json.Marshal(body)
	if err != nil {
		return mail.Body{}, err
	}
	// Measure the same HTML-escaping JSON encoding used by the HTTP response.
	if len(encoded) > MaxResponseBytes {
		body.HTML, body.HasRemoteImages, body.Truncated = "", false, true
	}
	if err := ctx.Err(); err != nil {
		return mail.Body{}, err
	}
	return body, nil
}

func readBodyPart(ctx context.Context, e *mimeMessage.Entity, decodingError error, depth int, count *int) (*bodyPart, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	*count += 1
	if e == nil || depth > 32 || *count > 256 {
		return nil, errInvalidBody
	}
	p, err := describeBodyPart(e.Header)
	if err != nil {
		return nil, err
	}
	imagePart := strings.HasPrefix(p.media, "image/")
	if p.attachment && !imagePart {
		return p, nil
	}
	if decodingError != nil {
		// An invalid image is an unavailable CID resource; display text stays readable.
		if imagePart {
			return p, nil
		}
		return nil, errInvalidBody
	}

	if mr := e.MultipartReader(); mr != nil {
		for {
			child, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			parsed, err := readBodyPart(ctx, child, err, depth+1, count)
			if err != nil {
				return nil, err
			}
			p.children = append(p.children, parsed)
		}
	} else if p.media == "text/plain" || p.media == "text/html" || imagePart {
		if err := p.decode(e.Body, decodingError); err != nil {
			return nil, err
		}
	}

	return p, nil
}

func (p *bodyPart) presentation() presentation {
	if p.attachment {
		return presentation{}
	}
	switch p.media {
	case "text/plain":
		if p.load == nil && len(p.data) == 0 {
			return presentation{}
		}
		return presentation{plain: p}
	case "text/html":
		if p.load == nil && len(p.data) == 0 {
			return presentation{}
		}
		return presentation{html: p}
	case "multipart/alternative":
		var chosen presentation
		for _, child := range p.children {
			candidate := child.presentation()
			// RFC 2046 orders alternatives from least to most faithful.
			if candidate.plain != nil {
				chosen.plain = candidate.plain
			}
			if candidate.html != nil {
				chosen.html, chosen.resources = candidate.html, candidate.resources
			}
		}
		return chosen
	case "multipart/related":
		if len(p.children) == 0 {
			return presentation{}
		}
		root := p.children[0]
		if p.start != "" {
			root = nil
			for _, child := range p.children {
				if child.id == p.start {
					if root != nil {
						return presentation{}
					}
					root = child
				}
			}
			if root == nil {
				return presentation{}
			}
		}
		chosen := root.presentation()
		resources := map[string]*bodyPart{}
		for _, child := range p.children {
			if child != root && child.id != "" && strings.HasPrefix(child.media, "image/") {
				if _, exists := resources[child.id]; exists {
					resources[child.id] = nil
				} else {
					resources[child.id] = child
				}
			}
		}
		// A nested related presentation owns its own CID namespace.
		if chosen.resources == nil {
			chosen.resources = resources
		}
		return chosen
	default:
		for _, child := range p.children {
			if candidate := child.presentation(); candidate.plain != nil || candidate.html != nil {
				return candidate
			}
		}
		return presentation{}
	}
}

func contentID(value string) string { return strings.Trim(strings.TrimSpace(value), "<>") }

// Part describes a MIME node. Read supplies only this leaf's transfer-encoded bytes
// from the current message; the adapter owns the shared transport read budget.
type Part struct {
	Header   mimeMessage.Header
	Children []*Part
	Read     func(context.Context) ([]byte, error)
}

// ReadParts accepts the adapter's bounded MIME tree, selects display representations and loads referenced
// CID resources lazily. Ordinary attachments and unselected alternatives stay unread.
func ReadParts(ctx context.Context, root *Part) (mail.Body, error) {
	parsed, err := preparePart(ctx, root)
	if err != nil {
		return mail.Body{}, err
	}
	return renderBody(ctx, parsed)
}

func preparePart(ctx context.Context, part *Part) (*bodyPart, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p, err := describeBodyPart(part.Header)
	if err != nil {
		return nil, err
	}
	for _, child := range part.Children {
		parsed, err := preparePart(ctx, child)
		if err != nil {
			return nil, err
		}
		p.children = append(p.children, parsed)
	}
	if part.Read != nil {
		p.load = func(ctx context.Context) error {
			raw, err := part.Read(ctx)
			if err != nil {
				return err
			}
			entity, decodingError := mimeMessage.New(part.Header, bytes.NewReader(raw))
			return p.decode(entity.Body, decodingError)
		}
	}

	return p, nil
}

func (p *bodyPart) decode(body io.Reader, decodingError error) error {
	imagePart := strings.HasPrefix(p.media, "image/")
	if decodingError != nil {
		if imagePart {
			return nil
		}
		return errInvalidBody
	}
	// Charset conversion can expand transfer-encoded text into UTF-8.
	decoded, err := io.ReadAll(io.LimitReader(body, 4*MaxReadBytes+1))
	if err != nil || len(decoded) > 4*MaxReadBytes {
		if imagePart {
			return nil
		}
		return errInvalidBody
	}
	if strings.HasPrefix(p.media, "text/") && !utf8.Valid(decoded) {
		return errInvalidBody
	}
	p.data = decoded
	return nil
}

func describeBodyPart(header mimeMessage.Header) (*bodyPart, error) {
	media, params, err := header.ContentType()
	if err != nil {
		return nil, errInvalidBody
	}
	disposition, dp, err := header.ContentDisposition()
	if header.Get("Content-Disposition") != "" && err != nil {
		return nil, errInvalidBody
	}
	p := &bodyPart{media: strings.ToLower(media), id: contentID(header.Get("Content-ID")), start: contentID(params["start"]), attachment: strings.EqualFold(disposition, "attachment") || dp["filename"] != "" || params["name"] != ""}
	return p, nil
}

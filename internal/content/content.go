// Package content turns a raw RFC 5322 message into readable text, a short excerpt and a
// verification code. It reads only what it is given; fetching and storage stay with callers.
package content

import (
	"errors"
	"io"
	"mime"
	"strings"
	"unicode/utf8"

	_ "github.com/emersion/go-message/charset" // registers legacy charsets such as GBK and Big5
	"github.com/emersion/go-message/mail"
)

// Limits keep one message bounded in memory and in the encrypted push payload.
const (
	MaxMessageBytes = 1 << 20
	MaxTextBytes    = 64 << 10
	ExcerptBytes    = 600
)

type Result struct {
	Truncated bool
	// Text is the readable body: text/plain when present, otherwise HTML converted to text.
	Text string
	// Excerpt is the start of Text with whitespace collapsed, at most ExcerptBytes.
	Excerpt string
	// Code is a detected verification code, or empty.
	Code string
}

// Extract parses a message and derives its readable text, excerpt and verification code.
// The subject takes part in code detection because many providers put the code there.
func Extract(raw io.Reader, subject string) (Result, error) {
	text, err := readableText(io.LimitReader(raw, MaxMessageBytes))
	if err != nil {
		return Result{}, err
	}
	truncated := len(text) > MaxTextBytes
	text = clip(text, MaxTextBytes)
	return Result{Truncated: truncated, Text: text, Excerpt: excerpt(text, ExcerptBytes), Code: DetectCode(subject, text)}, nil
}

func readableText(raw io.Reader) (string, error) {
	r, err := mail.CreateReader(raw)
	if err != nil {
		return "", err
	}
	var plain, html string
	for {
		part, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		inline, ok := part.Header.(*mail.InlineHeader)
		if !ok {
			continue
		}
		mediaType, _, _ := mime.ParseMediaType(inline.Get("Content-Type"))
		if mediaType == "" {
			mediaType = "text/plain"
		}
		if mediaType != "text/plain" && mediaType != "text/html" {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(part.Body, MaxMessageBytes))
		if err != nil {
			return "", err
		}
		if !utf8.Valid(body) {
			return "", errors.New("message_encoding_invalid")
		}
		decoded := string(body)
		switch {
		case mediaType == "text/plain" && plain == "":
			plain = decoded
		case mediaType == "text/html" && html == "":
			html = decoded
		}
	}
	text := plain
	if strings.TrimSpace(text) == "" {
		text = htmlToText(html)
	}
	return normalize(text), nil
}

// normalize unifies line endings, trims trailing spaces and collapses runs of blank lines.
func normalize(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimRight(line, " \t ")
		if strings.TrimSpace(line) == "" {
			if !blank && len(out) > 0 {
				out = append(out, "")
			}
			blank = true
			continue
		}
		out = append(out, line)
		blank = false
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func excerpt(text string, limit int) string {
	return clip(strings.Join(strings.Fields(text), " "), limit)
}

// clip limits UTF-8 bytes without splitting a character.
func clip(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	end := 0
	for i := range s {
		if i > limit-3 {
			break
		}
		end = i
	}
	return s[:end] + "…"
}

package delivery

import (
	"strings"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/i18n"
)

// Message is the rendered notification shared by every channel.
type Message struct {
	Title string
	Body  string
}

// Render displays the stored subject; structured sender metadata stays in Webhook.
func Render(language string, n event.Notification) Message {
	if n.Test {
		return Message{Title: i18n.Message(language, "notification.test_title", nil), Body: i18n.Message(language, "notification.test_body", nil)}
	}
	folder := n.Folder[strings.LastIndex(n.Folder, "/")+1:]
	m := Message{
		Title: i18n.Message(language, "notification.title", map[string]string{"account": n.Account, "folder": Clip(folder, 150)}),
		Body:  i18n.Message(language, "notification.new_mail", nil),
	}
	if n.Subject != "" {
		m.Body = Clip(n.Subject, 600)
	}
	return m
}

// Clip limits UTF-8 bytes without splitting a character, keeping payloads within provider limits.
func Clip(s string, limit int) string {
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

package delivery

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/i18n"
)

func TestRenderFollowsStoredPreview(t *testing.T) {
	for _, locale := range []string{"en", "zh-CN"} {
		plain := Render(locale, event.Notification{Account: "工作邮箱", MailboxID: "mbx_work", Folder: "客户"})
		if plain.Title != "工作邮箱 · 客户" || plain.Body != i18n.Message(locale, "notification.new_mail", nil) {
			t.Fatalf("%s without preview: %+v", locale, plain)
		}
		test := Render(locale, event.Notification{Test: true, Sender: "ignored"})
		if test.Title != i18n.Message(locale, "notification.test_title", nil) || test.Body != i18n.Message(locale, "notification.test_body", nil) {
			t.Fatalf("%s test notification: %+v", locale, test)
		}
	}
	if m := Render("en", event.Notification{Subject: "Only subject"}); m.Body != "Only subject" {
		t.Fatalf("subject without sender must not start with a blank line: %q", m.Body)
	}
	if m := Render("en", event.Notification{Sender: "a@example.org", Subject: "Hello"}); m.Body != "Hello" {
		t.Fatalf("sender and subject: %q", m.Body)
	}
}

func TestRenderKeepsUTF8WithinLimits(t *testing.T) {
	m := Render("en", event.Notification{Folder: strings.Repeat("文件夹", 100), Sender: strings.Repeat("发件人", 100), Subject: strings.Repeat("主题", 1000)})
	if !utf8.ValidString(m.Title) || !utf8.ValidString(m.Body) || len(m.Title)+len(m.Body) > 1000 {
		t.Fatalf("length or encoding: %d %d", len(m.Title), len(m.Body))
	}
}

func TestParseRetryAfterInvalidAndOverflow(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"", 0}, {"-1", 0}, {"invalid", 0}, {"0", 0},
		{"Wed, 21 Oct 2015 07:28:00 GMT", 0},
		{"9223372037", time.Duration(1<<63 - 1)},
		{"18446744073709551616", time.Duration(1<<63 - 1)},
	} {
		if got := ParseRetryAfter(tc.value); got != tc.want {
			t.Errorf("Retry-After %q: got %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestMailboxLabelIsPreservedInBothLanguages(t *testing.T) {
	label := strings.Repeat("邮", 64)
	for _, language := range []string{"en", "zh-CN"} {
		got := Render(language, event.Notification{MailboxID: "mbx_example", Account: label, Folder: "INBOX"})
		if got.Title != label+" · INBOX" {
			t.Fatal("display name was truncated or replaced", got.Title)
		}
	}
}

func TestReminderTextAndSenderExclusion(t *testing.T) {
	for language, want := range map[string]string{"en": "New mail", "zh-CN": "收到新邮件"} {
		for _, n := range []event.Notification{{}, {Sender: "hidden@example.test"}} {
			if got := Render(language, n).Body; got != want {
				t.Fatalf("reminder = %q", got)
			}
		}
	}
}

func TestRenderUsesLeafFolderName(t *testing.T) {
	for _, folder := range []string{"其他文件夹/CN区域", "Parent/Child/CN区域", "CN区域"} {
		for _, locale := range []string{"en", "zh-CN"} {
			n := event.Notification{Account: "工作邮箱", Folder: folder}
			if got := Render(locale, n).Title; got != "工作邮箱 · CN区域" {
				t.Fatalf("%s: %q", folder, got)
			}
			if n.Folder != folder {
				t.Fatal("notification must retain its original folder path")
			}
		}
	}
}

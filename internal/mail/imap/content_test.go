package imap

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/mingzaily/mailwake/internal/content"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestReadContentSelectsTextAndPreservesUnread(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
		truncated       bool
	}{
		{"plain", "Content-Type: text/plain\r\n\r\nhello", "hello", false},
		{"html", "Content-Type: text/html\r\n\r\n<p>Hello</p><ul><li>one</li></ul><a href=\"https://example.com\">site</a>", "https://example.com", false},
		{"truncated", "Content-Type: text/plain\r\n\r\n" + strings.Repeat("a", 70000), "aaa", true},
		{"attachments", "Content-Type: multipart/mixed; boundary=x\r\n\r\n--x\r\nContent-Type: text/plain\r\n\r\nhello\r\n--x\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=secret.txt\r\n\r\nattachment secret\r\n--x--", "hello", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, user := fixture(t)
			added, err := user.Append("Clients", strings.NewReader(tc.raw), &protocol.AppendOptions{})
			if err != nil {
				t.Fatal(err)
			}
			body, err := source.ReadContent(t.Context(), "Clients", mail.Location{UIDValidity: added.UIDValidity, UID: uint32(added.UID)})
			if err != nil || !strings.Contains(body.Text, tc.want) || body.Truncated != tc.truncated || strings.Contains(body.Text, "attachment secret") {
				t.Fatalf("body length=%d truncated=%v error=%v", len(body.Text), body.Truncated, err)
			}
			opened, err := source.Open(t.Context(), "Clients")
			if err != nil {
				t.Fatal(err)
			}
			defer opened.Close()
			messages, err := opened.(*session).client.Fetch(protocol.UIDSetNum(added.UID), &protocol.FetchOptions{Flags: true}).Collect()
			if err != nil || len(messages) != 1 || slices.Contains(messages[0].Flags, protocol.FlagSeen) {
				t.Fatal("read modified flags", err)
			}
		})
	}
}
func TestReadContentRejectsMissingEpochOversizeAndMalformed(t *testing.T) {
	source, user := fixture(t)
	for _, tc := range []struct{ raw, code string }{
		{"Content-Type: text/plain\r\n\r\n" + strings.Repeat("x", (1<<20)+1), "message_too_large"},
		{"Content-Type: text/plain\r\nContent-Transfer-Encoding: base64\r\n\r\n%%%", "message_content_invalid"},
	} {
		added, err := user.Append("Clients", strings.NewReader(tc.raw), &protocol.AppendOptions{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = source.ReadContent(t.Context(), "Clients", mail.Location{UIDValidity: added.UIDValidity, UID: uint32(added.UID)})
		if fault.From(err, "").Code != tc.code {
			t.Fatalf("wanted %s got %v", tc.code, err)
		}
		for _, location := range []mail.Location{{UIDValidity: added.UIDValidity + 1, UID: uint32(added.UID)}, {UIDValidity: added.UIDValidity, UID: 9999}} {
			_, err := source.ReadContent(t.Context(), "Clients", location)
			if fault.From(err, "").Code != "message_not_found" {
				t.Fatal(err)
			}
		}
	}
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := source.ReadContent(ctx, "Clients", mail.Location{UIDValidity: 1, UID: 1}); err == nil {
		t.Fatal("expired request succeeded")
	}
}

func TestReadContentTracksOriginalFolderAcrossMoveAndDeletion(t *testing.T) {
	source, user := fixture(t)
	added, err := user.Append("Clients", strings.NewReader("Content-Type: text/plain\r\n\r\noriginal"), &protocol.AppendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := user.Create("Archive", nil); err != nil {
		t.Fatal(err)
	}
	writer, err := source.connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.client.Select("Clients", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.client.Move(protocol.UIDSetNum(added.UID), "Archive").Wait(); err != nil {
		t.Fatal(err)
	}
	location := mail.Location{UIDValidity: added.UIDValidity, UID: uint32(added.UID)}
	if _, err := source.ReadContent(t.Context(), "Clients", location); fault.From(err, "").Code != "message_not_found" {
		t.Fatal(err)
	}
	if err := user.Delete("Clients"); err != nil {
		t.Fatal(err)
	}
	if _, err := source.ReadContent(t.Context(), "Clients", location); fault.From(err, "").Code != "message_not_found" {
		t.Fatal(err)
	}
}

func TestReadContentCancelsAStalledFetch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	source, user := fixtureWithFetch(t, func(_ *protocol.FetchOptions) { cancel() })
	added, err := user.Append("Clients", strings.NewReader("Content-Type: text/plain\r\n\r\nhello"), &protocol.AppendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.ReadContent(ctx, "Clients", mail.Location{UIDValidity: added.UIDValidity, UID: uint32(added.UID)}); err == nil {
		t.Fatal("cancelled fetch succeeded")
	}
}

func TestReadContentRetainsMIMEHTMLWithoutLoadingImagesOrMarkingRead(t *testing.T) {
	var requests atomic.Int32
	images := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(204) }))
	defer images.Close()
	source, user := fixture(t)
	raw := "Content-Type: multipart/alternative; boundary=a\r\n\r\n--a\r\nContent-Type: text/plain\r\n\r\nFallback\r\n--a\r\nContent-Type: text/html\r\n\r\n<p>HTML</p><img src=\"" + images.URL + "/image\">\r\n--a--"
	added, err := user.Append("Clients", strings.NewReader(raw), &protocol.AppendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	body, err := source.ReadContent(t.Context(), "Clients", mail.Location{UIDValidity: added.UIDValidity, UID: uint32(added.UID)})
	if err != nil || body.Text != "Fallback" || !strings.Contains(body.HTML, "<p>HTML</p>") || !body.HasRemoteImages {
		t.Fatalf("%+v %v", body, err)
	}
	if _, err := json.Marshal(body); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatal("Core fetched an image")
	}
	opened, err := source.Open(t.Context(), "Clients")
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	messages, err := opened.(*session).client.Fetch(protocol.UIDSetNum(added.UID), &protocol.FetchOptions{Flags: true}).Collect()
	if err != nil || len(messages) != 1 || slices.Contains(messages[0].Flags, protocol.FlagSeen) {
		t.Fatal("read modified flags", err)
	}
}

func contentMultipart(kind, boundary string, parts ...string) string {
	return "Content-Type: multipart/" + kind + "; boundary=" + boundary + "\r\n\r\n--" + boundary + "\r\n" + strings.Join(parts, "\r\n--"+boundary+"\r\n") + "\r\n--" + boundary + "--\r\n"
}

func contentPNG(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b.Bytes())
}

func TestReadContentSkipsLargeAttachmentsAndUnusedPresentations(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// This mailbox refuses reads of the PDF, unused alternative and unrelated image.
	// Successful reading therefore exercises the part-selection privacy boundary.
	source, user := fixtureWithFetch(t, func(options *protocol.FetchOptions) {
		for _, section := range options.BodySection {
			if len(section.Part) == 0 || section.Part[0] != 1 || (len(section.Part) > 1 && section.Part[1] != 2) {
				cancel()
			}
		}
	})
	raw := contentMultipart("mixed", "m",
		contentMultipart("alternative", "a", "Content-Type: text/html\r\n\r\n"+strings.Repeat("old", 400000), "Content-Type: text/html\r\n\r\n<p>Selected HTML</p>"),
		"Content-Type: application/pdf\r\nContent-Disposition: attachment; filename=large.pdf\r\n\r\n"+strings.Repeat("attachment", 500000),
		"Content-Type: image/png\r\nContent-ID: <unused>\r\n\r\n"+strings.Repeat("unused", 300000))
	added, err := user.Append("Clients", strings.NewReader(raw), &protocol.AppendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	body, err := source.ReadContent(ctx, "Clients", mail.Location{UIDValidity: added.UIDValidity, UID: uint32(added.UID)})
	if err != nil || body.Text != "Selected HTML" || !strings.Contains(body.HTML, "<p>Selected HTML</p>") {
		t.Fatalf("%+v %v", body, err)
	}
	opened, err := source.Open(t.Context(), "Clients")
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	messages, err := opened.(*session).client.Fetch(protocol.UIDSetNum(added.UID), &protocol.FetchOptions{Flags: true}).Collect()
	if err != nil || len(messages) != 1 || slices.Contains(messages[0].Flags, protocol.FlagSeen) {
		t.Fatal("large attachment read modified flags", err)
	}
}

func TestReadContentLoadsOnlyReferencedRelatedImages(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	source, user := fixtureWithFetch(t, func(options *protocol.FetchOptions) {
		for _, section := range options.BodySection {
			if len(section.Part) == 0 || section.Part[0] == 3 {
				cancel()
			}
		}
	})
	raw := contentMultipart("related", "r",
		contentMultipart("alternative", "a", "Content-Type: text/plain\r\n\r\nFallback", "Content-Type: text/html\r\n\r\n"+`<picture><source srcset="cid:logo 1x"><img src="cid:logo" alt="logo"></picture><div style="background-image:url(cid:logo)"></div>`),
		"Content-Type: image/png\r\nContent-ID: <logo>\r\nContent-Transfer-Encoding: base64\r\n\r\n"+contentPNG(t),
		"Content-Type: image/png\r\nContent-ID: <unused>\r\n\r\n"+strings.Repeat("unused", 300000))
	added, err := user.Append("Clients", strings.NewReader(raw), &protocol.AppendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	body, err := source.ReadContent(ctx, "Clients", mail.Location{UIDValidity: added.UIDValidity, UID: uint32(added.UID)})
	if err != nil || body.Text != "Fallback" || strings.Count(body.HTML, "data:image/png;base64,") != 3 || body.HasRemoteImages {
		t.Fatalf("%+v %v", body, err)
	}
}

func TestReadContentHonorsRelatedStartOnMultipartRoot(t *testing.T) {
	source, user := fixture(t)
	root := "Content-ID: <root>\r\n" + contentMultipart("alternative", "a", "Content-Type: text/plain\r\n\r\nFallback", "Content-Type: text/html\r\n\r\n"+`<p>Selected root</p><img src="cid:logo">`)
	raw := strings.Replace(contentMultipart("related", "r", "Content-Type: image/png\r\nContent-ID: <logo>\r\nContent-Transfer-Encoding: base64\r\n\r\n"+contentPNG(t), root), "boundary=r", "boundary=r; start=\"<root>\"", 1)
	added, err := user.Append("Clients", strings.NewReader(raw), &protocol.AppendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	body, err := source.ReadContent(t.Context(), "Clients", mail.Location{UIDValidity: added.UIDValidity, UID: uint32(added.UID)})
	if err != nil || body.Text != "Fallback" || !strings.Contains(body.HTML, "data:image/png;base64,") {
		t.Fatalf("%+v %v", body, err)
	}
}

func TestReadContentSharesBudgetAcrossTextHTMLAndCID(t *testing.T) {
	for _, extra := range []int{0, 1} {
		t.Run(map[int]string{0: "at limit", 1: "over limit"}[extra], func(t *testing.T) {
			source, user := fixture(t)
			encoded := contentPNG(t)
			html := `<p>HTML</p><img src="cid:logo">`
			plain := strings.Repeat("x", content.MaxReadBytes-len(html)-len(encoded)+extra)
			raw := contentMultipart("related", "r", contentMultipart("alternative", "a", "Content-Type: text/plain\r\n\r\n"+plain, "Content-Type: text/html\r\n\r\n"+html), "Content-Type: image/png\r\nContent-ID: <logo>\r\nContent-Transfer-Encoding: base64\r\n\r\n"+encoded)
			added, err := user.Append("Clients", strings.NewReader(raw), &protocol.AppendOptions{})
			if err != nil {
				t.Fatal(err)
			}
			body, err := source.ReadContent(t.Context(), "Clients", mail.Location{UIDValidity: added.UIDValidity, UID: uint32(added.UID)})
			if extra == 1 {
				if fault.From(err, "").Code != "message_too_large" {
					t.Fatalf("budget accepted: %v", err)
				}
				return
			}
			if err != nil || !body.Truncated || len(body.Text) > content.MaxTextBytes || !strings.Contains(body.HTML, "data:image/png;base64,") {
				t.Fatalf("exact budget rejected: %v html=%d", err, len(body.HTML))
			}
		})
	}
}

func TestReadContentSkipsCIDReferencesInDiscardedMarkup(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	source, user := fixtureWithFetch(t, func(options *protocol.FetchOptions) {
		for _, section := range options.BodySection {
			if len(section.Part) == 0 || section.Part[0] != 1 {
				cancel()
			}
		}
	})
	raw := contentMultipart("related", "r", "Content-Type: text/html\r\n\r\n"+`<p>Readable</p><script><img src="cid:unused"></script><div style="background:url(cid:unused) expression(bad)"></div>`, "Content-Type: image/png\r\nContent-ID: <unused>\r\n\r\n"+strings.Repeat("unused", 300000))
	added, err := user.Append("Clients", strings.NewReader(raw), &protocol.AppendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	body, err := source.ReadContent(ctx, "Clients", mail.Location{UIDValidity: added.UIDValidity, UID: uint32(added.UID)})
	if err != nil || body.Text != "Readable" || strings.Contains(body.HTML, "cid:") {
		t.Fatalf("discarded reference read resource: %+v %v", body, err)
	}
}

func TestReadContentCancelsDuringPartRead(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	source, user := fixtureWithFetch(t, func(options *protocol.FetchOptions) {
		if len(options.BodySection) > 0 {
			cancel()
		}
	})
	added, err := user.Append("Clients", strings.NewReader("Content-Type: text/html\r\n\r\n<p>hello</p>"), &protocol.AppendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	body, err := source.ReadContent(ctx, "Clients", mail.Location{UIDValidity: added.UIDValidity, UID: uint32(added.UID)})
	if err == nil || body.Text != "" || body.HTML != "" {
		t.Fatal("cancelled part returned content")
	}
}

func TestReadContentDecodesSelectedPartsAndDropsInvalidCID(t *testing.T) {
	source, user := fixture(t)
	encoded, err := simplifiedchinese.GBK.NewEncoder().String(`<p>中文正文</p><img src="cid:broken" alt="image">`)
	if err != nil {
		t.Fatal(err)
	}
	raw := contentMultipart("related", "r", contentMultipart("alternative", "a",
		"Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n"+base64.StdEncoding.EncodeToString([]byte("Fallback")),
		"Content-Type: text/html; charset=GBK\r\nContent-Transfer-Encoding: base64\r\n\r\n"+base64.StdEncoding.EncodeToString([]byte(encoded))),
		"Content-Type: image/png\r\nContent-ID: <broken>\r\nContent-Transfer-Encoding: base64\r\n\r\n%%%")
	added, err := user.Append("Clients", strings.NewReader(raw), &protocol.AppendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	body, err := source.ReadContent(t.Context(), "Clients", mail.Location{UIDValidity: added.UIDValidity, UID: uint32(added.UID)})
	if err != nil || body.Text != "Fallback" || !strings.Contains(body.HTML, "中文正文") || strings.Contains(body.HTML, "src=") {
		t.Fatalf("%+v %v", body, err)
	}
}

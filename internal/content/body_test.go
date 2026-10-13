package content

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"mime/quotedprintable"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mingzaily/mailwake/internal/mail"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func readBody(t *testing.T, raw string) mail.Body {
	t.Helper()
	body, err := ReadBody(t.Context(), strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return body
}
func mimePart(headers, body string) string { return headers + "\r\n\r\n" + body }
func multipart(kind, boundary string, parts ...string) string {
	return mimePart("Content-Type: multipart/"+kind+"; boundary="+boundary, "--"+boundary+"\r\n"+strings.Join(parts, "\r\n--"+boundary+"\r\n")+"\r\n--"+boundary+"--\r\n")
}
func htmlMessage(source string) string {
	return mimePart("Content-Type: text/html; charset=utf-8", source)
}
func imagePart(id, media string, data []byte) string {
	return mimePart("Content-Type: "+media+"\r\nContent-ID: <"+id+">\r\nContent-Transfer-Encoding: base64", base64.StdEncoding.EncodeToString(data))
}
func raster(t *testing.T, format string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&buf, img)
	case "jpeg":
		err = jpeg.Encode(&buf, img, nil)
	case "gif":
		err = gif.Encode(&buf, img, nil)
	case "webp":
		// A synthetic 2×2 red image encoded as lossless WebP.
		data, err := base64.StdEncoding.DecodeString("UklGRhwAAABXRUJQVlA4TA8AAAAvAUAAAAcQ/Y/+ByKi/wEA")
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestReadBodyMIMEPresentations(t *testing.T) {
	plain := mimePart("Content-Type: text/plain", "Plain fallback")
	rich := htmlMessage(`<table><tr><td style="color:red">Rich body</td></tr></table>`)
	for _, tc := range []struct{ name, raw, text, html string }{
		{"plain", plain, "Plain fallback", ""},
		{"HTML", rich, "Rich body", "<table>"},
		{"alternative", multipart("alternative", "a", plain, rich), "Plain fallback", "<table>"},
		{"last supported alternative", multipart("alternative", "a", plain, rich, htmlMessage("<p>Latest</p>")), "Plain fallback", "Latest"},
		{"mixed attachment", multipart("mixed", "m", multipart("alternative", "a", plain, rich), mimePart("Content-Type: text/html\r\nContent-Disposition: attachment; filename=a.html", "secret")), "Plain fallback", "Rich body"},
		{"named attachment", multipart("mixed", "m", mimePart("Content-Type: text/html; name=a.html", "secret"), rich), "Rich body", "<table>"},
		{"forwarded message", multipart("mixed", "m", plain, mimePart("Content-Type: message/rfc822", rich)), "Plain fallback", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := readBody(t, tc.raw)
			if body.Text != tc.text || (tc.html == "" && body.HTML != "") || (tc.html != "" && !strings.Contains(body.HTML, tc.html)) || strings.Contains(body.HTML, "secret") || body.HasRemoteImages {
				t.Fatalf("unexpected body: %+v", body)
			}
			encoded, _ := json.Marshal(body)
			var fields map[string]any
			json.Unmarshal(encoded, &fields)
			for _, field := range []string{"text", "truncated", "has_remote_images"} {
				if _, ok := fields[field]; !ok {
					t.Fatalf("missing %s", field)
				}
			}
		})
	}
}

func TestReadBodyDecodesCharacterAndTransferEncodings(t *testing.T) {
	for _, charset := range []string{"utf-8", "GBK"} {
		for _, encoding := range []string{"base64", "quoted-printable", "8bit"} {
			t.Run(charset+"/"+encoding, func(t *testing.T) {
				value := "<p>正文与排版</p>"
				if charset == "GBK" {
					var err error
					value, err = simplifiedchinese.GBK.NewEncoder().String(value)
					if err != nil {
						t.Fatal(err)
					}
				}
				switch encoding {
				case "base64":
					value = base64.StdEncoding.EncodeToString([]byte(value))
				case "quoted-printable":
					var b bytes.Buffer
					w := quotedprintable.NewWriter(&b)
					w.Write([]byte(value))
					w.Close()
					value = b.String()
				}
				body := readBody(t, mimePart("Content-Type: text/html; charset="+charset+"\r\nContent-Transfer-Encoding: "+encoding, value))
				if body.Text != "正文与排版" || !strings.Contains(body.HTML, "<p>正文与排版</p>") {
					t.Fatalf("%+v", body)
				}
			})
		}
	}
	for _, raw := range []string{
		mimePart("Content-Type: text/html; charset=unknown-charset", "bad"),
		mimePart("Content-Type: text/html\r\nContent-Transfer-Encoding: base64", "%%%"),
		mimePart("Content-Type: multipart/alternative; boundary=missing", "missing"),
		mimePart("Content-Type: text/html", string([]byte{255})),
	} {
		if _, err := ReadBody(t.Context(), strings.NewReader(raw)); err == nil {
			t.Fatal("invalid MIME accepted")
		}
	}
}

func TestReadBodySanitizesActiveContentAndKeepsLayout(t *testing.T) {
	body := readBody(t, htmlMessage(`<html><head><base href="https://bad.test"><meta http-equiv="refresh" content="0;url=https://bad.test"><link rel="stylesheet" href="https://bad.test"><link rel="preload" href="https://bad.test"><style>@import url(https://bad.test);</style></head><body onload="alert(1)"><script>alert(1)</script><iframe srcdoc="bad"></iframe><form action="https://bad.test"><input><button>submit</button></form><svg><image href="https://bad.test"/></svg><math>bad</math><object data="https://bad.test"></object><video poster="https://bad.test"></video><table bgcolor="#fff"><tr><td style="padding: 10px; color: rgb(1,2,3); font-family: Arial; position:fixed;behavior:url(https://bad.test);width:expression(alert(1));background-image:image-set('https://bad.test' 1x);--image:url(https://bad.test)"><b>Safe</b><a href="https://example.test/path" ping="https://bad.test" onclick="bad()">link</a><img alt="description" src="javascript:bad()" onerror="bad()"></td></tr></table></body></html>`))
	for _, bad := range []string{"bad.test", "<script", "iframe", "form", "<svg", "<math", "object", "video", "onload", "onclick", "onerror", "ping=", "position", "expression", "behavior", "image-set", "--image"} {
		if strings.Contains(body.HTML, bad) {
			t.Errorf("retained %s in %s", bad, body.HTML)
		}
	}
	for _, want := range []string{"<table", "<td", "padding:10px", "color:rgb(1,2,3)", "<b>Safe</b>", `href="https://example.test/path"`, `alt="description"`} {
		if !strings.Contains(body.HTML, want) {
			t.Errorf("missing %s: %s", want, body.HTML)
		}
	}
	if body.HasRemoteImages {
		t.Fatal("links or removed content marked as images")
	}
}

func TestReadBodyImageReferencesAndFlags(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
		remote             bool
	}{
		{"img", `<img src="https://img.test/a.png">`, `src="https://img.test/a.png"`, true},
		{"HTTP img", `<img src="http://img.test/a.png">`, `src="http://img.test/a.png"`, true},
		{"srcset", `<img srcset="https://img.test/a.png 1x, https://img.test/b.png 2x">`, `srcset=`, true},
		{"srcset commas", `<img srcset="https://img.test/a,b.png 100w, https://img.test/c.png 200w">`, `a,b.png`, true},
		{"picture", `<picture><source srcset="https://img.test/a.png 1x"><img alt="fallback"></picture>`, `<source srcset=`, true},
		{"background", `<div style="background-image:url('https://img.test/bg.png');color:red">hello</div>`, `background-image:url`, true},
		{"shorthand", `<div style="background: #fff url(https://img.test/bg.png) no-repeat">hello</div>`, `background:`, true},
		{"multiple backgrounds", `<div style="background:url(https://img.test/a),url(https://img.test/b)"></div>`, `background:url`, true},
		{"discarded CSS declaration", `<div style="background:url(https://img.test/a) expression(bad)">ok</div>`, `ok`, false},
		{"escaped CSS function", `<div style="background:u\72l(https://img.test/a)">ok</div>`, `ok`, false},
		{"only link", `<a href="https://img.test/a">link</a>`, `href=`, false},
		{"removed source", `<video><source srcset="https://img.test/a 1x"></video>`, "", false},
		{"orphan source", `<source srcset="https://img.test/a 1x">`, "", false},
		{"credentials", `<img src="https://user:pass@img.test/a">`, `<img`, false},
		{"relative", `<img src="//img.test/a">`, `<img`, false},
		{"invalid srcset", `<img srcset="https://img.test/a nope, javascript:x 2x">`, `<img`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := readBody(t, htmlMessage(tc.source))
			if body.HasRemoteImages != tc.remote || !strings.Contains(body.HTML, tc.want) {
				t.Fatalf("%+v", body)
			}
		})
	}
	for _, target := range []string{"javascript:alert(1)", "data:image/svg+xml,bad", "file:///etc/passwd", "blob:https://img.test/a", "https://user:pass@img.test/a", "https:///a", "//img.test/a", "https://img.test\\@bad.test/a"} {
		t.Run(target, func(t *testing.T) {
			body := readBody(t, htmlMessage(`<img src="`+target+`"><a href="`+target+`">ok</a>`))
			if strings.Contains(body.HTML, "src=") || strings.Contains(body.HTML, "href=") || body.HasRemoteImages {
				t.Fatalf("unsafe URL: %+v", body)
			}
		})
	}
}

func TestReadBodyCIDImagesStayInsideRelatedPresentation(t *testing.T) {
	for _, format := range []string{"png", "jpeg", "gif", "webp"} {
		t.Run(format, func(t *testing.T) {
			body := readBody(t, multipart("related", "r", multipart("alternative", "a", mimePart("Content-Type: text/plain", "fallback"), htmlMessage(`<img src="cid:logo" alt="logo"><div style="background-image:url(cid:logo)"></div><img srcset="cid:logo 1x">`)), imagePart("logo", "image/"+format, raster(t, format))))
			if body.Text != "fallback" || body.HasRemoteImages || strings.Count(body.HTML, "data:image/"+format+";base64,") != 3 {
				t.Fatalf("%+v", body)
			}
		})
	}
	img := imagePart("logo", "image/png", raster(t, "png"))
	for _, tc := range []struct {
		name, raw string
		valid     bool
	}{
		{"HTML before image", multipart("related", "r", htmlMessage(`<img src="cid:logo">`), img), true},
		{"percent CID", multipart("related", "r", htmlMessage(`<img src="cid:%6cogo">`), img), true},
		{"related inside alternative", multipart("alternative", "a", mimePart("Content-Type: text/plain", "fallback"), multipart("related", "r", htmlMessage(`<img src="cid:logo">`), img)), true},
		{"start parameter", strings.Replace(multipart("related", "r", img, mimePart("Content-Type: text/html\r\nContent-ID: <root>", `<img src="cid:logo">`)), "boundary=r", "boundary=r; start=\"<root>\"", 1), true},
		{"missing", multipart("related", "r", htmlMessage(`<img src="cid:missing" alt="kept">`), img), false},
		{"outside related", multipart("mixed", "m", htmlMessage(`<img src="cid:logo">`), img), false},
		{"sibling related", multipart("mixed", "m", multipart("related", "r", htmlMessage(`<img src="cid:logo">`)), multipart("related", "s", htmlMessage("other"), img)), false},
		{"duplicate CID", multipart("related", "r", htmlMessage(`<img src="cid:logo">`), img, img), false},
		{"SVG", multipart("related", "r", htmlMessage(`<img src="cid:logo">`), imagePart("logo", "image/svg+xml", []byte(`<svg onload="bad()"/>`))), false},
		{"MIME mismatch", multipart("related", "r", htmlMessage(`<img src="cid:logo">`), imagePart("logo", "image/jpeg", raster(t, "png"))), false},
		{"corrupt image", multipart("related", "r", htmlMessage(`<img src="cid:logo">`), imagePart("logo", "image/png", raster(t, "png")[:30])), false},
		{"untrusted data URL", htmlMessage(`<img src="data:image/png;base64,` + base64.StdEncoding.EncodeToString(raster(t, "png")) + `">`), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := readBody(t, tc.raw)
			if strings.Contains(body.HTML, "data:image/png") != tc.valid || body.HasRemoteImages {
				t.Fatalf("%+v", body)
			}
		})
	}
	body := readBody(t, htmlMessage(`<img src="cid:logo" alt="kept">`))
	if strings.Contains(body.HTML, "src=") || !strings.Contains(body.HTML, "kept") {
		t.Fatalf("%+v", body)
	}
}

func TestReadBodyLimitsAndJSONExpansion(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"text", mimePart("Content-Type: text/plain; charset=utf-8", strings.Repeat("中文", 20000))},
		{"HTML", htmlMessage("<p>" + strings.Repeat("x", MaxHTMLBytes) + "</p>")},
		{"JSON escapes", multipart("alternative", "a", mimePart("Content-Type: text/plain", strings.Repeat("<", MaxTextBytes)), htmlMessage("<p>"+strings.Repeat("&lt;", 40000)+"</p>"))},
		{"CID repetition", multipart("related", "r", htmlMessage(strings.Repeat(`<img src="cid:logo">`, 3000)), imagePart("logo", "image/png", raster(t, "png")))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := readBody(t, tc.raw)
			encoded, _ := json.Marshal(body)
			if !body.Truncated || len(body.Text) > MaxTextBytes || !utf8.ValidString(body.Text) || len(encoded) > MaxResponseBytes || body.HTML != "" || body.HasRemoteImages {
				t.Fatalf("limits: text=%d html=%d json=%d truncated=%t", len(body.Text), len(body.HTML), len(encoded), body.Truncated)
			}
		})
	}
	body := readBody(t, htmlMessage("<p>"+strings.Repeat("x", MaxHTMLBytes-7)+"</p>"))
	if len(body.HTML) != MaxHTMLBytes {
		t.Fatalf("HTML at limit: %d", len(body.HTML))
	}
	if _, err := ReadBody(t.Context(), strings.NewReader(strings.Repeat("x", MaxMessageBytes+1))); err == nil {
		t.Fatal("oversize MIME accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ReadBody(ctx, strings.NewReader(htmlMessage("hello"))); err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestReadBodyLimitsRepeatedCIDStylesAndSourceSets(t *testing.T) {
	image := imagePart("logo", "image/png", raster(t, "png"))
	for _, source := range []string{
		`<img srcset="` + strings.Repeat("cid:logo 1x, ", 3000) + `">`,
		`<div style="background-image:` + strings.TrimSuffix(strings.Repeat("url(cid:logo),", 3000), ",") + `"></div>`,
		strings.Repeat(`<div style="background-image:url(cid:logo)"></div>`, 3000),
	} {
		body := readBody(t, multipart("related", "r", htmlMessage("<p>fallback</p>"+source), image))
		if body.HTML != "" || !body.Truncated || body.Text != "fallback" || body.HasRemoteImages {
			t.Fatal("expanded CID content did not fall back")
		}
	}
}

func TestReadBodyRemovesMalformedActiveMarkup(t *testing.T) {
	for _, source := range []string{
		`<noscript><img src="https://bad.test"></noscript><p>safe</p>`,
		`<svg><foreignObject><p><img src="https://bad.test"></p></foreignObject></svg><p>safe</p>`,
		`<math><mtext><table><mglyph><style><!--</style><img title="--><img src='https://bad.test' onerror='alert(1)'>">`,
		`<div style="@import 'https://bad.test'; color:red; font-family:url(https://bad.test);background-image:var(--bad)">safe</div>`,
		`<img src="javascript&colon;alert(1)" onerror="alert(1)"><a href="java&#x09;script:alert(1)">safe</a>`,
	} {
		body := readBody(t, htmlMessage(source))
		for _, bad := range []string{"<script", "<style", "<svg", "<math", "onerror=", "javascript:", "src=\"https://bad.test"} {
			if strings.Contains(body.HTML, bad) {
				t.Fatalf("active markup retained: %s", body.HTML)
			}
		}
	}
}

func FuzzReadBody(f *testing.F) {
	for _, raw := range []string{htmlMessage(`<img src="https://images.test/a"><p>Hello</p>`), htmlMessage(`<div style="background:url('https://images.test/b')">fallback</div>`), multipart("alternative", "a", mimePart("Content-Type: text/plain", "text"), htmlMessage("<table><tr><td>html</td></tr></table>")), "Content-Type: text/plain\r\n\r\nhello"} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > MaxMessageBytes {
			return
		}
		body, err := ReadBody(t.Context(), strings.NewReader(raw))
		if err != nil {
			return
		}
		encoded, err := json.Marshal(body)
		if err != nil || len(encoded) > MaxResponseBytes || len(body.HTML) > MaxHTMLBytes || len(body.Text) > MaxTextBytes || !utf8.ValidString(body.HTML) || !utf8.ValidString(body.Text) || (body.HTML == "" && body.HasRemoteImages) {
			t.Fatal("invalid bounded response")
		}
	})
}

func TestReadBodyRejectsCIDDimensionsAndOversizeUnsupportedResources(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 4097, 1))); err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		imagePart("logo", "image/png", encoded.Bytes()),
		imagePart("logo", "image/svg+xml", []byte("<svg>"+strings.Repeat(" ", 200000)+"</svg>")),
	} {
		body := readBody(t, multipart("related", "r", htmlMessage(`<p>fallback</p><img src="cid:logo" alt="kept">`), part))
		if body.HTML == "" || strings.Contains(body.HTML, "src=") || !strings.Contains(body.HTML, `alt="kept"`) || body.Truncated {
			t.Fatalf("invalid resource should be removed: %+v", body)
		}
	}
}

func TestReadBodyKeepsTextWhenCIDTransferEncodingIsInvalid(t *testing.T) {
	for _, part := range []string{
		mimePart("Content-Type: image/png\r\nContent-ID: <logo>\r\nContent-Transfer-Encoding: base64", "%%%"),
		mimePart("Content-Type: image/png\r\nContent-ID: <logo>\r\nContent-Transfer-Encoding: unknown-encoding", "unreadable"),
	} {
		body := readBody(t, multipart("related", "r", htmlMessage(`<p>readable</p><img src="cid:logo" alt="logo">`), part))
		if body.Text != "readable" || body.HTML == "" || strings.Contains(body.HTML, "src=") || !strings.Contains(body.HTML, `alt="logo"`) {
			t.Fatalf("invalid image affected body: %+v", body)
		}
	}
}

package content

import (
	"encoding/base64"
	"mime/quotedprintable"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func message(headers, body string) string {
	return "From: sender@example.org\r\nTo: me@example.org\r\n" + headers + "\r\n\r\n" + body
}

func TestDetectCode(t *testing.T) {
	for _, tc := range []struct {
		name, subject, text, want string
	}{
		{"github", "[GitHub] Please verify your device", "Verification code: 482913\nIf you did not attempt to sign in, change your password.", "482913"},
		{"apple", "Your Apple Account code", "Your Apple Account verification code is 730215. Do not share this code.", "730215"},
		{"spaced", "Your login code", "Use 915 406 to finish signing in.", "915406"},
		{"code in subject", "123456 is your Slack confirmation code", "Welcome back.", "123456"},
		{"chinese", "登录提醒", "您的验证码为：482913，5 分钟内有效，请勿泄露。", "482913"},
		{"chinese no separator", "", "【招商银行】验证码482913，用于登录", "482913"},
		{"japanese", "", "認証コード: 402913", "402913"},
		{"letter digit", "", "Your one-time code is G7K2QX.", "G7K2QX"},
		{"otp", "", "OTP 5521 expires in 10 minutes", "5521"},
		{"amount", "您的 9 月账单已出", "本期应还金额 ¥3,284.50，到期还款日 2026.10.15。", ""},
		{"order number", "Your package has shipped", "Order #12345678 shipped. Tracking 948271", ""},
		{"shipping is not pin", "Shipping update", "Shipping 948271 departed", ""},
		{"promo code", "Sale", "Use promo code SAVE20 for 20% off", ""},
		{"year near code", "", "Your verification code expires in 2026", ""},
		{"time", "", "Security code sent at 10:32", ""},
		{"no keyword", "Meeting", "Room 402913 on floor 3", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectCode(tc.subject, tc.text); got != tc.want {
				t.Fatalf("DetectCode(%q, %q) = %q, want %q", tc.subject, tc.text, got, tc.want)
			}
		})
	}
}

func TestExtractPrefersPlainText(t *testing.T) {
	raw := message("Subject: Code\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=b",
		"--b\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nYour verification code is 482913.\r\n--b\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>HTML version 999999</p>\r\n--b--\r\n")
	r, err := Extract(strings.NewReader(raw), "Code")
	if err != nil || r.Text != "Your verification code is 482913." || r.Code != "482913" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestExtractConvertsHTMLAndLegacyCharset(t *testing.T) {
	html := `<html><head><style>p{color:red}</style><title>t</title></head><body>
<h1>招商银行</h1><p>您的验证码为：<b>482913</b></p>
<ul><li>勿泄露</li><li>5 分钟有效</li></ul>
<p><a href="https://bank.example.org/help">帮助中心</a> <a href="javascript:alert(1)">x</a></p>
<script>var code = 111111</script><img src="https://tracker.example.org/p.gif"></body></html>`
	encoded, err := simplifiedchinese.GBK.NewEncoder().String(html)
	if err != nil {
		t.Fatal(err)
	}
	raw := message("Subject: =?GBK?B?"+base64.StdEncoding.EncodeToString([]byte(mustGBK(t, "登录验证")))+"?=\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=GBK\r\nContent-Transfer-Encoding: base64",
		base64.StdEncoding.EncodeToString([]byte(encoded)))
	r, err := Extract(strings.NewReader(raw), "登录验证")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"招商银行", "您的验证码为： 482913", "• 勿泄露", "• 5 分钟有效", "帮助中心 (https://bank.example.org/help)"} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("text lacks %q:\n%s", want, r.Text)
		}
	}
	for _, unwanted := range []string{"color:red", "111111", "javascript", "tracker"} {
		if strings.Contains(r.Text, unwanted) {
			t.Errorf("text contains %q:\n%s", unwanted, r.Text)
		}
	}
	if r.Code != "482913" {
		t.Fatalf("code %q", r.Code)
	}
}

func mustGBK(t *testing.T, s string) string {
	t.Helper()
	encoded, err := simplifiedchinese.GBK.NewEncoder().String(s)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestExtractQuotedPrintableAndLimits(t *testing.T) {
	long := strings.Repeat("长文本 ", 20000)
	var encoded strings.Builder
	w := quotedprintable.NewWriter(&encoded)
	if _, err := w.Write([]byte(long)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	raw := message("Subject: Info\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable",
		"Your code is =\r\n771204.\r\n\r\n\r\n\r\nNext paragraph.\r\n"+encoded.String())
	r, err := Extract(strings.NewReader(raw), "Info")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.Text, "Your code is 771204.\n\nNext paragraph.") || r.Code != "771204" {
		t.Fatalf("soft line break or paragraphs: %q %q", clip(r.Text, 60), r.Code)
	}
	if len(r.Text) > MaxTextBytes || len(r.Excerpt) > ExcerptBytes || strings.Contains(r.Excerpt, "\n") {
		t.Fatalf("limits: text=%d excerpt=%d", len(r.Text), len(r.Excerpt))
	}
}

func TestExtractSkipsAttachments(t *testing.T) {
	raw := message("Subject: Invoice\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=m",
		"--m\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nInvoice attached.\r\n--m\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=secret.txt\r\n\r\nverification code 999999\r\n--m--\r\n")
	r, err := Extract(strings.NewReader(raw), "Invoice")
	if err != nil || r.Text != "Invoice attached." || r.Code != "" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestExtractRejectsDecodingFailures(t *testing.T) {
	partialCode := base64.StdEncoding.EncodeToString([]byte("Your verification code is 4829")) + "!"
	for _, tc := range []struct{ name, headers, body string }{
		{"broken base64", "Content-Type: text/plain\r\nContent-Transfer-Encoding: base64", partialCode},
		{"unknown charset", "Content-Type: text/plain; charset=unknown-charset", "Your verification code is 482913"},
		{"broken multipart encoding", "Content-Type: multipart/alternative; boundary=b", "--b\r\nContent-Type: text/plain\r\n\r\nYour verification code is 482913\r\n--b\r\nContent-Type: text/html\r\nContent-Transfer-Encoding: base64\r\n\r\n" + partialCode + "\r\n--b--\r\n"},
		{"unknown part charset", "Content-Type: multipart/alternative; boundary=b", "--b\r\nContent-Type: text/plain; charset=unknown-charset\r\n\r\nYour verification code is 482913\r\n--b--\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Extract(strings.NewReader(message(tc.headers, tc.body)), "Sign in")
			if err == nil || result != (Result{}) {
				t.Fatalf("expected decoding failure with empty result, got %+v, %v", result, err)
			}
		})
	}
}

func TestInlineImageKeepsReadableTextAndCode(t *testing.T) {
	raw := "Content-Type: multipart/related; boundary=x\r\n\r\n--x\r\nContent-Type: text/plain\r\n\r\nYour verification code is 482913.\r\n--x\r\nContent-Type: image/png\r\nContent-Disposition: inline\r\nContent-Transfer-Encoding: base64\r\n\r\niVBORw==\r\n--x--"
	result, err := Extract(strings.NewReader(raw), "")
	if err != nil || result.Code != "482913" {
		t.Fatalf("inline image changed text parsing: %v", err)
	}
}

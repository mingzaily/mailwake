package content

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCodeValidityMIMERepresentations(t *testing.T) {
	english := "Your verification code is 482913. This code expires in 10 min."
	for _, tc := range []struct{ name, headers, body string }{
		{"plain English", "Content-Type: text/plain; charset=utf-8", english},
		{"base64 English", "Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64", base64.StdEncoding.EncodeToString([]byte(english))},
		{"quoted-printable English", "Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable", "Your verification code is 482913. This code expires in 10=\r\n min."},
		{"HTML base64 English", "Content-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: base64", base64.StdEncoding.EncodeToString([]byte("<p>" + english + "</p>"))},
		{"GBK Chinese", "Content-Type: text/plain; charset=GBK\r\nContent-Transfer-Encoding: base64", base64.StdEncoding.EncodeToString([]byte(mustGBK(t, "验证码482913，10分钟内有效。")))},
		{"preferred plain English", "Content-Type: multipart/alternative; boundary=validity", "--validity\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>OTP 482913 expires in 5 minutes.</p>\r\n--validity\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + english + "\r\n--validity--\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Extract(strings.NewReader(message(tc.headers, tc.body)), "Your login code")
			if err != nil || result.Code != "482913" || result.CodeLifetime != 10*time.Minute {
				t.Fatalf("code = %q, lifetime = %v; error %v", result.Code, result.CodeLifetime, err)
			}
		})
	}
}

func TestExtractCodeValidity(t *testing.T) {
	// Synthetic messages cover published template phrases and ambiguous durations.
	// Phrase sources: Duende SMTP OTP dispatcher and Logto email template references.
	for _, tc := range []struct {
		name, subject, text string
		want                time.Duration
	}{
		{"Cindy Chinese wording", "登录验证码", "请在 10 分钟内输入以下验证码完成登录。验证码：482913", 10 * time.Minute},
		{"Duende expiry wording", "Your login code", "Your code is 482913. This code will expire in 5 minutes.", 5 * time.Minute},
		{"Logto active wording", "Your login code", "Your code is 482913. The code will remain active for 10 minutes.", 10 * time.Minute},
		{"Chinese template suffix", "邮箱验证", "您的验证码为482913。验证码 5 分钟内有效。", 5 * time.Minute},
		{"minutes abbreviation", "Your login code", "Your verification code is 482913. It expires in 10 min.", 10 * time.Minute},
		{"seconds", "OTP", "OTP 482913 is valid for 90 seconds.", 90 * time.Second},
		{"hours", "OTP", "OTP 482913 is valid for 2 hours.", 2 * time.Hour},
		{"Chinese prefix", "验证码", "验证码482913，有效期为10分钟。", 10 * time.Minute},
		{"traditional prefix", "驗證碼", "驗證碼482913，有效期為5分鐘", 5 * time.Minute},
		{"traditional suffix", "驗證碼", "驗證碼482913，5分鐘內有效", 5 * time.Minute},
		{"Chinese seconds", "验证码", "验证码482913，90秒后失效", 90 * time.Second},
		{"Chinese hours", "验证码", "验证码482913，有效期：2小时", 2 * time.Hour},
		{"traditional hours", "驗證碼", "驗證碼482913，有效期為2小時", 2 * time.Hour},
		{"Chinese future expiry", "验证码", "验证码482913，将在10分钟后过期。", 10 * time.Minute},
		{"Chinese code expiry", "验证码", "此验证码将在10分钟后失效。验证码：482913", 10 * time.Minute},
		{"Chinese request", "验证码", "验证码482913。请于5分钟内使用此验证码。", 5 * time.Minute},
		{"traditional request", "驗證碼", "驗證碼482913。請於5分鐘內輸入此驗證碼。", 5 * time.Minute},
		{"only valid", "OTP", "OTP 482913. This code is only valid for 10 minutes.", 10 * time.Minute},
		{"remains valid", "OTP", "OTP 482913. This code remains valid for 10 minutes.", 10 * time.Minute},
		{"will be valid", "OTP", "OTP 482913. This code will be valid for 10 minutes.", 10 * time.Minute},
		{"expires after", "OTP", "OTP 482913 expires after 10 minutes.", 10 * time.Minute},
		{"use within", "OTP", "OTP 482913. Use this verification code within 10 minutes.", 10 * time.Minute},
		{"enter within", "OTP", "OTP 482913. Enter your security code within 30 seconds.", 30 * time.Second},
		{"upper case", "OTP", "OTP 482913. THIS CODE EXPIRES IN 10 MIN.", 10 * time.Minute},
		{"no unit space", "OTP", "OTP 482913 expires in 10min.", 10 * time.Minute},
		{"line breaks", "OTP", "OTP 482913. This code will\nexpire in\n10\nminutes.", 10 * time.Minute},
		{"HTML inline tags", "OTP", "OTP <b>482913</b>. This code expires in <strong>10</strong> minutes.", 10 * time.Minute},
		{"HTML breaks", "OTP", "OTP 482913. This code will<br>expire in 10<br>minutes.", 10 * time.Minute},
		{"HTML nonbreaking spaces", "OTP", "OTP 482913. This code expires in 10&nbsp;minutes.", 10 * time.Minute},
		{"subject validity", "OTP 482913 expires in 10 minutes", "Sign in using the code in the subject.", 10 * time.Minute},
		{"same value repeated", "OTP", "OTP 482913 expires in 5 min. This code is valid for 5 minutes.", 5 * time.Minute},
		{"equivalent units", "OTP", "OTP 482913 expires in 5 min. 验证码有效期为300秒。", 5 * time.Minute},
		{"unrelated resend interval", "OTP", "OTP 482913 expires in 10 minutes. You can request another code in 60 seconds.", 10 * time.Minute},
		{"code and independent link", "OTP", "OTP 482913 expires in 10 minutes. The help link expires in 24 hours.", 10 * time.Minute},
		{"Chinese code and independent link", "验证码", "验证码482913，10分钟内有效。重置链接有效期为24小时。", 10 * time.Minute},

		{"English singular minute", "Your login code", "Your code 482913 expires in 1 minute.", time.Minute},
		{"English security code", "Your security code", "Your security code 482913 will expire after 15 minutes.", 15 * time.Minute},
		{"English abbreviated seconds", "OTP", "OTP 482913. This passcode is valid for 30 secs.", 30 * time.Second},
		{"English abbreviated hours", "OTP", "OTP 482913. This PIN expires in 2 hrs.", 2 * time.Hour},
		{"English OTP subject", "OTP", "OTP 482913. This OTP is valid for 5 mins.", 5 * time.Minute},
		{"English only after for", "OTP", "OTP 482913. This code is valid for only 10 minutes.", 10 * time.Minute},
		{"English next minutes", "OTP", "OTP 482913. This code is valid for the next 10 minutes.", 10 * time.Minute},
		{"English active for", "OTP", "OTP 482913. This code will be active for 10 minutes.", 10 * time.Minute},
		{"English good for", "OTP", "OTP 482913. This code is good for 10 minutes.", 10 * time.Minute},
		{"English validity label", "OTP", "OTP 482913. Code validity: 10 minutes.", 10 * time.Minute},
		{"English expiration period", "OTP", "OTP 482913. Code expiration period is 10 minutes.", 10 * time.Minute},
		{"English standalone label", "OTP", "OTP 482913. Valid for 10 minutes.", 10 * time.Minute},
		{"English code within", "OTP", "OTP 482913. Use code 482913 within 10 minutes.", 10 * time.Minute},
		{"English one-time code", "OTP", "OTP 482913. Enter your one-time code within the next 10 minutes.", 10 * time.Minute},
		{"English mixed case", "OTP", "OTP 482913. Your CoDe ExPiReS iN 10 MiNuTeS.", 10 * time.Minute},
		{"English unicode spacing", "OTP", "OTP 482913. This code expires in 10\u202fminutes.", 10 * time.Minute},
		{"English grouped code", "Your login code", "Your code 482 913 expires in 10 minutes.", 10 * time.Minute},
		{"English alphanumeric code", "Your login code", "Your code G7K2QX expires in 10 minutes.", 10 * time.Minute},
		{"English implicit conjunction", "OTP", "OTP 482913 is valid for 5 minutes but expires after 10 minutes.", 0},
		{"English compound with and", "OTP", "OTP 482913 expires in 1 hour and 30 minutes.", 0},
		{"English alternative durations", "OTP", "OTP 482913 expires in 5 minutes or 10 minutes.", 0},
		{"English range with repeated units", "OTP", "OTP 482913 is valid for 5 minutes to 10 minutes.", 0},
		{"English dash with repeated units", "OTP", "OTP 482913 is valid for 5 min – 10 min.", 0},
		{"Chinese alternatives", "验证码", "验证码482913，有效期为1小时或30分钟。", 0},
		{"English numeric overflow", "OTP", "OTP 482913 expires in 9223372036854775807 hours.", 0},
		{"English approximate duration", "OTP", "OTP 482913 expires in approximately 10 minutes.", 0},
		{"English deadline only", "OTP", "OTP 482913 is valid until 12:30 UTC.", 0},
		{"English negative after for", "OTP", "OTP 482913 is not valid for 10 minutes.", 0},
		{"English link pronoun", "OTP", "OTP 482913. Reset your password using this link. It expires in 24 hours.", 0},
		{"English session pronoun", "OTP", "OTP 482913. Your session is ready. It expires in 2 hours.", 0},
		{"English link active", "OTP", "OTP 482913. This link will remain active for 10 minutes.", 0},
		{"English resend restriction", "OTP", "OTP 482913. Request another code after 30 seconds.", 0},
		{"English two different subjects", "OTP", "OTP 482913 is valid for 10 minutes, and your session expires in 2 hours.", 10 * time.Minute},
		{"unknown", "验证码", "验证码482913", 0},
		{"no code", "Meeting", "The meeting expires in 10 minutes.", 0},
		{"support response", "验证码", "验证码482913，客服会在10分钟内联系您", 0},
		{"link only", "OTP", "OTP 482913. The reset link expires in 24 hours.", 0},
		{"Chinese link only", "验证码", "验证码482913。此链接有效期为24小时。", 0},
		{"session only", "OTP", "OTP 482913. Your session is valid for 2 hours.", 0},
		{"invalid phrase", "OTP", "OTP 482913. This code is invalid for 10 minutes.", 0},
		{"negative phrase", "OTP", "OTP 482913. This code is not valid for 10 minutes.", 0},
		{"survey deadline", "验证码", "验证码482913。请在10分钟内完成问卷。", 0},
		{"conflicting", "OTP", "OTP 482913 expires in 5 minutes. Code valid for 10 min.", 0},
		{"subject body conflict", "OTP 482913 expires in 5 minutes", "This code expires in 10 minutes.", 0},
		{"bilingual conflict", "OTP", "OTP 482913 expires in 10 minutes. 验证码有效期为5分钟。", 0},
		{"zero", "OTP", "OTP 482913 expires in 0 minutes.", 0},
		{"zero and positive", "OTP", "OTP 482913 expires in 0 minutes. This code expires in 5 minutes.", 0},
		{"negative duration", "OTP", "OTP 482913 expires in -5 minutes.", 0},
		{"Chinese negative duration", "验证码", "验证码482913，-5分钟内有效", 0},
		{"English decimal", "OTP", "OTP 482913 expires in 1.5 minutes.", 0},
		{"Chinese decimal", "验证码", "验证码482913，1.5分钟内有效", 0},
		{"English range", "OTP", "OTP 482913 expires in 5-10 minutes.", 0},
		{"Chinese range", "验证码", "验证码482913，5至10分钟内有效", 0},
		{"Chinese dash range", "验证码", "验证码482913，5–10分钟内有效", 0},
		{"compound duration", "OTP", "OTP 482913 expires in 1 hour 30 minutes.", 0},
		{"Chinese compound duration", "验证码", "验证码482913，有效期为1小时30分钟。", 0},
		{"Chinese compound suffix", "验证码", "验证码482913，1小时30分钟内有效。", 0},
		{"English number words", "OTP", "OTP 482913 expires in ten minutes.", 0},
		{"Chinese number words", "验证码", "验证码482913，十分钟内有效", 0},
		{"full width digits", "验证码", "验证码482913，１０分钟内有效", 0},
		{"unsupported days", "OTP", "OTP 482913 expires in 2 days.", 0},
		{"unit prefix", "OTP", "OTP 482913 expires in 10 milliseconds.", 0},
		{"absolute expiry", "OTP", "OTP 482913 expires at 12:30 UTC.", 0},
		{"duration overflow", "OTP", "OTP 482913 expires in 999999999999999999999 hours.", 0},
		{"Chinese duration overflow", "验证码", "验证码482913，999999999999999999999分钟内有效", 0},
		{"mixed precision conflict", "OTP", "OTP 482913 expires in 5 minutes. This code expires in 1.5 minutes.", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := message("Content-Type: text/html; charset=utf-8", "<p>"+tc.text+"</p>")
			result, err := Extract(strings.NewReader(raw), tc.subject)
			if err != nil || result.CodeLifetime != tc.want {
				t.Fatalf("code = %q, lifetime = %v, want %v; error %v", result.Code, result.CodeLifetime, tc.want, err)
			}
		})
	}
}

func TestCodeValidityUnitSpellings(t *testing.T) {
	for unit, duration := range map[string]time.Duration{
		"s": time.Second, "sec": time.Second, "secs": time.Second, "second": time.Second, "seconds": time.Second,
		"m": time.Minute, "min": time.Minute, "mins": time.Minute, "minute": time.Minute, "minutes": time.Minute,
		"h": time.Hour, "hr": time.Hour, "hrs": time.Hour, "hour": time.Hour, "hours": time.Hour,
	} {
		for _, amount := range []int{1, 5, 90} {
			t.Run(fmt.Sprintf("%d_%s", amount, unit), func(t *testing.T) {
				raw := message("Content-Type: text/plain; charset=utf-8", fmt.Sprintf("OTP 482913. This code expires in %d %s.", amount, unit))
				got, err := Extract(strings.NewReader(raw), "Your login code")
				if err != nil || got.CodeLifetime != time.Duration(amount)*duration {
					t.Fatalf("lifetime = %v; error %v", got.CodeLifetime, err)
				}
			})
		}
	}
}

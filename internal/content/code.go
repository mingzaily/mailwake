package content

import (
	"regexp"
	"strings"
	"unicode"
)

// Keywords that introduce a verification code. Detection is deliberately conservative:
// a number counts only when one of these appears in the subject or shortly before it.
// English phrases match whole words, so "pin" never matches "shipping"; a bare "code"
// is excluded because promo and postal codes are common.
var englishKeyword = regexp.MustCompile(`(?i)\b(?:verification|security|login|sign[- ]?in|confirmation|access|authentication|auth|one[- ]time|your) (?:code|pin|passcode)\b|\bcode (?:is|:)|\bcode:|\b(?:passcode|otp|2fa|pin)\b`)

var otherKeywords = []string{
	"验证码", "校验码", "动态码", "动态密码", "确认码", "安全码", "登录码", "驗證碼", "認證碼",
	"認証コード", "確認コード", "ワンタイム", "인증번호", "인증 코드",
}

// candidate matches 4–8 digits, optionally split once by a space or hyphen ("915 406"),
// or a 6–8 character uppercase letter-digit code ("G7K2QX").
var candidate = regexp.MustCompile(`\b(\d{3,4}[ -]\d{3,4}|\d{4,8}|[A-Z0-9]{6,8})\b`)

// keywordWindow is how far before a candidate, in bytes, a keyword may appear.
const keywordWindow = 120

// DetectCode returns the verification code nearest after a keyword, or "" when none is
// found. Amounts, dates, times, years and order numbers are rejected.
func DetectCode(subject, text string) string {
	subjectHasKeyword := hasKeyword(subject)
	for _, source := range []string{subject, text} {
		if code := firstCode(source, subjectHasKeyword); code != "" {
			return code
		}
	}
	return ""
}

func firstCode(s string, keywordElsewhere bool) string {
	for _, loc := range candidate.FindAllStringIndex(s, -1) {
		raw := s[loc[0]:loc[1]]
		code := strings.NewReplacer(" ", "", "-", "").Replace(raw)
		if !plausible(code) || embedded(s, loc[0], loc[1]) {
			continue
		}
		window := s[max(0, loc[0]-keywordWindow):loc[0]]
		if hasKeyword(window) || (keywordElsewhere && len(s) <= keywordWindow*2) {
			return code
		}
	}
	return ""
}

func hasKeyword(s string) bool {
	if englishKeyword.MatchString(s) {
		return true
	}
	for _, keyword := range otherKeywords {
		if strings.Contains(s, keyword) {
			return true
		}
	}
	return false
}

// plausible rejects lengths and shapes that are rarely codes.
func plausible(code string) bool {
	if len(code) < 4 || len(code) > 8 {
		return false
	}
	digits := 0
	for _, r := range code {
		if unicode.IsDigit(r) {
			digits++
		}
	}
	if digits == len(code) {
		// A bare four-digit year is far more common than a four-digit code near "code".
		return !(len(code) == 4 && (strings.HasPrefix(code, "19") || strings.HasPrefix(code, "20")))
	}
	// Letter-digit codes need at least two digits and two letters to avoid plain words.
	return digits >= 2 && len(code)-digits >= 2
}

// embedded rejects numbers that are part of amounts, dates, times, versions, phone
// numbers or identifiers such as "#12345678" and "ORD-123456".
// Sentence punctuation after a code ("is 482913.") is allowed; the same characters
// between digits ("3,284.50", "2026.10.15", "10:32") are not.
func embedded(s string, start, end int) bool {
	before, beforeNext := lastRunes(s[:start])
	after, afterNext := firstRunes(s[end:])
	switch before {
	case '#', '-', '_', '@', '/', '$', '¥', '€', '£', '￥', '+', '=':
		return true
	case '.', ',', ':':
		if unicode.IsDigit(beforeNext) {
			return true
		}
	}
	switch after {
	case '#', '-', '_', '@', '/', '%', '元', '円':
		return true
	case '.', ',', ':':
		if unicode.IsDigit(afterNext) {
			return true
		}
	}
	return false
}

// firstRunes returns the first two runes of s.
func firstRunes(s string) (rune, rune) {
	var r [2]rune
	i := 0
	for _, c := range s {
		if i == 2 {
			break
		}
		r[i] = c
		i++
	}
	return r[0], r[1]
}

// lastRunes returns the last rune of s and the rune before it.
func lastRunes(s string) (rune, rune) {
	r := []rune(s)
	switch len(r) {
	case 0:
		return 0, 0
	case 1:
		return r[0], 0
	}
	return r[len(r)-1], r[len(r)-2]
}

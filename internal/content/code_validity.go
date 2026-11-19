package content

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// Capture complete quantities so decimal, range and compound durations stay unknown.
	validityQuantity       = `[+-]?[0-9]+(?:[.,][0-9]+)?(?:\s*[-–—~～至到]\s*[0-9]+)?`
	validityUnit           = `(?:seconds?|secs?|s|minutes?|mins?|m|hours?|hrs?|h)\b|秒|分钟|分鐘|小时|小時`
	validityDuration       = `(` + validityQuantity + `)\s*(` + validityUnit + `)((?:\s*(?:(?:and|or|to|[-–—~～至到/或和又])\s*)?` + validityQuantity + `\s*(?:` + validityUnit + `))*)`
	validityCode           = `(?:[0-9]{3,4}[ -][0-9]{3,4}|[a-z0-9]*[0-9][a-z0-9]*)`
	englishCodeNoun        = `\b(?:code|otp|passcode|pin)\b`
	englishValiditySubject = `(?:` + englishCodeNoun + `(?:\s+` + validityCode + `)?\s+|` +
		englishCodeNoun + `(?:\s+is)?\s*:?\s*` + validityCode + `[.!?;]\s*it\s+|` +
		`(?:^|[.!?;])\s*|\b(?:and|but)\s+)`
	chineseValiditySubject = `(?:(?:验证码|驗證碼|校验码|校驗碼|安全码|安全碼|动态码|動態碼)(?:\s*(?:为|為|是|[:：])?\s*[A-Z0-9]{4,8})?\s*[，,：:]?\s*|(?:^|[。！？!?；;，,（(])\s*)`
)

// Code subjects and standalone validity clauses distinguish expiry from link/session lifetimes.
// Each pattern captures quantity, unit and the remainder of a compound duration.
var validityPhrases = []*regexp.Regexp{
	regexp.MustCompile(`(?i)` + englishValiditySubject + `(?:(?:(?:is|will\s+be|remains?)\s+)?(?:only\s+)?(?:valid|active|good)\s+for|` +
		`(?:will\s+)?expires?\s+(?:in|after)|(?:will\s+)?remain\s+(?:active|valid)\s+for)` +
		`\s+(?:only\s+)?(?:the\s+next\s+)?` + validityDuration),
	regexp.MustCompile(`(?i)` + englishValiditySubject + `(?:validity|expiry|expiration)(?:\s+(?:time|period))?\s*(?:is|:)?\s*` + validityDuration),
	regexp.MustCompile(`(?i)\b(?:enter|use)\s+(?:(?:this|the|your)\s+)?(?:(?:verification|security|login|one[- ]time|authentication|confirmation|access)\s+)?code(?:\s+` + validityCode + `)?\s+within\s+(?:the\s+next\s+)?` + validityDuration),
	regexp.MustCompile(chineseValiditySubject + `(?:将在|将于|將在|將於)?\s*` + validityDuration + `\s*(?:内有效|內有效|有效|后失效|後失效|后过期|後過期)`),
	regexp.MustCompile(chineseValiditySubject + `有效期\s*(?:为|為|是|:|：)?\s*` + validityDuration),
	regexp.MustCompile(`(?:请在|请于|請在|請於)\s*` + validityDuration + `\s*[内內]\s*(?:(?:输入|輸入|使用|验证|驗證)\s*(?:以下|此|本|该|該)?\s*(?:验证码|驗證碼|安全码|安全碼)|完成(?:登录|登入|验证|驗證))`),
}

// codeLifetime returns one explicit whole-number duration for a detected code.
// Conflicting, unsupported or unrepresentable durations keep expiry unknown.
func codeLifetime(subject, text, code string) time.Duration {
	if code == "" {
		return 0
	}
	var lifetime time.Duration
	for _, source := range []string{subject, text} {
		source = strings.Join(strings.Fields(source), " ")
		for _, phrase := range validityPhrases {
			for _, match := range phrase.FindAllStringSubmatch(source, -1) {
				amount, err := strconv.ParseInt(match[1], 10, 64)
				if err != nil || amount <= 0 || match[3] != "" {
					return 0
				}
				unit := time.Second
				switch strings.ToLower(match[2]) {
				case "分钟", "分鐘", "minute", "minutes", "min", "mins", "m":
					unit = time.Minute
				case "小时", "小時", "hour", "hours", "hr", "hrs", "h":
					unit = time.Hour
				}
				if amount > math.MaxInt64/int64(unit) {
					return 0
				}
				value := time.Duration(amount) * unit
				if lifetime != 0 && lifetime != value {
					return 0
				}
				lifetime = value
			}
		}
	}
	return lifetime
}

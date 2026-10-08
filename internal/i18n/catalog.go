// Package i18n serves the shared English and Simplified Chinese message catalogs.
package i18n

import (
	"embed"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/mingzaily/mailwake/internal/fault"
	"golang.org/x/text/language"
)

const Default = "en"

//go:embed locales/*.json
var files embed.FS

var catalogs = load()
var placeholder = regexp.MustCompile(`\{([a-z_]+)\}`)
var matcher = language.NewMatcher([]language.Tag{language.English, language.SimplifiedChinese})

func load() map[string]map[string]string {
	result := make(map[string]map[string]string)
	for _, locale := range []string{"en", "zh-CN"} {
		data, err := files.ReadFile("locales/" + locale + ".json")
		if err != nil {
			panic(err)
		}
		var messages map[string]string
		if err := json.Unmarshal(data, &messages); err != nil {
			panic(err)
		}
		result[locale] = messages
	}
	return result
}

func Supported(locale string) bool { _, ok := catalogs[locale]; return ok }

// Match honors Accept-Language weights and falls back to English.
func Match(header string) string {
	tags, _, err := language.ParseAcceptLanguage(header)
	if err != nil || len(tags) == 0 {
		return Default
	}
	_, index, confidence := matcher.Match(tags...)
	if confidence == language.No {
		return Default
	}
	return []string{"en", "zh-CN"}[index]
}

func Catalog(locale string) ([]byte, error) { return files.ReadFile("locales/" + locale + ".json") }

// ErrorMessage renders log and CLI errors in the requested language.
func ErrorMessage(locale string, err error) string {
	var coded *fault.Error
	if errors.As(err, &coded) {
		return Message(locale, coded.Code, coded.Params)
	}
	return err.Error()
}

func Message(locale, key string, params map[string]string) string {
	text := catalogs[locale][key]
	if text == "" {
		text = catalogs[Default][key]
	}
	if text == "" {
		text = catalogs[Default]["unknown_error"]
	}
	return placeholder.ReplaceAllStringFunc(text, func(token string) string {
		if value, ok := params[token[1:len(token)-1]]; ok {
			return value
		}
		return token
	})
}

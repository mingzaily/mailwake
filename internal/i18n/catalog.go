// Package i18n serves the shared message catalogs and language negotiation.
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

//go:embed languages.json
var languageData []byte

type Language struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// Languages returns the supported catalog identifiers and native names.
func Languages() []Language {
	var result []Language
	if err := json.Unmarshal(languageData, &result); err != nil {
		panic(err)
	}
	return result
}

// Resolve maps BCP 47 language variants to a catalog, keeping Chinese scripts distinct.
func Resolve(value string) (string, bool) {
	tag, err := language.Parse(value)
	if err != nil || value == "" {
		return "", false
	}
	base, _, _ := tag.Raw()
	switch base.String() {
	case "zh":
		script, _ := tag.Script()
		if script.String() == "Hant" {
			return "zh-Hant", true
		}
		return "zh-CN", true
	case "pt":
		return "pt-BR", true
	case "en", "ja", "ko", "de", "fr", "es":
		return base.String(), true
	default:
		return "", false
	}
}

func load() map[string]map[string]string {
	result := make(map[string]map[string]string)
	for _, item := range Languages() {
		locale := item.Code
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
	for _, tag := range tags {
		if tag == language.Und {
			return Default
		}
		if locale, ok := Resolve(tag.String()); ok {
			return locale
		}
	}
	return Default
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

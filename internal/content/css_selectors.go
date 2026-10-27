package content

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/tdewolff/parse/v2/css"
	"golang.org/x/net/html"
)

var cssIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]{0,63}$|^-[a-zA-Z_][a-zA-Z0-9_-]{0,62}$`)

func (s *bodySanitizer) identifier(value string) string {
	if !cssIdentifier.MatchString(value) {
		return ""
	}
	return s.cssPrefix + value
}

func (s *bodySanitizer) classes(value string) string {
	values := cssFields(value)
	if len(values) > 32 {
		s.oversized = true
		return ""
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if clean := s.identifier(v); clean != "" {
			out = append(out, clean)
		}
	}
	return strings.Join(out, " ")
}

type cssCompound struct {
	tag          string
	ids, classes []string
	root         bool
}

type cssSelector struct {
	text        string
	compounds   []cssCompound
	combinators []string
	specificity cssSpecificity
}

type cssSpecificity struct{ ids, classes, types int }

// Each universal alternative matches unconditionally with one unit of weight.
// Simple units preserve specificity without re-running structural selectors in
// the browser after Core has already bound their original targets.
func (w cssSpecificity) selector() string {
	return strings.Repeat(":is(#_,*)", w.ids) + strings.Repeat(":is(._,*)", w.classes) + strings.Repeat(":is(mw,*)", w.types)
}

// Parse the static selector subset once for matching and specificity emission.
func (s *bodySanitizer) selector(tokens []css.Token) (cssSelector, bool) {
	var result cssSelector
	if len(tokens) > 128 {
		return result, false
	}
	var parts []string
	var current strings.Builder
	var compound cssCompound
	finish := func() bool {
		if current.Len() == 0 {
			return false
		}
		parts = append(parts, current.String())
		result.compounds = append(result.compounds, compound)
		current.Reset()
		compound = cssCompound{}
		return len(parts) <= 16
	}
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		v := string(t.Data)
		switch t.TokenType {
		case css.WhitespaceToken:
			if current.Len() > 0 {
				if !finish() {
					return result, false
				}
				result.combinators = append(result.combinators, " ")
			}
		case css.IdentToken:
			if current.Len() > 0 {
				return result, false
			}
			v = strings.ToLower(v)
			if !bodyTags[v] && v != "html" && v != "body" {
				return result, false
			}
			compound.tag = v
			result.specificity.types++
			current.WriteString(v)
		case css.HashToken:
			if len(v) < 2 || s.identifier(v[1:]) == "" {
				return result, false
			}
			compound.ids = append(compound.ids, v[1:])
			result.specificity.ids++
			current.WriteString("#" + s.identifier(v[1:]))
		case css.ColonToken:
			if i+1 >= len(tokens) || tokens[i+1].TokenType != css.IdentToken || !strings.EqualFold(string(tokens[i+1].Data), "root") {
				return result, false
			}
			compound.root = true
			result.specificity.classes++
			current.WriteString(":root")
			i++
		case css.DelimToken:
			switch v {
			case ".":
				if i+1 >= len(tokens) || tokens[i+1].TokenType != css.IdentToken {
					return result, false
				}
				name := string(tokens[i+1].Data)
				if s.identifier(name) == "" {
					return result, false
				}
				compound.classes = append(compound.classes, name)
				result.specificity.classes++
				current.WriteString("." + s.identifier(name))
				i++
			case "*":
				if current.Len() > 0 {
					return result, false
				}
				compound.tag = "*"
				current.WriteByte('*')
			case ">", "+", "~":
				if current.Len() > 0 {
					if !finish() {
						return result, false
					}
					result.combinators = append(result.combinators, v)
				} else if len(result.combinators) > 0 && result.combinators[len(result.combinators)-1] == " " {
					result.combinators[len(result.combinators)-1] = v
				} else {
					return result, false
				}
			default:
				return result, false
			}
		default:
			return result, false
		}
	}
	if !finish() {
		if len(result.combinators) == 0 || result.combinators[len(result.combinators)-1] != " " {
			return result, false
		}
		result.combinators = result.combinators[:len(result.combinators)-1]
	}
	if len(parts) == 0 || len(result.combinators) != len(parts)-1 {
		return result, false
	}
	result.text = parts[0]
	for i, c := range result.combinators {
		result.text += c + parts[i+1]
	}
	return result, true
}

func (s *bodySanitizer) scope() string {
	return `:where(mailwake-html[data-mailwake-scope="` + s.cssPrefix + `"])`
}

func (s *bodySanitizer) selectorList(tokens []css.Token) (string, bool) {
	var selectors []cssSelector
	start := 0
	for i := 0; i <= len(tokens); i++ {
		if i < len(tokens) && tokens[i].TokenType != css.CommaToken {
			continue
		}
		selector, ok := s.selector(tokens[start:i])
		if !ok || len(selectors) >= 32 {
			return "", false
		}
		selectors = append(selectors, selector)
		start = i + 1
	}
	var out []string
	for _, selector := range selectors {
		value := s.bindSelector(selector)
		if s.err != nil || s.oversized {
			return "", false
		}
		out = append(out, value)
	}
	return strings.Join(out, ","), true
}

// Bind static selectors to their original targets before DOM rewriting changes
// relationships. :where contributes zero specificity; simple :is units give
// every bound target exactly the original selector's specificity. Media
// evaluation, source order and !important remain native browser behavior.
func (s *bodySanitizer) bindSelector(selector cssSelector) string {
	if value, ok := s.selectors[selector.text]; ok {
		return value
	}
	matches := s.matchSelector(selector)
	if s.err != nil || s.oversized {
		return ""
	}
	marker := s.cssPrefix + "s" + strconv.Itoa(len(s.selectors))
	for i, matched := range matches {
		clean := s.elements[i].clean
		if !matched || clean == nil {
			continue
		}
		s.css.markerBytes += len(marker) + 1
		if s.css.markerBytes > MaxHTMLBytes {
			s.oversized = true
			return ""
		}
		// Generated attributes are appended after all authored attributes.
		last := len(clean.Attr) - 1
		if last >= 0 && clean.Attr[last].Key == "data-mailwake-match" {
			clean.Attr[last].Val += " " + marker
		} else {
			clean.Attr = append(clean.Attr, html.Attribute{Key: "data-mailwake-match", Val: marker})
		}
	}
	value := `:where([data-mailwake-match~="` + marker + `"])` + selector.specificity.selector()
	s.selectors[selector.text] = value
	return value
}

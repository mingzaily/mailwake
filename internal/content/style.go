package content

import (
	"regexp"
	"strings"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
)

var (
	cssProperties    = wordSet("color background background-color background-image background-position background-repeat background-size border border-top border-bottom border-left border-right border-color border-style border-width border-collapse border-spacing border-radius box-sizing display float clear width height min-width max-width min-height max-height margin margin-top margin-right margin-bottom margin-left padding padding-top padding-right padding-bottom padding-left font font-family font-size font-style font-weight line-height letter-spacing text-align text-decoration text-indent text-transform vertical-align white-space word-break word-wrap overflow-wrap list-style-type list-style-position table-layout opacity visibility overflow overflow-x overflow-y direction")
	cssFunctions     = wordSet("rgb( rgba( hsl( hsla( calc( min( max( clamp( linear-gradient( radial-gradient( repeating-linear-gradient( repeating-radial-gradient(")
	cssColor         = regexp.MustCompile(`^(#[0-9a-fA-F]{3,8}|[a-zA-Z]{1,24})$`)
	visibilityValues = wordSet("visible hidden collapse inherit initial unset revert")
	overflowValues   = wordSet("visible hidden clip scroll auto inherit initial unset revert")
	directionValues  = wordSet("ltr rtl inherit initial unset revert")
	cssFont          = regexp.MustCompile(`^[a-zA-Z0-9 ,'-]{1,128}$`)
	cssRootLength    = regexp.MustCompile(`(?i)(rem|rex|rch|rcap|ric|rlh)$`)
)

// style accepts parsed declarations and a small passive value grammar. Escapes,
// variables, at-rules and unknown functions are discarded rather than interpreted.
func (s *bodySanitizer) style(source string) (string, bool) {
	components, ok := s.cssInput(source)
	if !ok {
		return "", false
	}
	return s.styleDeclarations(source, components, false)
}

// Declaration lists accept top-level declarations only. At-rule contents are
// discarded as a unit before property validation or CID resolution.
func (s *bodySanitizer) styleDeclarations(source string, components []cssComponent, ruleBody bool) (string, bool) {
	var out strings.Builder
	remote := false
	for i := 0; i < len(components); {
		if err := s.ctx.Err(); err != nil {
			s.err = err
		}
		if s.err != nil || s.oversized {
			return "", false
		}
		first := components[i]
		if first.kind == css.WhitespaceToken || first.kind == css.CommentToken || first.kind == css.SemicolonToken {
			i++
			continue
		}
		atRule := first.kind == css.AtKeywordToken
		nestedRule := ruleBody && first.kind != css.CustomPropertyNameToken
		end := first.end
		container := false
		for i < len(components) {
			c := components[i]
			i++
			end = c.end
			// Custom-property values own their blocks through the semicolon.
			// Other blocks in style-rule bodies are unsupported nested rules;
			// inline attributes retain declaration-list recovery instead.
			container = c.kind == css.LeftBraceToken && (atRule || nestedRule)
			if c.kind == css.SemicolonToken || container {
				break
			}
		}
		if atRule || container {
			continue
		}
		p := css.NewParser(parse.NewInputString(source[first.start:end]), true)
		grammar, _, name := p.Next()
		if grammar != css.DeclarationGrammar {
			continue
		}
		value, isRemote := s.declaration(string(name), p.Values())
		if out.Len()+len(value) > MaxHTMLBytes {
			s.oversized = true
			return "", false
		}
		out.WriteString(value)
		remote = remote || isRemote
	}
	return out.String(), remote
}

// Check the entire declaration before resolving any same-message CID bytes.
func (s *bodySanitizer) styleValue(property string, tokens []css.Token) (string, bool, bool) {
	values := make([]string, len(tokens))
	urls := make([]bool, len(tokens))
	functions := 0
	for i, token := range tokens {
		data := string(token.Data)
		if strings.ContainsAny(data, "\\\x00\r\n\f<>") {
			return "", false, false
		}
		switch token.TokenType {
		case css.WhitespaceToken:
			data = " "
		case css.DimensionToken:
			// These units resolve against the host document root after embedding.
			// Discard the declaration and retain its earlier fallback or inheritance.
			if cssRootLength.MatchString(data) {
				return "", false, false
			}
		case css.IdentToken, css.NumberToken, css.PercentageToken, css.HashToken, css.CommaToken:
		case css.StringToken:
			// EOF also closes CSS strings. Serialize the closing quote before
			// combining this declaration with another declaration or rule.
			if len(data) == 1 || data[len(data)-1] != data[0] {
				data += data[:1]
			}
		case css.FunctionToken:
			if !cssFunctions[strings.ToLower(data)] {
				return "", false, false
			}
			functions++
		case css.RightParenthesisToken:
			if functions == 0 {
				return "", false, false
			}
			functions--
		case css.DelimToken:
			if !strings.Contains("/+-*!", data) {
				return "", false, false
			}
		case css.URLToken:
			if property != "background" && property != "background-image" {
				return "", false, false
			}
			start := strings.IndexByte(data, '(')
			if start < 0 || !strings.HasSuffix(data, ")") {
				return "", false, false
			}
			data = strings.TrimSpace(data[start+1 : len(data)-1])
			if len(data) >= 2 && ((data[0] == '"' && data[len(data)-1] == '"') || (data[0] == '\'' && data[len(data)-1] == '\'')) {
				data = data[1 : len(data)-1]
			}
			if !strings.HasPrefix(strings.ToLower(data), "cid:") && validURL(data, false) == "" {
				return "", false, false
			}
			urls[i] = true
		default:
			return "", false, false
		}
		values[i] = data
	}
	var out strings.Builder
	remote := false
	for i, data := range values {
		if urls[i] {
			target, isRemote := s.imageURL(data)
			if target == "" {
				return "", false, false
			}
			data = `url("` + strings.ReplaceAll(target, `"`, "%22") + `")`
			remote = remote || isRemote
		}
		if out.Len()+len(data) > MaxHTMLBytes {
			s.oversized = true
			return "", false, false
		}
		out.WriteString(data)
	}
	// Preserve CSS's implicit EOF closure while keeping every emitted value
	// self-contained when inline declarations and style elements are combined.
	if out.Len()+functions > MaxHTMLBytes {
		s.oversized = true
		return "", false, false
	}
	out.WriteString(strings.Repeat(")", functions))
	value := out.String()
	keyword := cssTrim(strings.ToLower(value))
	var allowed map[string]bool
	switch property {
	case "visibility":
		allowed = visibilityValues
	case "overflow", "overflow-x", "overflow-y":
		allowed = overflowValues
	case "direction":
		allowed = directionValues
	}
	if allowed != nil {
		parts := cssFields(keyword)
		if len(parts) == 0 || len(parts) > 1 && (property != "overflow" || len(parts) > 2) {
			return "", false, false
		}
		for _, part := range parts {
			if !allowed[part] {
				return "", false, false
			}
		}
	}
	return value, remote, true
}

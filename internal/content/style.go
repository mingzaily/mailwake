package content

import (
	"regexp"
	"strings"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
)

var (
	cssProperties = wordSet("color background background-color background-image background-position background-repeat background-size border border-top border-bottom border-left border-right border-color border-style border-width border-collapse border-spacing border-radius box-sizing display float clear width height min-width max-width min-height max-height margin margin-top margin-right margin-bottom margin-left padding padding-top padding-right padding-bottom padding-left font font-family font-size font-style font-weight line-height letter-spacing text-align text-decoration text-indent text-transform vertical-align white-space word-break word-wrap overflow-wrap list-style-type list-style-position table-layout opacity")
	cssFunctions  = wordSet("rgb( rgba( hsl( hsla( calc( min( max( clamp( linear-gradient( radial-gradient( repeating-linear-gradient( repeating-radial-gradient(")
	cssColor      = regexp.MustCompile(`^(#[0-9a-fA-F]{3,8}|[a-zA-Z]{1,24})$`)
	cssFont       = regexp.MustCompile(`^[a-zA-Z0-9 ,'-]{1,128}$`)
)

// style accepts parsed declarations and a small passive value grammar. Escapes,
// variables, at-rules and unknown functions are discarded rather than interpreted.
func (s *bodySanitizer) style(source string) (string, bool) {
	p := css.NewParser(parse.NewInputString(source), true)
	var out strings.Builder
	remote := false
	for {
		grammar, _, name := p.Next()
		if grammar == css.ErrorGrammar {
			break
		}
		property := strings.ToLower(string(name))
		if grammar != css.DeclarationGrammar || !cssProperties[property] {
			continue
		}
		value, isRemote, ok := s.styleValue(property, p.Values())
		if ok && value != "" {
			if out.Len()+len(property)+len(value)+2 > MaxHTMLBytes {
				s.oversized = true
				return "", false
			}
			out.WriteString(property + ":" + value + ";")
			remote = remote || isRemote
		}
	}
	return out.String(), remote
}

// Check the entire declaration before resolving any same-message CID bytes.
func (s *bodySanitizer) styleValue(property string, tokens []css.Token) (string, bool, bool) {
	values := make([]string, len(tokens))
	urls := make([]bool, len(tokens))
	for i, token := range tokens {
		data := string(token.Data)
		if strings.ContainsAny(data, "\\\x00\r\n\f") {
			return "", false, false
		}
		switch token.TokenType {
		case css.WhitespaceToken:
			data = " "
		case css.IdentToken, css.NumberToken, css.PercentageToken, css.DimensionToken, css.HashToken, css.StringToken, css.CommaToken, css.RightParenthesisToken:
		case css.FunctionToken:
			if !cssFunctions[strings.ToLower(data)] {
				return "", false, false
			}
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
	return out.String(), remote, true
}

package content

import (
	"io"
	"strings"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
)

const (
	maxCSSBytes        = 64 << 10
	maxCSSTokens       = 32768
	maxCSSRules        = 512
	maxCSSDeclarations = 4096
	maxCSSDepth        = 8
)

type cssBudget struct{ bytes, tokens, rules, declarations, matchWork, markerBytes int }

// Components retain source boundaries independently of grammar error recovery.
// A block includes its opening/closing delimiters and owns its child components.
type cssComponent struct {
	kind       css.TokenType
	start, end int
	children   []cssComponent
}

// Tokenize once, with a shared message budget, before parsing isolated rules and
// declarations. Strings, comments and URLs remain atomic lexer tokens.
func (s *bodySanitizer) cssInput(source string) ([]cssComponent, bool) {
	s.css.bytes += len(source)
	if s.css.bytes > maxCSSBytes {
		s.oversized = true
		return nil, false
	}
	input := parse.NewInputString(source)
	l := css.NewLexer(input)
	var components func(css.TokenType, int) ([]cssComponent, bool)
	components = func(closing css.TokenType, depth int) ([]cssComponent, bool) {
		var out []cssComponent
		for {
			if err := s.ctx.Err(); err != nil {
				s.err = err
				return nil, false
			}
			start := input.Offset()
			kind, _ := l.Next()
			if kind == css.ErrorToken {
				// CSS implicitly closes unfinished blocks at EOF.
				return out, l.Err() == io.EOF
			}
			s.css.tokens++
			if s.css.tokens > maxCSSTokens {
				s.oversized = true
				return nil, false
			}
			if kind == css.RightParenthesisToken || kind == css.RightBraceToken || kind == css.RightBracketToken {
				return out, kind == closing
			}
			c := cssComponent{kind: kind, start: start}
			var end css.TokenType
			switch kind {
			case css.FunctionToken, css.LeftParenthesisToken:
				end = css.RightParenthesisToken
			case css.LeftBraceToken:
				end = css.RightBraceToken
			case css.LeftBracketToken:
				end = css.RightBracketToken
			}
			if end != css.ErrorToken {
				if depth == maxCSSDepth {
					s.oversized = true
					return nil, false
				}
				var ok bool
				c.children, ok = components(end, depth+1)
				if !ok {
					return nil, false
				}
			}
			c.end = input.Offset()
			out = append(out, c)
		}
	}
	return components(css.ErrorToken, 0)
}

func cssText(tokens []css.Token) string {
	var out strings.Builder
	for _, t := range tokens {
		out.Write(t.Data)
	}
	return out.String()
}

func (s *bodySanitizer) declaration(property string, tokens []css.Token) (string, bool) {
	s.css.declarations++
	if s.css.declarations > maxCSSDeclarations {
		s.oversized = true
		return "", false
	}
	property = strings.ToLower(property)
	if !cssProperties[property] {
		return "", false
	}
	tokens, priority := cssPriority(tokens)
	value, remote, ok := s.styleValue(property, tokens)
	if !ok || value == "" {
		return "", false
	}
	return property + ":" + value + priority + ";", remote
}

// Priority belongs to the declaration, outside its value. Extract the trailing
// tokens before serializing the value's implicit EOF function closures.
func cssPriority(tokens []css.Token) ([]css.Token, string) {
	end := len(tokens)
	for end > 0 && tokens[end-1].TokenType == css.WhitespaceToken {
		end--
	}
	if end == 0 || tokens[end-1].TokenType != css.IdentToken || !strings.EqualFold(string(tokens[end-1].Data), "important") {
		return tokens, ""
	}
	bang := end - 2
	for bang >= 0 && tokens[bang].TokenType == css.WhitespaceToken {
		bang--
	}
	if bang >= 0 && tokens[bang].TokenType == css.DelimToken && string(tokens[bang].Data) == "!" {
		return tokens[:bang], "!important"
	}
	return tokens, ""
}

func (s *bodySanitizer) stylesheet(source string) (string, bool) {
	components, ok := s.cssInput(source)
	if !ok {
		return "", false
	}
	return s.styleRules(source, components, true)
}

// Each rule is parsed within its lexical block. Malformed or unsupported rules
// stay within that block, so their recovery cannot absorb a following sibling.
func (s *bodySanitizer) styleRules(source string, components []cssComponent, topLevel bool) (string, bool) {
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
		if first.kind == css.WhitespaceToken || first.kind == css.CommentToken || topLevel && (first.kind == css.CDOToken || first.kind == css.CDCToken) {
			i++
			continue
		}
		// At-rules end at a semicolon or block; qualified rules require a block.
		for i < len(components) && components[i].kind != css.LeftBraceToken && (first.kind != css.AtKeywordToken || components[i].kind != css.SemicolonToken) {
			i++
		}
		if i == len(components) {
			break
		}
		block := components[i]
		i++
		if block.kind != css.LeftBraceToken {
			continue
		}
		p := css.NewParser(parse.NewInputString(source[first.start:block.start+1]), false)
		grammar, _, name := p.Next()
		var header, body string
		var hasRemote bool
		switch grammar {
		case css.BeginRulesetGrammar:
			selectors, ok := s.selectorList(p.Values())
			s.css.rules++
			if s.css.rules > maxCSSRules {
				s.oversized = true
				return "", false
			}
			if ok {
				header = selectors
				body, hasRemote = s.styleDeclarations(source, block.children, true)
			}
		case css.BeginAtRuleGrammar:
			media, ok := mediaCondition(cssText(p.Values()), true)
			if strings.EqualFold(string(name), "@media") && media != "" && ok {
				header = "@media " + media
				body, hasRemote = s.styleRules(source, block.children, false)
			}
		}
		if body == "" {
			continue
		}
		if out.Len()+len(header)+len(body)+2 > MaxHTMLBytes {
			s.oversized = true
			return "", false
		}
		out.WriteString(header + "{" + body + "}")
		remote = remote || hasRemote
	}
	return out.String(), remote
}

package content

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// htmlToText renders HTML as readable text: block elements become line breaks, list items
// get bullets, links keep their target, and scripts, styles and hidden content are dropped.
// No resource is ever loaded.
func htmlToText(source string) string {
	if source == "" {
		return ""
	}
	z := html.NewTokenizer(strings.NewReader(source))
	var b strings.Builder
	skip := 0
	var links []string
	newline := func() {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
	}
	for {
		switch z.Next() {
		case html.ErrorToken:
			return b.String()
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			switch tag {
			case "script", "style", "head", "title", "template", "noscript":
				if z.Token().Type == html.StartTagToken {
					skip++
				}
				continue
			case "br":
				b.WriteByte('\n')
			case "p", "div", "tr", "table", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "section", "article", "ul", "ol":
				newline()
			case "li":
				newline()
				b.WriteString("• ")
			case "td", "th":
				b.WriteByte(' ')
			case "a":
				href := ""
				for hasAttr {
					var key, value []byte
					key, value, hasAttr = z.TagAttr()
					if string(key) == "href" {
						href = string(value)
					}
				}
				links = append(links, safeLink(href))
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)
			switch tag {
			case "script", "style", "head", "title", "template", "noscript":
				if skip > 0 {
					skip--
				}
			case "p", "div", "tr", "table", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "section", "article", "li":
				newline()
			case "a":
				if n := len(links); n > 0 {
					if href := links[n-1]; href != "" && !strings.Contains(b.String()[max(0, b.Len()-len(href)-8):], href) {
						b.WriteString(" (" + href + ")")
					}
					links = links[:n-1]
				}
			}
		case html.TextToken:
			if skip > 0 {
				continue
			}
			text := strings.Join(strings.Fields(string(z.Text())), " ")
			if text == "" {
				continue
			}
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") && !strings.HasSuffix(b.String(), " ") {
				b.WriteByte(' ')
			}
			b.WriteString(text)
		}
	}
}

// safeLink keeps only absolute http(s) and mailto targets.
func safeLink(href string) string {
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "mailto") {
		return ""
	}
	return u.String()
}

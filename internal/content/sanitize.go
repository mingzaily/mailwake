package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/image/colornames"
	_ "golang.org/x/image/webp"
	"golang.org/x/net/html"
)

var (
	bodyTags        = wordSet("a abbr b bdi bdo blockquote br caption center code col colgroup dd del div dl dt em figcaption figure font h1 h2 h3 h4 h5 h6 hr i img ins li mark ol p picture pre q s samp section small source span strike strong sub sup table tbody td tfoot th thead tr tt u ul")
	directions      = wordSet("ltr rtl auto")
	horizontalAlign = wordSet("left right center justify")
	verticalAlign   = wordSet("top middle bottom baseline")
	imageMedia      = map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif", "webp": "image/webp"}
	discardTags     = wordSet("script style title template noscript iframe frame frameset object embed applet svg math form input button select option textarea video audio track canvas base link meta")
	language        = regexp.MustCompile(`^[a-zA-Z]{1,8}(?:-[a-zA-Z0-9]{1,8})*$`)
	dimension       = regexp.MustCompile(`^[0-9]{1,5}%?$`)
)

func wordSet(words string) map[string]bool {
	result := map[string]bool{}
	for _, word := range strings.Fields(words) {
		result[word] = true
	}
	return result
}

type bodySanitizer struct {
	ctx          context.Context
	err          error
	resources    map[string]*bodyPart
	images       map[string]string
	oversized    bool
	remote       bool
	cssPrefix    string
	css          cssBudget
	styles       strings.Builder
	rootHints    strings.Builder
	elements     []cssElement
	elementIndex map[*html.Node]int
	selectors    map[string]string
}

// sanitizeBody rebuilds an allowlisted HTML tree. No URL is dereferenced.
func sanitizeBody(ctx context.Context, source string, resources map[string]*bodyPart) (string, bool, bool, error) {
	if source == "" {
		return "", false, false, nil
	}
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return "", false, false, errInvalidBody
	}
	digest := sha256.Sum256([]byte(source))
	s := bodySanitizer{ctx: ctx, resources: resources, images: map[string]string{}, cssPrefix: "mw-" + hex.EncodeToString(digest[:8]) + "-"}
	if err := s.indexElements(doc); err != nil {
		if errors.Is(err, errHTMLLimit) {
			return "", false, true, nil
		}
		return "", false, false, err
	}
	output := &html.Node{Type: html.DocumentNode}
	retainedBytes := 0
	var styleElements []*html.Node
	var visit func(*html.Node, *html.Node) error
	visit = func(n, parent *html.Node) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch n.Type {
		case html.TextNode:
			retainedBytes += len(n.Data)
			if retainedBytes > MaxHTMLBytes {
				return errHTMLLimit
			}
			parent.AppendChild(&html.Node{Type: html.TextNode, Data: n.Data})
			return nil
		case html.ElementNode:
			if n.Namespace == "" && n.Data == "style" {
				styleElements = append(styleElements, n)
				return nil
			}
			if n.Namespace != "" || discardTags[n.Data] {
				return nil
			}
			if bodyTags[n.Data] || n.Data == "html" || n.Data == "body" {
				if n.Data == "source" && n.Parent.Data != "picture" {
					return nil
				}
				clean := &html.Node{Type: html.ElementNode, Data: n.Data, DataAtom: n.DataAtom}
				var retain bool
				clean.Attr, retain = s.attributes(n)
				if !retain {
					return nil
				}
				if n.Data == "html" || n.Data == "body" {
					clean.Data = "mailwake-" + n.Data
					clean.DataAtom = 0
					if n.Data == "html" {
						clean.Attr = append(clean.Attr, html.Attribute{Key: "data-mailwake-root"}, html.Attribute{Key: "data-mailwake-scope", Val: s.cssPrefix})
					}
					s.rootPresentation(n)
				}
				if s.err != nil {
					return s.err
				}
				for _, a := range clean.Attr {
					retainedBytes += len(a.Val)
				}
				if s.oversized || retainedBytes > MaxHTMLBytes {
					return errHTMLLimit
				}
				parent.AppendChild(clean)
				s.elements[s.elementIndex[n]].clean = clean
				parent = clean
			} else if parent.Data == "picture" {
				// Picture selection uses direct children. Keep a neutral boundary
				// when replacing an unsupported wrapper around fallback images.
				clean := &html.Node{Type: html.ElementNode, Data: "span"}
				parent.AppendChild(clean)
				parent = clean
			}
		case html.CommentNode, html.DoctypeNode:
			return nil
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if err := visit(child, parent); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(doc, output); err != nil {
		if errors.Is(err, errHTMLLimit) {
			return "", false, true, nil
		}
		return "", false, false, err
	}
	preferred := s.preferredStyleSet()
	for _, n := range styleElements {
		s.styleElement(n, preferred)
		if s.err != nil {
			return "", false, false, s.err
		}
		if s.oversized {
			return "", false, true, nil
		}
	}
	if s.oversized {
		return "", false, true, nil
	}
	rootDefault := s.scope() + `:where(:not([hidden]),[hidden="until-found"])`
	bodyDefault := s.scope() + ` > :where(mailwake-body:not([hidden]),mailwake-body[hidden="until-found"])`
	baseline := rootDefault + "," + bodyDefault + "{display:block;}"
	sheet := baseline + s.rootHints.String() + s.styles.String()
	if len(sheet) > MaxHTMLBytes {
		return "", false, true, nil
	}
	style := &html.Node{Type: html.ElementNode, Data: "style"}
	style.AppendChild(&html.Node{Type: html.TextNode, Data: sheet})
	output.InsertBefore(style, output.FirstChild)
	var out htmlBuffer
	if err := html.Render(&out, output); err != nil {
		if errors.Is(err, errHTMLLimit) {
			return "", false, true, nil
		}
		return "", false, false, err
	}
	return out.String(), s.remote, false, nil
}

var errHTMLLimit = errors.New("html_limit")

type htmlBuffer struct{ bytes.Buffer }

func (b *htmlBuffer) Write(p []byte) (int, error) {
	if len(p) > MaxHTMLBytes-b.Len() {
		return 0, errHTMLLimit
	}
	return b.Buffer.Write(p)
}

// Render uses io.StringWriter when available, so both paths enforce the limit.
func (b *htmlBuffer) WriteString(p string) (int, error) { return b.Write([]byte(p)) }
func (b *htmlBuffer) WriteByte(p byte) error            { _, err := b.Write([]byte{p}); return err }

// attributes validates image conditions before resolving resources. An unsupported
// source condition removes the element as a unit, preserving its fallback order.
func (s *bodySanitizer) attributes(n *html.Node) ([]html.Attribute, bool) {
	var out []html.Attribute
	seen := map[string]bool{}
	sizes, sizesOK := "", true
	if n.Data == "img" || n.Data == "source" {
		sizes, sizesOK = imageSizes(firstAttribute(n, "sizes"))
	}
	sourceMedia, sourceType := "", ""
	if n.Data == "source" {
		var mediaOK bool
		sourceMedia, mediaOK = mediaCondition(firstAttribute(n, "media"), true)
		sourceType = strings.ToLower(cssTrim(firstAttribute(n, "type")))
		if !mediaOK || !sizesOK || (sourceType != "" && sourceType != "image/png" && sourceType != "image/jpeg" && sourceType != "image/gif" && sourceType != "image/webp") {
			return nil, false
		}
	}
	for _, a := range n.Attr {
		if a.Namespace != "" || seen[a.Key] {
			continue
		}
		seen[a.Key] = true
		value := ""
		allowEmpty := false
		switch a.Key {
		case "aria-hidden":
			candidate := strings.ToLower(strings.Trim(a.Val, " \t\r\n\f"))
			switch candidate {
			case "true", "false", "undefined":
				value = candidate
			}
		case "hidden":
			// HTML's empty and invalid-value defaults are Hidden, including "false".
			allowEmpty = true
			if strings.EqualFold(a.Val, "until-found") {
				value = "until-found"
			}
		case "role":
			value = passiveRole(a.Val)
		case "class":
			value = s.classes(a.Val)
		case "id":
			value = s.identifier(a.Val)
		case "title":
			value = a.Val
		case "lang":
			if len(a.Val) <= 64 && (a.Val == "" || language.MatchString(a.Val)) {
				value = a.Val
				allowEmpty = true
			}
		case "media":
			value = sourceMedia
		case "type":
			value = sourceType
		case "sizes":
			if (n.Data == "img" || n.Data == "source") && sizesOK {
				value = sizes
			}
		case "dir":
			if candidate := strings.ToLower(a.Val); directions[candidate] {
				value = candidate
			}
		case "style":
			var remote bool
			value, remote = s.style(a.Val)
			s.remote = s.remote || remote
		case "href":
			if n.Data == "a" {
				value = safeLink(a.Val)
			}
		case "src":
			if n.Data == "img" {
				var remote bool
				value, remote = s.imageURL(a.Val)
				s.remote = s.remote || remote
			}
		case "srcset":
			if (n.Data == "img" || n.Data == "source") && sizesOK {
				var remote bool
				value, remote = s.srcset(a.Val)
				s.remote = s.remote || remote
			}
		case "alt":
			if n.Data == "img" {
				value = a.Val
				allowEmpty = true
			}
		case "width", "height":
			if dimension.MatchString(a.Val) {
				value = a.Val
			}
		case "colspan", "rowspan", "cellpadding", "cellspacing", "border", "span", "start":
			if dimension.MatchString(a.Val) && !strings.Contains(a.Val, "%") {
				value = a.Val
			}
		case "align":
			if horizontalAlign[a.Val] {
				value = a.Val
			}
		case "valign":
			if verticalAlign[a.Val] {
				value = a.Val
			}
		case "bgcolor", "color":
			if cssColor.MatchString(a.Val) {
				value = a.Val
			}
		case "face":
			if n.Data == "font" && cssFont.MatchString(a.Val) {
				value = a.Val
			}
		case "size":
			if n.Data == "font" && dimension.MatchString(a.Val) {
				value = a.Val
			}
		}
		if value != "" || allowEmpty {
			out = append(out, html.Attribute{Key: a.Key, Val: value})
		}
	}
	return out, true
}

// passiveRole retains only equivalent decorative roles. Requiring every token
// to be supported preserves fallback meaning instead of selecting a new role.
func passiveRole(value string) string {
	roles := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return strings.ContainsRune(" \t\r\n\f", r)
	})
	for _, role := range roles {
		if role != "none" && role != "presentation" {
			return ""
		}
	}
	return strings.Join(roles, " ")
}

// validURL only validates syntax. It performs no DNS lookup, HTTP request or filesystem access.
func validURL(value string, link bool) string {
	value = strings.TrimSpace(value)
	if strings.ContainsAny(value, `\`) || strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return ""
	}
	u, err := url.Parse(value)
	if err != nil || u.User != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Hostname() == "" || u.Opaque != "" {
			return ""
		}
	case "mailto":
		if !link || u.Opaque == "" {
			return ""
		}
	default:
		return ""
	}
	return u.String()
}

func (s *bodySanitizer) imageURL(value string) (string, bool) {
	if s.err != nil {
		return "", false
	}
	value = strings.TrimSpace(value)
	if len(value) >= 4 && strings.EqualFold(value[:4], "cid:") {
		id, err := url.PathUnescape(value[4:])
		if err != nil {
			return "", false
		}
		if cached, ok := s.images[id]; ok {
			return cached, false
		}
		p := s.resources[id]
		s.images[id] = ""
		if p == nil || (p.media != "image/png" && p.media != "image/jpeg" && p.media != "image/gif" && p.media != "image/webp") {
			return "", false
		}
		if p.load != nil {
			if err := p.load(s.ctx); err != nil {
				s.err = err
				return "", false
			}
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(p.data))
		expected := imageMedia[format]
		if err != nil || expected == "" || p.media != expected || config.Width <= 0 || config.Height <= 0 || config.Width > 4096 || config.Height > 4096 || int64(config.Width)*int64(config.Height) > 4<<20 {
			return "", false
		}
		if len(p.data) > MaxHTMLBytes*3/4 {
			s.oversized = true
			return "", false
		}
		if _, _, err := image.Decode(bytes.NewReader(p.data)); err != nil {
			return "", false
		}
		result := "data:" + expected + ";base64," + base64.StdEncoding.EncodeToString(p.data)
		s.images[id] = result
		return result, false
	}
	value = validURL(value, false)
	return value, value != ""
}

// srcset tokenizes candidates without splitting commas inside URLs (WHATWG image candidates).
// Only the standard single width or density descriptor is retained.
func (s *bodySanitizer) srcset(value string) (string, bool) {
	var candidates []string
	retainedBytes := 0
	remote := false
	for len(value) > 0 {
		value = strings.TrimLeft(value, " \t\r\n\f,")
		if value == "" {
			break
		}
		end := strings.IndexAny(value, " \t\r\n\f")
		if end < 0 {
			end = len(value)
		}
		target := value[:end]
		value = value[end:]
		descriptor := ""
		if strings.HasSuffix(target, ",") {
			target = strings.TrimRight(target, ",")
		} else {
			end = strings.IndexByte(value, ',')
			if end < 0 {
				end = len(value)
			}
			descriptor = strings.TrimSpace(value[:end])
			value = value[end:]
		}
		if descriptor != "" {
			if len(descriptor) < 2 {
				continue
			}
			number, err := strconv.ParseFloat(descriptor[:len(descriptor)-1], 64)
			unit := descriptor[len(descriptor)-1]
			if err != nil || math.IsNaN(number) || number <= 0 || number > 100000 || (unit != 'w' && unit != 'x') || (unit == 'w' && strings.ContainsAny(descriptor, ".eE")) {
				continue
			}
		}
		target, isRemote := s.imageURL(target)
		if target == "" {
			continue
		}
		if descriptor != "" {
			target += " " + descriptor
		}
		retainedBytes += len(target)
		if retainedBytes > MaxHTMLBytes {
			s.oversized = true
			return "", false
		}
		candidates = append(candidates, target)
		remote = remote || isRemote
	}
	return strings.Join(candidates, ", "), remote
}

func firstAttribute(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == key {
			return a.Val
		}
	}
	return ""
}

func isCSSStyle(n *html.Node) bool {
	if n.Namespace != "" || n.Data != "style" {
		return false
	}
	mediaType := strings.ToLower(firstAttribute(n, "type"))
	return mediaType == "" || mediaType == "text/css"
}

// Resolve the embedded default set in source order. Meta default-style changes
// the selection; media and CSS filtering preserve that choice.
func (s *bodySanitizer) preferredStyleSet() string {
	preferred := ""
	inert := make([]bool, len(s.elements))
	for i, e := range s.elements {
		n := e.source
		inert[i] = n.Namespace != "" || n.Data == "template" || e.parent >= 0 && inert[e.parent]
		if inert[i] {
			continue
		}
		if n.Data == "meta" && strings.EqualFold(firstAttribute(n, "http-equiv"), "default-style") {
			if title := firstAttribute(n, "content"); title != "" {
				preferred = title
			}
		} else if preferred == "" && isCSSStyle(n) {
			preferred = firstAttribute(n, "title")
		}
	}
	return preferred
}

func (s *bodySanitizer) styleElement(n *html.Node, preferred string) {
	if !isCSSStyle(n) {
		return
	}
	if title := firstAttribute(n, "title"); title != "" && title != preferred {
		return
	}
	media, ok := mediaCondition(firstAttribute(n, "media"), true)
	if !ok {
		return
	}
	var source strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			source.WriteString(c.Data)
		}
	}
	value, remote := s.stylesheet(source.String())
	if value == "" {
		return
	}
	if media != "" {
		value = "@media " + media + "{" + value + "}"
	}
	if s.styles.Len()+len(value) > MaxHTMLBytes {
		s.oversized = true
		return
	}
	s.styles.WriteString(value)
	s.remote = s.remote || remote
}

// Legacy body colors are author presentational hints with zero specificity.
// Keep them ahead of the original stylesheet and inline declarations.
func (s *bodySanitizer) rootPresentation(n *html.Node) {
	if n.Data != "body" {
		return
	}
	var value strings.Builder
	for _, hint := range []struct{ attribute, property string }{{"bgcolor", "background-color"}, {"text", "color"}} {
		if color := legacyColor(firstAttribute(n, hint.attribute)); color != "" {
			value.WriteString(hint.property + ":" + color + ";")
		}
	}
	if value.Len() > 0 {
		s.rootHints.WriteString(s.scope() + " > :where(mailwake-body){" + value.String() + "}")
	}
}

// Convert the existing ASCII color allowlist using HTML's legacy color rules.
// Its 24-character limit bounds each RGB component to eight hexadecimal digits.
func legacyColor(value string) string {
	if !cssColor.MatchString(value) {
		return ""
	}
	value = strings.ToLower(value)
	if value == "transparent" {
		return ""
	}
	_, named := colornames.Map[value]
	// colornames contains CSS3 names; CSS4 adds rebeccapurple.
	if named || value == "rebeccapurple" || len(value) == 4 && value[0] == '#' {
		return value
	}
	digits := []byte(strings.TrimPrefix(value, "#"))
	for i, c := range digits {
		if !strings.ContainsRune("0123456789abcdef", rune(c)) {
			digits[i] = '0'
		}
	}
	for len(digits)%3 != 0 {
		digits = append(digits, '0')
	}
	width := len(digits) / 3
	components := []string{string(digits[:width]), string(digits[width : 2*width]), string(digits[2*width:])}
	for len(components[0]) > 2 && components[0][0] == '0' && components[1][0] == '0' && components[2][0] == '0' {
		for i := range components {
			components[i] = components[i][1:]
		}
	}
	var rgb [3]byte
	for i, component := range components {
		n, _ := strconv.ParseUint(component[:min(2, len(component))], 16, 8)
		rgb[i] = byte(n)
	}
	return "#" + hex.EncodeToString(rgb[:])
}

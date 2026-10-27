package content

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
	"golang.org/x/net/html"
)

func bodyStyle(t *testing.T, source string) string {
	t.Helper()
	var out strings.Builder
	for _, n := range contentElements(t, source, "style") {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			out.WriteString(c.Data)
		}
	}
	return out.String()
}

func TestReadBodyRetainsResponsiveTemplate(t *testing.T) {
	fixture, err := os.ReadFile("testdata/responsive-email.html")
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, multipart("related", "r", htmlMessage(string(fixture)), imagePart("logo", "image/png", raster(t, "png")), imagePart("wide", "image/png", raster(t, "png")), imagePart("narrow", "image/png", raster(t, "png"))))
	if body.Truncated || body.HasRemoteImages || strings.Contains(body.HTML, "cid:") {
		t.Fatal("lost bounded local presentation")
	}
	sheet := bodyStyle(t, body.HTML)
	for _, want := range []string{"@media (max-width:630px)", "width:100%!important", "display:table!important", "overflow:hidden", "padding:28px 18px 0!important"} {
		if !strings.Contains(sheet, want) {
			t.Errorf("missing %q in %s", want, sheet)
		}
	}
	counts := map[string]int{}
	for _, n := range contentElements(t, body.HTML, "div") {
		classes, _ := contentAttribute(n, "class")
		for _, suffix := range []string{"-mobile_hide", "-desktop_hide"} {
			if strings.HasSuffix(classes, suffix) {
				counts[suffix]++
			}
		}
	}
	if counts["-mobile_hide"] != 4 || counts["-desktop_hide"] != 4 {
		t.Fatalf("content switches: %v", counts)
	}
	root := contentElements(t, body.HTML, "mailwake-html")[0]
	if lang, _ := contentAttribute(root, "lang"); lang != "en" {
		t.Fatal("lost root language")
	}
	b := contentElements(t, body.HTML, "mailwake-body")[0]
	if dir, _ := contentAttribute(b, "dir"); dir != "ltr" {
		t.Fatal("lost direction")
	}
}

func TestReadBodyScopesStyleRulesAndPreservesCascade(t *testing.T) {
	body := readBody(t, htmlMessage(`<html id="root" class="theme" lang="ar" dir="rtl" style="font:18px Arial;color:#eee;background:#123"><head><style>
 html{color:red}:root{background-color:#123}html.theme body>table.card td+a{color:blue!important}
 #frame{width:610px}.card{width:500px}.card{width:480px}
 @media screen and (max-width:630px){@media (orientation:portrait){#frame{width:100%!important}}}
 </style></head><body bgcolor="#fff" text="#222" style="text-align:center"><table id="frame" class="card" style="width:610px"><tr><td>Text<a>Link</a></td></tr></table></body></html>`))
	if body.Truncated {
		t.Fatal("unexpected fallback")
	}
	sheet := bodyStyle(t, body.HTML)
	for _, want := range []string{":where(mailwake-html[", "data-mailwake-match", "color:blue!important", "@media screen and (max-width:630px)", "@media (orientation:portrait)", "background-color:#fff;color:#222;"} {
		if !strings.Contains(sheet, want) {
			t.Errorf("missing %q: %s", want, sheet)
		}
	}
	if strings.Index(sheet, "width:500px") > strings.Index(sheet, "width:480px") {
		t.Fatal("changed source order")
	}
	for _, n := range contentElements(t, body.HTML, "table") {
		id, _ := contentAttribute(n, "id")
		class, _ := contentAttribute(n, "class")
		if !strings.HasSuffix(id, "-frame") || !strings.HasSuffix(class, "-card") {
			t.Fatal("lost authored identifiers")
		}
		bindings, _ := contentAttribute(n, "data-mailwake-match")
		if bindings == "" {
			t.Fatal("lost stylesheet targets")
		}
		for _, binding := range strings.Fields(bindings) {
			if !strings.Contains(sheet, binding) {
				t.Fatal("selector and target bindings disagree")
			}
		}
	}
}

func TestReadBodyPreservesImageConditionsAndFallback(t *testing.T) {
	for _, tc := range []struct {
		name, attrs string
		kept        bool
	}{
		{"desktop", `media="(min-width:631px)" type="image/webp" sizes="(max-width:900px) 60vw, 610px"`, true},
		{"empty conditions", `media="" type="" sizes=""`, true},
		{"calc size", `sizes="(max-width:630px) calc(100vw - 36px), 610px"`, true},
		{"unknown media", `media="(device-width:100px)"`, false},
		{"mixed valid and unknown media", `media="(max-width:630px), (hover:hover)"`, false},
		{"negation", `media="not screen"`, false},
		{"invalid feature prefix", `media="(min-max-width:630px)"`, false},
		{"Unicode media separator", "media=\"\u00a0screen\"", false},
		{"Unicode MIME separator", "type=\"\u00a0image/png\"", false},
		{"unknown MIME", `type="image/svg+xml"`, false},
		{"MIME parameters", `type="image/png;bad=1"`, false},
		{"invalid sizes", `sizes="calc(100vw + var(--size))"`, false},
		{"percentage sizes", `sizes="100%"`, false},
		{"invalid size condition", `sizes="(hover:hover) 100vw, 610px"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := readBody(t, multipart("related", "r", htmlMessage(`<picture><source `+tc.attrs+` srcset="https://images.example.test/wide.png 610w, cid:logo 1220w"><img alt="" src="cid:logo" width="40" height="40"></picture>`), imagePart("logo", "image/png", raster(t, "png"))))
			sources := contentElements(t, body.HTML, "source")
			if (len(sources) == 1) != tc.kept || body.HasRemoteImages != tc.kept {
				t.Fatalf("conditional source retained=%t remote=%t", len(sources) == 1, body.HasRemoteImages)
			}
			img := contentElements(t, body.HTML, "img")[0]
			src, _ := contentAttribute(img, "src")
			if !strings.HasPrefix(src, "data:image/png;base64,") {
				t.Fatal("lost CID fallback")
			}
			if alt, ok := contentAttribute(img, "alt"); !ok || alt != "" {
				t.Fatal("lost empty alt")
			}
		})
	}
	body := readBody(t, htmlMessage(`<img sizes="bad" srcset="https://images.example.test/conditional.png 600w" src="https://images.example.test/fallback.png">`))
	if strings.Contains(body.HTML, "srcset=") || strings.Contains(body.HTML, "conditional.png") || !strings.Contains(body.HTML, "fallback.png") {
		t.Fatal("invalid sizes changed candidate meaning")
	}
}

func TestReadBodyStylesKeepResourceAndSecurityBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		remote       bool
		want         string
	}{
		{"scoped background", `<style>.card{background:url(https://images.example.test/bg.png)}</style><div class="card">Safe</div>`, true, "bg.png"},
		{"media background", `<style media="(max-width:630px)">.card{background:url(https://images.example.test/bg.png)}</style><div class="card">Safe</div>`, true, "@media (max-width:630px)"},
		{"unsupported media", `<style media="(hover:hover)">.card{background:url(https://bad.test/x)}</style><p>Safe</p>`, false, "Safe"},
		{"unsupported selector", `<style>a:visited{background:url(https://bad.test/x)}</style><p>Safe</p>`, false, "Safe"},
		{"unsupported container", `<style>@supports(display:grid){p{background:url(https://bad.test/x)}}@font-face{src:url(https://bad.test/font)}@import url(https://bad.test/css);p{color:red}</style><p>Safe</p>`, false, "color:red"},
		{"dangerous values", `<style>p{background:url(javascript:bad);width:expression(bad);color:var(--x);font-family:"</style><script>bad()</script>"}</style><p>Safe</p>`, false, "Safe"},
		{"hidden passive content", `<div style="visibility:hidden;overflow:clip"><span style="visibility:visible">Safe</span></div>`, false, "visibility:hidden;overflow:clip"},
		{"invalid passive values", `<p style="visibility:magic;overflow:bogus;direction:sideways">Safe</p>`, false, "Safe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := readBody(t, htmlMessage(tc.source))
			if body.HasRemoteImages != tc.remote || !strings.Contains(body.HTML, tc.want) {
				t.Fatalf("unexpected output: %s", body.HTML)
			}
			for _, bad := range []string{"bad.test", "<script", "javascript:", "expression(", "var(", "@import", "@font-face", "visibility:magic", "overflow:bogus"} {
				if strings.Contains(body.HTML, bad) {
					t.Errorf("retained %s", bad)
				}
			}
		})
	}
	body := readBody(t, multipart("related", "r", htmlMessage(`<style>.art{background-image:url(cid:logo)}</style><div class="art" aria-hidden="true"></div>`), imagePart("logo", "image/png", raster(t, "png"))))
	if body.HasRemoteImages || !strings.Contains(body.HTML, "data:image/png;base64,") {
		t.Fatal("stylesheet CID failed")
	}
}

func TestReadBodyBoundsCSSComplexityAndKeepsText(t *testing.T) {
	for _, source := range []string{
		`<style>` + strings.Repeat(" ", maxCSSBytes+1) + `</style><p>Fallback</p>`,
		`<style>` + strings.Repeat("@media (max-width:630px){", maxCSSDepth+1) + `p{color:red}` + strings.Repeat("}", maxCSSDepth+1) + `</style><p>Fallback</p>`,
		`<style>` + strings.Repeat("p{color:red}", maxCSSRules+1) + `</style><p>Fallback</p>`,
		`<p style="` + strings.Repeat("color:red;", maxCSSDeclarations+1) + `">Fallback</p>`,
		`<style>p{margin:` + strings.Repeat("0,", maxCSSTokens/2) + `0}</style><p>Fallback</p>`,
		`<style>` + strings.Repeat(".a,.b,.c,.d,.e,.f,.g,.h{color:red}", 500) + `</style><p>Fallback</p>`,
	} {
		body := readBody(t, htmlMessage(source))
		if body.HTML != "" || !body.Truncated || body.HasRemoteImages || body.Text != "Fallback" {
			t.Fatalf("failed CSS budget: html=%d truncated=%t text=%q", len(body.HTML), body.Truncated, body.Text)
		}
	}
}

func TestReadBodyPreservesNormalizedImageSelection(t *testing.T) {
	body := readBody(t, htmlMessage(`<picture><source media="ONLY SCREEN and (min-width: 39em) and (orientation: landscape), (min-resolution:2dppx)" type=" IMAGE/WEBP " sizes="(max-width:630px) calc(100vw - 36px), 610px" srcset="https://images.example.test/small.webp 610w, https://images.example.test/large.webp 1220w"><img src="https://images.example.test/fallback.png" sizes="100vw" srcset="https://images.example.test/small.png 610w"></picture>`))
	source := contentElements(t, body.HTML, "source")[0]
	for attr, want := range map[string]string{
		"media":  "only screen and (min-width:39em) and (orientation:landscape), (min-resolution:2dppx)",
		"type":   "image/webp",
		"sizes":  "(max-width:630px) calc(100vw - 36px), 610px",
		"srcset": "https://images.example.test/small.webp 610w, https://images.example.test/large.webp 1220w",
	} {
		if got, ok := contentAttribute(source, attr); !ok || got != want {
			t.Errorf("%s = %q; want %q", attr, got, want)
		}
	}
	if sizes, _ := contentAttribute(contentElements(t, body.HTML, "img")[0], "sizes"); sizes != "100vw" {
		t.Fatal("lost img size condition")
	}
}

func TestReadBodyRecoversCSSWithinItsOriginalContainer(t *testing.T) {
	body := readBody(t, htmlMessage(`<style>p{broken;color:red} @supports(display:grid){p{background:url(https://bad.test/ignored)}} @media(max-width:630px){p{broken;visibility:hidden ! important</style><p style="broken;overflow:hidden ! important">Safe</p>`))
	sheet := bodyStyle(t, body.HTML)
	if !strings.Contains(sheet, "color:red") || !strings.Contains(sheet, "@media (max-width:630px){") || !strings.Contains(sheet, "visibility:hidden!important;") || body.HasRemoteImages {
		t.Fatalf("lost parsed rule boundaries: %s", sheet)
	}
	style, _ := contentAttribute(contentElements(t, body.HTML, "p")[0], "style")
	if style != "overflow:hidden!important;" {
		t.Fatalf("lost declaration after malformed input: %q", style)
	}
}

func TestReadBodyKeepsRulesAfterMalformedMediaInTheirOriginalScope(t *testing.T) {
	for _, tc := range []struct {
		name, sheet string
		want        []string
	}{
		{"print", `@media print { broken } p{display:none}`, []string{"|display:none"}},
		{"unsupported condition", `@media (hover:hover) { broken } p{display:none}`, []string{"|display:none"}},
		{"nested", `@media screen{@media print{broken}p{visibility:hidden}}p{color:blue}`, []string{"screen|visibility:hidden", "|color:blue"}},
		{"nested unsupported condition", `@media screen{@media (hover:hover){broken}p{visibility:hidden}}p{color:blue}`, []string{"screen|visibility:hidden", "|color:blue"}},
		{"retained sibling", `@media print{p{color:red}broken}p{visibility:hidden}`, []string{"print|color:red", "|visibility:hidden"}},
		{"comment and string braces", `@media print{/* } */p{font-family:"}"}broken}p{visibility:hidden}`, []string{`print|font-family:"}"`, "|visibility:hidden"}},
		{"implicit close", `@media screen{p{broken;visibility:hidden`, []string{"screen|visibility:hidden"}},
		{"nested rule sibling", `p{span{color:red}color:blue}`, []string{"|color:blue"}},
		{"invalid block sibling", `p{color:{broken}visibility:hidden;overflow:clip}`, []string{"|visibility:hidden", "|overflow:clip"}},
		{"custom property block", `p{--palette:{color:red}color:blue;visibility:hidden}`, []string{"|visibility:hidden"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := readBody(t, htmlMessage(`<style>`+tc.sheet+`</style><p>Content</p>`))
			p := css.NewParser(parse.NewInputString(bodyStyle(t, body.HTML)), false)
			var media, got []string
			for {
				grammar, _, name := p.Next()
				if grammar == css.ErrorGrammar {
					if p.HasParseError() {
						t.Fatal("invalid output CSS", p.Err())
					}
					break
				}
				switch grammar {
				case css.BeginAtRuleGrammar:
					media = append(media, strings.TrimSpace(cssText(p.Values())))
				case css.EndAtRuleGrammar:
					media = media[:len(media)-1]
				case css.DeclarationGrammar:
					value := string(name) + ":" + cssText(p.Values())
					if value != "display:block" { // Generated root defaults.
						got = append(got, strings.Join(media, "/")+"|"+value)
					}
				}
			}
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("declarations by media: %q; want %q", got, tc.want)
			}
		})
	}
}

func TestReadBodyDiscardsInlineAtRulesWithTheirContents(t *testing.T) {
	for _, source := range []string{
		`@media print {p{visibility:hidden}}`,
		`@media screen {@media print {broken}p{visibility:hidden}}`,
		`@font-face{font-family:Hidden;visibility:hidden}`,
		`@supports(display:block){p{visibility:hidden}}`,
		`@media print {broken}`,
		`@media print {p{background:url(https://images.example.test/ignored)}}`,
	} {
		t.Run(source, func(t *testing.T) {
			body := readBody(t, htmlMessage(`<p style="color:blue;`+source+`visibility:visible!important;overflow:hidden">Content</p>`))
			style, _ := contentAttribute(contentElements(t, body.HTML, "p")[0], "style")
			if style != "color:blue;visibility:visible!important;overflow:hidden;" || body.HasRemoteImages {
				t.Fatalf("inline containers changed declarations: %q; remote=%t", style, body.HasRemoteImages)
			}
		})
	}
	for _, source := range []string{
		`<p style="@media print{p{background:url(cid:unused)}}">Content</p>`,
		`<style>@media (hover:hover){p{background:url(cid:unused)}broken}p{color:blue}</style><p>Content</p>`,
	} {
		_, remote, truncated, err := sanitizeBody(t.Context(), source, map[string]*bodyPart{"unused": {media: "image/png", load: func(context.Context) error { return errors.New("discarded CSS requested CID") }}})
		if err != nil || remote || truncated {
			t.Fatalf("discarded CSS affected resources: remote=%t truncated=%t err=%v", remote, truncated, err)
		}
	}
}

func TestReadBodyPreservesDeclarationBoundaries(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{`@media print{p{visibility:hidden}}`, ""},
		{`color:blue;@media print{p{visibility:hidden`, "color:blue;"},
		{`font-family:'a;b}';/* } ; */color:blue!important`, `font-family:'a;b}';color:blue!important;`},
		{`background:url(https://images.example.test/a;b.png);visibility:hidden`, `background:url("https://images.example.test/a;b.png");visibility:hidden;`},
		{`color:blue;@import url(https://styles.example.test/a);visibility:hidden`, `color:blue;visibility:hidden;`},
		{`color:blue;@unknown{p{visibility:hidden}}overflow:clip`, `color:blue;overflow:clip;`},
		{`span{visibility:hidden}color:blue;overflow:clip`, `overflow:clip;`},
		{`color:blue;@media print{p{visibility:hidden]}}`, ""},
		{`color:blue;@media print{p{visibility:hidden)}}`, ""},
	} {
		body := readBody(t, htmlMessage(`<p style="`+tc.source+`">Content</p>`))
		style, _ := contentAttribute(contentElements(t, body.HTML, "p")[0], "style")
		if style != tc.want || body.Truncated {
			t.Fatalf("source %q: got %q, want %q (truncated=%t)", tc.source, style, tc.want, body.Truncated)
		}
	}
}

func TestReadBodyKeepsFontInheritanceWhenRootUnitsAreUnsupported(t *testing.T) {
	body := readBody(t, htmlMessage(`<html lang="ar" dir="rtl" style="font-size:20px;background:#123;color:#eee"><body><p style="font-size:18px;font-size:2rem;width:calc(100% - 1rem);line-height:2rlh">Safe</p><p style="font-size:1.5em">Relative</p></body></html>`))
	paragraphs := contentElements(t, body.HTML, "p")
	if style, _ := contentAttribute(paragraphs[0], "style"); style != "font-size:18px;" {
		t.Fatalf("changed root-relative fallback: %s", style)
	}
	if style, _ := contentAttribute(paragraphs[1], "style"); style != "font-size:1.5em;" {
		t.Fatalf("lost inherited font size: %s", style)
	}
}

func TestReadBodyPreservesDirectionAttributeStates(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{"RTL", "rtl"}, {"LTR", "ltr"}, {"Auto", "auto"},
		{" rtl ", ""}, {"\tltr\n", ""}, {" auto ", ""}, {"sideways", ""},
	} {
		for _, tag := range []string{"html", "body", "div"} {
			t.Run(tag+"/"+tc.value, func(t *testing.T) {
				body := readBody(t, htmlMessage(`<`+tag+` dir="`+tc.value+`"><p>Readable</p></`+tag+`>`))
				outputTag := tag
				if tag == "html" || tag == "body" {
					outputTag = "mailwake-" + tag
				}
				value, present := contentAttribute(contentElements(t, body.HTML, outputTag)[0], "dir")
				if value != tc.want || present != (tc.want != "") || body.Truncated {
					t.Fatalf("direction=%q present=%t, want %q", value, present, tc.want)
				}
			})
		}
	}
}

func TestReadBodyPreservesLegacyRootColors(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{"#123", "#123"}, {"#1234", "#123400"}, {"#12345", "#123450"},
		{"#123456", "#123456"}, {"#1234567", "#124570"}, {"#12345678", "#124578"},
		{"#00000001", "#000010"}, {"AliceBlue", "aliceblue"}, {"rebeccapurple", "rebeccapurple"},
		{"currentColor", "#c0e000"}, {"chucknorris", "#c00000"}, {"a", "#0a0000"},
		{"transparent", ""}, {"TRANSPARENT", ""}, {"", ""}, {"url(https://images.example.test/color)", ""},
	} {
		t.Run(tc.value, func(t *testing.T) {
			body := readBody(t, htmlMessage(`<body bgcolor="`+tc.value+`" text="`+tc.value+`"><p>Readable</p></body>`))
			sheet := bodyStyle(t, body.HTML)
			if body.Truncated || body.HasRemoteImages {
				t.Fatal("root colors changed response availability")
			}
			if tc.want == "" {
				if strings.Contains(sheet, "color:") {
					t.Fatal("invalid legacy color changed inheritance")
				}
			} else if !strings.Contains(sheet, "background-color:"+tc.want+";color:"+tc.want+";") {
				t.Fatalf("missing legacy color %q in %s", tc.want, sheet)
			}
		})
	}
	// CSS colors keep their own interpretation and precedence over HTML hints.
	body := readBody(t, htmlMessage(`<body bgcolor="#1234" text="transparent" style="color:transparent;background:#1234"><p>Readable</p></body>`))
	style, _ := contentAttribute(contentElements(t, body.HTML, "mailwake-body")[0], "style")
	if style != "color:transparent;background:#1234;" {
		t.Fatalf("changed authored CSS colors: %s", style)
	}
}

func TestReadBodyRejectsUnsupportedSelectorsAsWholeRules(t *testing.T) {
	for _, selector := range []string{"p,a:visited", "[dir=rtl] p", `p.\78`, strings.Repeat("p ", 17), strings.Repeat("p,", 32) + "p"} {
		body := readBody(t, htmlMessage(`<style>`+selector+`{background:url(https://bad.test/ignored)}p{color:red}</style><p>Safe</p>`))
		if body.HasRemoteImages || strings.Contains(body.HTML, "bad.test") || !strings.Contains(bodyStyle(t, body.HTML), "color:red") {
			t.Fatalf("selector changed rule meaning: %q", selector)
		}
	}
}

func TestStylesheetCIDReadFailureReachesCaller(t *testing.T) {
	failure := errors.New("synthetic CID read failure")
	for _, source := range []string{`<style>p{background:url(cid:logo)}</style><p>Safe</p>`, `<p style="background:url(cid:logo)">Safe</p>`} {
		_, _, _, err := sanitizeBody(t.Context(), source, map[string]*bodyPart{"logo": {media: "image/png", load: func(context.Context) error { return failure }}})
		if !errors.Is(err, failure) {
			t.Fatalf("lost CID error: %v", err)
		}
	}
}

func TestReadBodyReparsedStyleCannotEscapeMessage(t *testing.T) {
	body := readBody(t, htmlMessage(`<style>.x{font-family:"&lt;/style&gt;&lt;img src=https://bad.test&gt;";background:url("https://images.example.test/a?x=%3C/style%3E")}</style><p class="x">Safe</p>`))
	parsed, err := html.Parse(strings.NewReader(body.HTML))
	if err != nil {
		t.Fatal(err)
	}
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "img") {
			t.Fatal("raw-text breakout")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(parsed)
}

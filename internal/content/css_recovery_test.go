package content

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
)

func TestReadBodyClosesEOFValuesBeforeCombiningRules(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"color", `color:rgb(255,0,0`, `color:rgb(255,0,0);`},
		{"gradient", `background-image:linear-gradient(red,blue`, `background-image:linear-gradient(red,blue);`},
		{"calculation", `width:calc(100% - 20px`, `width:calc(100% - 20px);`},
		{"nested functions", `width:calc(100% - min(20px,30px`, `width:calc(100% - min(20px,30px));`},
		{"string", `font-family:'Synthetic Font`, `font-family:'Synthetic Font';`},
		{"empty string", `font-family:'`, `font-family:'';`},
		{"comment", `color:red/*unfinished`, `color:red;`},
		{"important color", `color:rgb(255,0,0!important`, `color:rgb(255,0,0)!important;`},
		{"important gradient", `background-image:linear-gradient(red,blue ! important`, `background-image:linear-gradient(red,blue)!important;`},
		{"important calculation", `width:calc(100% - 20px!important`, `width:calc(100% - 20px)!important;`},
		{"important nested functions", `width:calc(100% - min(20px,30px !IMPORTANT`, `width:calc(100% - min(20px,30px))!important;`},
		{"important comments", `color:rgb(255,0,0!/**/important/*unfinished`, `color:rgb(255,0,0)!important;`},
		{"important keyword", `visibility:hidden ! important`, `visibility:hidden!important;`},
		{"complete important function", `color:rgb(255,0,0)!important`, `color:rgb(255,0,0)!important;`},
		{"priority inside closed function", `color:rgb(255,0,0!important)`, `color:rgb(255,0,0!important);`},
		{"priority inside string", `font-family:'Font!important`, `font-family:'Font!important';`},
		{"invalid priority", `color:red!important!important`, `color:red!important!important;`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inline := readBody(t, htmlMessage(`<p style="`+tc.input+`">Text</p>`))
			style, _ := contentAttribute(contentElements(t, inline.HTML, "p")[0], "style")
			if style != tc.want {
				t.Fatalf("inline value: got %q, want %q", style, tc.want)
			}
			body := readBody(t, htmlMessage(`<style>p{`+tc.input+`</style><style>p{background-color:yellow}</style><p>Text</p>`))
			p := css.NewParser(parse.NewInputString(bodyStyle(t, body.HTML)), false)
			var declarations []string
			for {
				grammar, _, name := p.Next()
				if grammar == css.ErrorGrammar {
					if p.HasParseError() {
						t.Fatal("invalid CSS output", p.Err())
					}
					break
				}
				if grammar == css.DeclarationGrammar {
					declarations = append(declarations, string(name)+":"+cssText(p.Values())+";")
				}
			}
			if !slices.Equal(declarations, []string{"display:block;", tc.want, "background-color:yellow;"}) {
				t.Fatalf("independent declarations: %v", declarations)
			}
		})
	}
}

var selectorTopologyCases = []struct {
	name, source string
	want         []string
}{
	{"relocated body style", `<div>Heading</div><style>div+p{color:red}</style><p title="body">Visible</p>`, nil},
	{"relocated nested style", `<div><span>Heading</span><style>span+p{color:red}</style><p title="body">Visible</p></div>`, nil},
	{"unwrapped ancestor", `<style>div>p{color:red}</style><div><article><p title="body">Visible</p></article></div>`, nil},
	{"removed sibling", `<style>div+p{color:red}</style><div></div><input><p title="body">Visible</p>`, nil},
	{"removed conditional source", `<style>div+img{color:red}</style><picture><div></div><source type="image/svg+xml"><img title="image" alt=""></picture>`, nil},
	{"actual adjacent sibling", `<style>div+p{color:red}</style><div></div>text<!--gap--><p title="body">Red</p>`, []string{"body"}},
	{"general sibling across removed node", `<style>div~p{color:red}</style><div></div><input><p title="body">Red</p>`, []string{"body"}},
	{"universal removed sibling", `<body><style>*+p{color:red}</style><p title="body">Red</p></body>`, []string{"body"}},
	{"descendant across unwrapped node", `<style>div p{color:red}</style><div><article><p title="body">Red</p></article></div>`, []string{"body"}},
	{"unwrapped class ancestor", `<style>.wrapper>p{color:red}</style><article class="wrapper"><p title="body">Red</p></article>`, []string{"body"}},
	{"picture boundary ancestor", `<style>.wrapper>img{width:40px!important}</style><picture><source srcset="https://images.example.test/wide.png"><article class="wrapper"><img title="image" alt=""></article></picture>`, []string{"image"}},
	{"nested descendant alternatives", `<style>.outer .middle>p{color:red}</style><div class="outer"><div class="middle"><div class="middle"><p title="body">Red</p></div></div></div>`, []string{"body"}},
	{"mixed sibling chain", `<style>div~section+p{color:red}</style><div></div><section></section><span></span><section></section><p title="body">Red</p>`, []string{"body"}},
	{"root and body", `<html class="theme" title="root"><style>:ROOT.theme,html>body{color:red}</style><body title="body"><p>Red by inheritance</p></body></html>`, []string{"root", "body"}},
	{"same ID compound", `<style>#target#target.a.a{color:red}</style><p id="target" class="a" title="body">Red</p>`, []string{"body"}},
	{"conflicting ID compound", `<style>#one#two{color:red}</style><p id="one" title="body">Visible</p>`, nil},
	{"media target", `<style>@media(max-width:630px){div+p{color:red}}</style><div></div><p title="body">Narrow red</p>`, []string{"body"}},
}

// Read the generated selector/element binding, the public output consumed by
// the renderer. Offline WebKit acceptance also verifies the applied cascade.
func TestReadBodyKeepsOriginalSelectorTargets(t *testing.T) {
	markerPattern := regexp.MustCompile(`data-mailwake-match~="([^"]+)"`)
	for _, tc := range selectorTopologyCases {
		t.Run(tc.name, func(t *testing.T) {
			body := readBody(t, htmlMessage(tc.source))
			if body.Truncated {
				t.Fatal("unexpected fallback")
			}
			markers := map[string]bool{}
			for _, match := range markerPattern.FindAllStringSubmatch(bodyStyle(t, body.HTML), -1) {
				markers[match[1]] = true
			}
			var got []string
			for _, tag := range []string{"mailwake-html", "mailwake-body", "p", "img"} {
				for _, n := range contentElements(t, body.HTML, tag) {
					bound, _ := contentAttribute(n, "data-mailwake-match")
					for _, marker := range strings.Fields(bound) {
						if markers[marker] {
							title, _ := contentAttribute(n, "title")
							got = append(got, title)
							break
						}
					}
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("styled elements: got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReadBodyBoundsSelectorMatchingAndOutput(t *testing.T) {
	var sheet strings.Builder
	for i := 0; i < 200; i++ {
		sheet.WriteString(".c" + strconv.Itoa(i) + " p{color:red}")
	}
	for _, source := range []string{
		`<style>` + sheet.String() + `</style>` + strings.Repeat(`<p></p>`, 6000) + `<p>Fallback</p>`,
		`<style>*{color:red}</style>` + strings.Repeat(`<span></span>`, 9000) + `<p>Fallback</p>`,
	} {
		body := readBody(t, htmlMessage(source))
		if body.HTML != "" || !body.Truncated || body.HasRemoteImages || body.Text != "Fallback" {
			t.Fatalf("selector budget: html=%d truncated=%t text=%q", len(body.HTML), body.Truncated, body.Text)
		}
	}
}

func TestReadBodyOwnsSelectorBindings(t *testing.T) {
	body := readBody(t, htmlMessage(`<style>p{color:red}</style><div data-mailwake-match="forged">Safe</div><p data-mailwake-match="forged">Red</p>`))
	if strings.Contains(body.HTML, "forged") {
		t.Fatal("retained authored selector bindings")
	}
	if value, _ := contentAttribute(contentElements(t, body.HTML, "div")[0], "data-mailwake-match"); value != "" {
		t.Fatal("bound an unrelated element")
	}
}

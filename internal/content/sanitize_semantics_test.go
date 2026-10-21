package content

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func contentElements(t *testing.T, source, tag string) []*html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	var result []*html.Node
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == tag {
			result = append(result, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)
	return result
}

func contentAttribute(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func TestReadBodyPreservesDecorativeEmailStructure(t *testing.T) {
	// Independently reconstructed structure. All text and URLs are synthetic;
	// the source email, headers and login links stay outside this fixture.
	raw, err := os.ReadFile("testdata/decorative-email.html")
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, htmlMessage(string(raw)))
	if !body.HasRemoteImages || body.Truncated {
		t.Fatal("lost retained remote images")
	}
	images := contentElements(t, body.HTML, "img")
	if len(images) != 3 {
		t.Fatalf("image count %d", len(images))
	}
	for i, want := range []struct {
		value   string
		present bool
	}{{"Claude", true}, {"", true}, {"", false}} {
		value, present := contentAttribute(images[i], "alt")
		if value != want.value || present != want.present {
			t.Fatalf("image %d alt=%q present=%v", i, value, present)
		}
	}
	tables := contentElements(t, body.HTML, "table")
	for _, n := range tables {
		if value, _ := contentAttribute(n, "role"); value != "presentation" {
			t.Fatal("lost layout-table role")
		}
	}
	decoration := contentElements(t, body.HTML, "div")[0]
	if value, _ := contentAttribute(decoration, "aria-hidden"); value != "true" {
		t.Fatal("lost decorative ancestor")
	}
	style, _ := contentAttribute(decoration, "style")
	for _, want := range []string{"wave.gif", "wave.png", "width:100px", "height:100px"} {
		if !strings.Contains(style, want) {
			t.Fatalf("lost %s", want)
		}
	}
	if strings.Index(style, "wave.gif") > strings.Index(style, "wave.png") {
		t.Fatal("background layers reordered")
	}
	visible := readBody(t, htmlMessage(strings.Replace(string(raw), `aria-hidden="true"`, "", 1)))
	if strings.Replace(body.HTML, ` aria-hidden="true"`, "", 1) != visible.HTML {
		t.Fatal("ARIA changed visual markup")
	}
}

func TestReadBodyValidatesPassiveSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, input, key, want string
		present                bool
	}{
		{"ARIA true", `aria-hidden="true"`, "aria-hidden", "true", true},
		{"ARIA false", `aria-hidden="false"`, "aria-hidden", "false", true},
		{"ARIA default", `aria-hidden="undefined"`, "aria-hidden", "undefined", true},
		{"ARIA normalized", `ARIA-HIDDEN=" TRUE "`, "aria-hidden", "true", true},
		{"ARIA empty", `aria-hidden`, "aria-hidden", "", false},
		{"ARIA invalid", `aria-hidden="yes"`, "aria-hidden", "", false},
		{"ARIA control", `aria-hidden="tr&#10;ue"`, "aria-hidden", "", false},
		{"ARIA namespace lookalike", `x:aria-hidden="true"`, "aria-hidden", "", false},
		{"ARIA duplicate first wins", `aria-hidden="false" aria-hidden="true"`, "aria-hidden", "false", true},
		{"ARIA invalid first", `aria-hidden="yes" aria-hidden="true"`, "aria-hidden", "", false},
		{"hidden bare", `hidden`, "hidden", "", true},
		{"hidden empty", `hidden=""`, "hidden", "", true},
		{"hidden keyword", `hidden="HIDDEN"`, "hidden", "", true},
		{"hidden until found", `hidden="until-found"`, "hidden", "until-found", true},
		{"hidden mixed case", `hidden="UnTiL-FoUnD"`, "hidden", "until-found", true},
		{"hidden false remains hidden", `hidden="false"`, "hidden", "", true},
		{"hidden invalid default", `hidden="invalid"`, "hidden", "", true},
		{"hidden padded keyword uses default", `hidden=" until-found "`, "hidden", "", true},
		{"role none", `role="none"`, "role", "none", true},
		{"role presentation", `role="presentation"`, "role", "presentation", true},
		{"role aliases", `role=" NONE&#9;presentation "`, "role", "none presentation", true},
		{"role unknown", `role="mystery"`, "role", "", false},
		{"role interactive", `role="button"`, "role", "", false},
		{"role mixed unsupported", `role="button presentation"`, "role", "", false},
		{"role empty", `role=""`, "role", "", false},
		{"role Unicode separator", `role="none&#160;presentation"`, "role", "", false},
		{"alt belongs to images", `alt=""`, "alt", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := readBody(t, htmlMessage(`<div `+tc.input+`><img src="https://images.example.test/a.png"></div>`))
			nodes := contentElements(t, body.HTML, "div")
			if len(nodes) != 1 {
				t.Fatal("lost container")
			}
			value, present := contentAttribute(nodes[0], tc.key)
			if value != tc.want || present != tc.present {
				t.Fatalf("%s=%q present=%v", tc.key, value, present)
			}
			if len(contentElements(t, body.HTML, "img")) != 1 || !body.HasRemoteImages {
				t.Fatal("metadata removed image")
			}
		})
	}
}

func TestReadBodyDistinguishesEmptyAndMissingImageDescriptions(t *testing.T) {
	for _, tc := range []struct {
		name, attributes, want string
		present                bool
	}{
		{"missing", "", "", false},
		{"explicit empty", `alt=""`, "", true},
		{"bare attribute", `alt`, "", true},
		{"whitespace preserved", `alt=" "`, " ", true},
		{"description", `alt="Claude"`, "Claude", true},
		{"first empty wins", `alt="" alt="description"`, "", true},
		{"first description wins", `alt="description" alt=""`, "description", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := readBody(t, htmlMessage(`<img `+tc.attributes+` src="https://images.example.test/a.png">`))
			image := contentElements(t, body.HTML, "img")[0]
			value, present := contentAttribute(image, "alt")
			if value != tc.want || present != tc.present {
				t.Fatalf("alt=%q present=%v", value, present)
			}
		})
	}
}

func TestReadBodyRetainsAncestorSemanticsWithoutFlattening(t *testing.T) {
	body := readBody(t, htmlMessage(`<section aria-hidden="true"><div aria-hidden="false"><img alt="described" src="https://images.example.test/a.png"></div></section><div hidden><span aria-hidden="false">Retained hidden subtree</span></div><table role="presentation"><tr><td><img alt="Claude"></td></tr></table>`))
	section := contentElements(t, body.HTML, "section")[0]
	if v, _ := contentAttribute(section, "aria-hidden"); v != "true" {
		t.Fatal("lost ancestor")
	}
	div := section.FirstChild
	if div == nil || div.Data != "div" {
		t.Fatal("flattened subtree")
	}
	if v, _ := contentAttribute(div, "aria-hidden"); v != "false" {
		t.Fatal("rewrote descendant declaration")
	}
	hidden := contentElements(t, body.HTML, "div")[1]
	if _, ok := contentAttribute(hidden, "hidden"); !ok {
		t.Fatal("lost hidden ancestor")
	}
	images := contentElements(t, body.HTML, "img")
	if _, ok := contentAttribute(images[1], "role"); ok {
		t.Fatal("spread presentation role to descendant image")
	}
	if !strings.Contains(body.HTML, "Retained hidden subtree") {
		t.Fatal("removed hidden content")
	}
}

func TestReadBodyPassiveSemanticsKeepSecurityBoundaries(t *testing.T) {
	body := readBody(t, htmlMessage(`<div aria-hidden="true" hidden="onclick=bad()" role="presentation" onclick="bad()" style="background:url(javascript:bad);width:expression(bad)"><img alt="" src="javascript:bad" onerror="bad()"><a href="javascript:bad">text</a><script>active()</script><svg aria-hidden="true"><image href="https://images.example.test/bad.png"/></svg></div>`))
	for _, bad := range []string{"onclick", "onerror", "javascript", "expression", "<script", "<svg", "active()", "bad.png"} {
		if strings.Contains(body.HTML, bad) {
			t.Fatalf("retained %s", bad)
		}
	}
	if body.HasRemoteImages {
		t.Fatal("discarded source marked remote")
	}
	images := contentElements(t, body.HTML, "img")
	if v, ok := contentAttribute(images[0], "alt"); v != "" || !ok {
		t.Fatal("lost explicit empty alt with removed source")
	}
}

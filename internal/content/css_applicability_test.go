package content

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestReadBodyKeepsDefaultEmbeddedStyleSet(t *testing.T) {
	for _, tc := range []struct {
		name, sheets string
		want, absent []string
	}{
		{"different titles", `<style title="Default">p{display:block}</style><style title="Alternate">p{display:none}</style>`, []string{"display:block"}, []string{"display:none"}},
		{"same title and persistent sheets", `<style title="Theme">p{color:red}</style><style>p{background:yellow}</style><style title="Theme">p{width:40px}</style>`, []string{"color:red", "background:yellow", "width:40px"}, nil},
		{"empty title", `<style title="">p{color:red}</style><style title="Theme">p{width:40px}</style>`, []string{"color:red", "width:40px"}, nil},
		{"exact title", `<style title=" Theme ">p{color:red}</style><style title="Theme">p{display:none}</style><style title=" theme ">p{width:40px}</style>`, []string{"color:red"}, []string{"display:none", "width:40px"}},
		{"non CSS type", `<style type="text/plain" title="Other">p{display:none}</style><style title="Theme">p{color:red}</style>`, []string{"color:red"}, []string{"display:none"}},
		{"inert template", `<template><style title="Other">p{display:none}</style></template><style title="Theme">p{color:red}</style>`, []string{"color:red"}, []string{"display:none"}},
		{"print set", `<style title="Print" media="print">p{color:red}</style><style title="Screen">p{display:none}</style>`, []string{"@media print", "color:red"}, []string{"display:none"}},
		{"unsupported media", `<style title="First" media="(hover:hover)">p{color:red}</style><style title="Second">p{display:none}</style><style>p{width:40px}</style>`, []string{"width:40px"}, []string{"color:red", "display:none"}},
		{"empty preferred sheet", `<style title="First"></style><style title="Second">p{display:none}</style>`, nil, []string{"display:none"}},
		{"discarded container", `<form><style title="First">p{color:red}</style></form><style title="Second">p{display:none}</style>`, nil, []string{"color:red", "display:none"}},
		{"declared default set", `<meta http-equiv="default-style" content="Second"><style title="First">p{display:none}</style><style title="Second">p{color:red}</style>`, []string{"color:red"}, []string{"display:none"}},
		{"later default selection", `<style title="First">p{display:none}</style><style title="Second">p{color:red}</style><meta http-equiv="DEFAULT-STYLE" content="Second">`, []string{"color:red"}, []string{"display:none"}},
		{"empty default selection", `<style title="First">p{color:red}</style><meta http-equiv="default-style" content=""><style title="Second">p{display:none}</style>`, []string{"color:red"}, []string{"display:none"}},
		{"inert default selection", `<style title="First">p{color:red}</style><template><meta http-equiv="default-style" content="Second"></template><style title="Second">p{display:none}</style>`, []string{"color:red"}, []string{"display:none"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := readBody(t, htmlMessage(tc.sheets+`<p>Readable</p>`))
			if body.Truncated {
				t.Fatal("unexpected HTML fallback")
			}
			if len(contentElements(t, body.HTML, "meta")) != 0 {
				t.Fatal("retained document metadata")
			}
			sheet := bodyStyle(t, body.HTML)
			for _, value := range tc.want {
				if !strings.Contains(sheet, value) {
					t.Errorf("missing active declaration %q", value)
				}
			}
			for _, value := range tc.absent {
				if strings.Contains(sheet, value) {
					t.Errorf("activated excluded declaration %q", value)
				}
			}
		})
	}
}

func TestReadBodyDiscardedStyleSetsResolveNoResources(t *testing.T) {
	body := readBody(t, htmlMessage(`<style title="Default">p{color:red}</style><style title="Alternate">p{background:url(https://images.example.test/unused.png),url(cid:missing)}</style><p>Readable</p>`))
	if body.Truncated || body.HasRemoteImages || strings.Contains(body.HTML, "unused.png") || strings.Contains(body.HTML, "background:") {
		t.Fatal("inactive style set retained resources")
	}
	_, remote, truncated, err := sanitizeBody(t.Context(), `<style title="Default"></style><style title="Alternate">p{background:url(cid:unused)}</style><p>Readable</p>`, map[string]*bodyPart{
		"unused": {media: "image/png", load: func(context.Context) error { return errors.New("synthetic unavailable resource") }},
	})
	if err != nil || remote || truncated {
		t.Fatal("inactive resource changed the content response", err)
	}
}

func TestReadBodyPreservesStyleTypeEligibility(t *testing.T) {
	for _, tc := range []struct {
		value  string
		active bool
	}{
		{"", true}, {"text/css", true}, {"TEXT/CSS", true},
		{" text/css", false}, {"text/css ", false}, {"\ttext/css\n", false},
		{" ", false}, {"text/css; charset=utf-8", false},
	} {
		t.Run(tc.value, func(t *testing.T) {
			for _, titled := range []bool{false, true} {
				title := ""
				if titled {
					title = ` title="First"`
				}
				body := readBody(t, htmlMessage(`<style type="`+tc.value+`"`+title+`>p{display:none;background:url(https://images.example.test/style.png)}</style><style title="Second">p{color:blue}</style><p>Readable</p>`))
				sheet := bodyStyle(t, body.HTML)
				if body.Truncated || body.HasRemoteImages != tc.active || strings.Contains(sheet, "display:none") != tc.active {
					t.Fatal("changed stylesheet eligibility")
				}
				if strings.Contains(sheet, "color:blue") != (!titled || !tc.active) {
					t.Fatal("changed preferred stylesheet set")
				}
			}
		})
	}
}

func TestReadBodyPreservesPictureCandidateRelationships(t *testing.T) {
	for _, tc := range []struct {
		name, markup string
		sources      int
		pictureImage bool
	}{
		{"direct children", `<source srcset="https://images.example.test/wide.png"><img src="cid:logo">`, 1, true},
		{"nested source", `<article><source srcset="https://images.example.test/wide.png"></article><img src="cid:logo">`, 0, true},
		{"nested fallback", `<source srcset="https://images.example.test/wide.png"><article><img src="cid:logo"></article>`, 1, false},
		{"nested wrapper chain", `<source srcset="https://images.example.test/wide.png"><article><header><img src="cid:logo"></header></article>`, 1, false},
		{"retained wrapper", `<source srcset="https://images.example.test/wide.png"><span><img src="cid:logo"></span>`, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := readBody(t, multipart("related", "r", htmlMessage(`<picture>`+tc.markup+`</picture>`), imagePart("logo", "image/png", raster(t, "png"))))
			if body.Truncated || len(contentElements(t, body.HTML, "source")) != tc.sources || body.HasRemoteImages != (tc.sources > 0) {
				t.Fatal("changed source eligibility or resource flag")
			}
			img := contentElements(t, body.HTML, "img")[0]
			if (img.Parent.Data == "picture") != tc.pictureImage {
				t.Fatal("changed fallback image's source-set association")
			}
			if src, _ := contentAttribute(img, "src"); !strings.HasPrefix(src, "data:image/png;base64,") {
				t.Fatal("lost local fallback image")
			}
		})
	}
}

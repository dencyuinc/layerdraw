// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package materialize

import (
	"errors"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
)

func TestAssetContentValidation(t *testing.T) {
	validSVG := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><defs><linearGradient id="g"/></defs><rect fill="url(#g)"/></svg>`)
	for _, test := range []struct {
		name      string
		mediaType string
		data      []byte
		unsafe    bool
	}{
		{name: "PNG", mediaType: "image/png", data: encodedPNG(t)},
		{name: "SVG", mediaType: "image/svg+xml", data: validSVG},
		{name: "wrong signature", mediaType: "image/jpeg", data: encodedPNG(t)},
		{name: "truncated image", mediaType: "image/png", data: []byte("\x89PNG\r\n\x1a\n")},
		{name: "missing SVG root", mediaType: "image/svg+xml", data: nil},
		{name: "wrong SVG root", mediaType: "image/svg+xml", data: []byte(`<html/>`)},
		{name: "malformed SVG", mediaType: "image/svg+xml", data: []byte(`<svg>`)},
		{name: "script", mediaType: "image/svg+xml", data: []byte(`<svg><script/></svg>`), unsafe: true},
		{name: "event", mediaType: "image/svg+xml", data: []byte(`<svg onload="run()"/>`), unsafe: true},
		{name: "external href", mediaType: "image/svg+xml", data: []byte(`<svg><image href="https://example.invalid/a.png"/></svg>`), unsafe: true},
		{name: "external CSS", mediaType: "image/svg+xml", data: []byte(`<svg><rect style="fill:url(https://example.invalid/a.svg)"/></svg>`), unsafe: true},
		{name: "external style element", mediaType: "image/svg+xml", data: []byte(`<svg><style>rect { fill: url('https://example.invalid/a.svg') }</style></svg>`), unsafe: true},
		{name: "mixed CSS references", mediaType: "image/svg+xml", data: []byte(`<svg><rect style="fill:url(#local);stroke:url(https://example.invalid/a.svg)"/></svg>`), unsafe: true},
		{name: "CSS comment hides URL", mediaType: "image/svg+xml", data: []byte(`<svg><rect style="fill:u/**/rl(https://example.invalid/a.svg)"/></svg>`), unsafe: true},
		{name: "CSS escape hides URL", mediaType: "image/svg+xml", data: []byte(`<svg><rect style="fill:u\72 l(https://example.invalid/a.svg)"/></svg>`), unsafe: true},
		{name: "CSS escape hides import", mediaType: "image/svg+xml", data: []byte(`<svg><style>@\69 mport 'https://example.invalid/a.css';</style></svg>`), unsafe: true},
		{name: "CSS comment hides import", mediaType: "image/svg+xml", data: []byte(`<svg><style>@im/**/port 'https://example.invalid/a.css';</style></svg>`), unsafe: true},
		{name: "XML comment splits CSS", mediaType: "image/svg+xml", data: []byte(`<svg><style>u<!-- hidden -->rl(https://example.invalid/a.svg)</style></svg>`), unsafe: true},
		{name: "XML base", mediaType: "image/svg+xml", data: []byte(`<svg xml:base="https://example.invalid/"><use href="#local"/></svg>`), unsafe: true},
		{name: "processing instruction", mediaType: "image/svg+xml", data: []byte(`<?xml-stylesheet href="https://example.invalid/a.css"?><svg/>`), unsafe: true},
		{name: "directive", mediaType: "image/svg+xml", data: []byte(`<!DOCTYPE svg><svg/>`), unsafe: true},
		{name: "multiple roots", mediaType: "image/svg+xml", data: []byte(`<svg/><svg/>`)},
		{name: "text before root", mediaType: "image/svg+xml", data: []byte(`hidden<svg/>`)},
		{name: "text after root", mediaType: "image/svg+xml", data: []byte(`<svg/>hidden`)},
		{name: "unsupported", mediaType: "application/octet-stream", data: []byte("asset")},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateAssetContent(test.mediaType, test.data)
			if test.name == "PNG" || test.name == "SVG" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || errors.Is(err, errUnsafeAsset) != test.unsafe {
				t.Fatalf("error=%v unsafe=%v", err, errors.Is(err, errUnsafeAsset))
			}
		})
	}
}

func TestUnsafeAssetFailsTransactionallyWithLDL1901(t *testing.T) {
	input := projectStages(t, assetFixture)
	asset := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><foreignObject/></svg>`)
	input.Resolved.Assets = []ResolvedAsset{{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, Locator: "icon.png", Bytes: asset, ExpectedDigest: rawDigest(asset), ExpectedMediaType: "image/svg+xml", ExpectedByteLength: int64(len(asset))}}
	result := Compile(input)
	if !result.HasErrors || result.Snapshot().Document != nil || !diagnosticCode(result.Diagnostics, "LDL1901") {
		t.Fatalf("unsafe asset result=%+v diagnostics=%+v", result.Snapshot(), result.Diagnostics)
	}
}

func TestCSSSecurityTokenNormalizationEdgeCases(t *testing.T) {
	for _, value := range []string{"/*", `url(https://example.invalid`, "url ('https://example.invalid/a')", `u\72l(https://example.invalid)`, `url(\68 ttps://example.invalid)`} {
		if !unsafeSVGStyle(value) {
			t.Fatalf("unsafe CSS accepted: %q", value)
		}
	}
	for _, value := range []string{`url(#local)`, `url( '#local' )`, "u\\\nrl(#local)", "u\\\r\nrl(#local)", "u\\\frl(#local)"} {
		if unsafeSVGStyle(value) {
			t.Fatalf("local CSS reference rejected: %q", value)
		}
	}
	for _, value := range []string{`trailing\`, `/* unterminated`} {
		if _, err := normalizeCSSSecurityTokens(value); err == nil {
			t.Fatalf("malformed CSS token sequence accepted: %q", value)
		}
	}
	normalized, err := normalizeCSSSecurityTokens(`\40 media { color: red } \0`)
	if err != nil || !strings.Contains(normalized, "@media") || !strings.ContainsRune(normalized, '\ufffd') {
		t.Fatalf("CSS escapes normalized to %q, err=%v", normalized, err)
	}
}

func diagnosticCode(diagnostics []resolve.Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code && strings.Contains(diagnostic.Message, "unsafe asset content") {
			return true
		}
	}
	return false
}

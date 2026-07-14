// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package materialize

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strconv"
	"strings"

	_ "golang.org/x/image/webp"
)

var errUnsafeAsset = errors.New("unsafe asset content")

var decodedImageFormats = map[string]string{
	"image/jpeg": "jpeg",
	"image/png":  "png",
	"image/webp": "webp",
}

var forbiddenSVGElements = map[string]bool{
	"animate":          true,
	"animatecolor":     true,
	"animatemotion":    true,
	"animatetransform": true,
	"discard":          true,
	"foreignobject":    true,
	"script":           true,
	"set":              true,
}

func validateAssetContent(mediaType string, data []byte) error {
	if format, ok := decodedImageFormats[mediaType]; ok {
		_, decodedFormat, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("decode %s asset: %w", format, err)
		}
		if decodedFormat != format {
			return fmt.Errorf("asset signature is %s, expected %s", decodedFormat, format)
		}
		return nil
	}
	if mediaType == "image/svg+xml" {
		return validateSVG(data)
	}
	return fmt.Errorf("unsupported asset media type %q", mediaType)
}

func validateSVG(data []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	seenRoot := false
	rootClosed := false
	depth := 0
	styleDepth := 0
	var styleText strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if !seenRoot {
				return errors.New("SVG has no root element")
			}
			if !rootClosed || depth != 0 {
				return errors.New("SVG root element is incomplete")
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("decode SVG: %w", err)
		}
		switch value := token.(type) {
		case xml.Directive:
			return fmt.Errorf("%w: XML directives are forbidden", errUnsafeAsset)
		case xml.ProcInst:
			if !strings.EqualFold(value.Target, "xml") {
				return fmt.Errorf("%w: XML processing instructions are forbidden", errUnsafeAsset)
			}
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(value)) != "" {
				return errors.New("SVG contains non-whitespace outside its root element")
			}
			if styleDepth > 0 {
				styleText.Write(value)
			}
		case xml.Comment:
			if styleDepth > 0 {
				return fmt.Errorf("%w: comments inside SVG style elements are forbidden", errUnsafeAsset)
			}
		case xml.StartElement:
			name := strings.ToLower(value.Name.Local)
			if depth == 0 {
				if seenRoot || rootClosed {
					return errors.New("SVG contains multiple root elements")
				}
				if name != "svg" {
					return errors.New("SVG root element is not svg")
				}
				seenRoot = true
			}
			depth++
			if forbiddenSVGElements[name] {
				return fmt.Errorf("%w: SVG element %s is forbidden", errUnsafeAsset, value.Name.Local)
			}
			if name == "style" {
				if styleDepth != 0 {
					return fmt.Errorf("%w: nested SVG style elements are forbidden", errUnsafeAsset)
				}
				styleText.Reset()
				styleDepth++
			}
			for _, attribute := range value.Attr {
				if unsafeSVGAttribute(attribute) {
					return fmt.Errorf("%w: SVG attribute %s is forbidden", errUnsafeAsset, attribute.Name.Local)
				}
			}
		case xml.EndElement:
			if strings.EqualFold(value.Name.Local, "style") {
				if unsafeSVGStyle(styleText.String()) {
					return fmt.Errorf("%w: external SVG style reference is forbidden", errUnsafeAsset)
				}
				styleDepth--
			}
			depth--
			if depth == 0 {
				rootClosed = true
			}
		}
	}
}

func unsafeSVGAttribute(attribute xml.Attr) bool {
	name := strings.ToLower(attribute.Name.Local)
	value := strings.TrimSpace(attribute.Value)
	lowerValue := strings.ToLower(value)
	if strings.HasPrefix(name, "on") {
		return true
	}
	if name == "base" {
		return value != ""
	}
	if name == "href" || name == "src" {
		return value != "" && !strings.HasPrefix(value, "#")
	}
	if name == "style" {
		return unsafeSVGStyle(value)
	}
	return hasExternalSVGURL(lowerValue)
}

func unsafeSVGStyle(value string) bool {
	normalized, err := normalizeCSSSecurityTokens(value)
	return err != nil || strings.Contains(normalized, "@import") || hasExternalSVGURL(normalized)
}

func hasExternalSVGURL(value string) bool {
	normalized, err := normalizeCSSSecurityTokens(value)
	if err != nil {
		return true
	}
	remaining := normalized
	for {
		index := strings.Index(remaining, "url")
		if index < 0 {
			return false
		}
		cursor := index + len("url")
		for cursor < len(remaining) && isCSSWhitespace(remaining[cursor]) {
			cursor++
		}
		if cursor >= len(remaining) || remaining[cursor] != '(' {
			remaining = remaining[index+len("url"):]
			continue
		}
		cursor++
		for cursor < len(remaining) && isCSSWhitespace(remaining[cursor]) {
			cursor++
		}
		if cursor < len(remaining) && (remaining[cursor] == '\'' || remaining[cursor] == '"') {
			cursor++
			for cursor < len(remaining) && isCSSWhitespace(remaining[cursor]) {
				cursor++
			}
		}
		closeIndex := strings.IndexByte(remaining[cursor:], ')')
		if closeIndex < 0 {
			return true
		}
		if cursor < len(remaining) && remaining[cursor] != '#' && remaining[cursor] != ')' {
			return true
		}
		remaining = remaining[cursor+closeIndex+1:]
	}
}

func normalizeCSSSecurityTokens(value string) (string, error) {
	var out strings.Builder
	for index := 0; index < len(value); {
		if index+1 < len(value) && value[index] == '/' && value[index+1] == '*' {
			end := strings.Index(value[index+2:], "*/")
			if end < 0 {
				return "", errors.New("unterminated CSS comment")
			}
			index += end + 4
			continue
		}
		if value[index] != '\\' {
			out.WriteByte(value[index])
			index++
			continue
		}
		index++
		if index >= len(value) {
			return "", errors.New("unterminated CSS escape")
		}
		if value[index] == '\n' || value[index] == '\f' {
			index++
			continue
		}
		if value[index] == '\r' {
			index++
			if index < len(value) && value[index] == '\n' {
				index++
			}
			continue
		}
		if isCSSHex(value[index]) {
			start := index
			for index < len(value) && index-start < 6 && isCSSHex(value[index]) {
				index++
			}
			decoded, err := strconv.ParseUint(value[start:index], 16, 32)
			if err != nil {
				return "", err
			}
			if index < len(value) && isCSSWhitespace(value[index]) {
				index++
			}
			if decoded == 0 || decoded > 0x10ffff || decoded >= 0xd800 && decoded <= 0xdfff {
				out.WriteRune('\ufffd')
			} else {
				out.WriteRune(rune(decoded))
			}
			continue
		}
		out.WriteByte(value[index])
		index++
	}
	return strings.ToLower(out.String()), nil
}

func isCSSHex(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}

func isCSSWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n' || value == '\f'
}

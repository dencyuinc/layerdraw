// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

var portableSchemePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

// ResolveAuthoredAssetLocator validates an unquoted authored asset spelling and
// resolves it to an origin-relative portable locator. Raw dot segments are
// allowed, while percent-encoded traversal and separators are not.
func ResolveAuthoredAssetLocator(modulePath, raw string) (string, bool) {
	if raw == "" || !utf8.ValidString(raw) || !norm.NFC.IsNormalString(raw) ||
		portableSchemePattern.MatchString(raw) || strings.HasPrefix(raw, "/") || strings.Contains(raw, "\\") || containsUnicodeControl(raw) {
		return "", false
	}
	rawSegments := strings.Split(raw, "/")
	for _, segment := range rawSegments {
		if segment == "" {
			return "", false
		}
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil || !utf8.ValidString(decoded) || !norm.NFC.IsNormalString(decoded) ||
		portableSchemePattern.MatchString(decoded) || strings.HasPrefix(decoded, "/") || strings.Contains(decoded, "\\") || containsUnicodeControl(decoded) {
		return "", false
	}
	decodedSegments := strings.Split(decoded, "/")
	if len(rawSegments) != len(decodedSegments) {
		return "", false
	}
	for i, segment := range decodedSegments {
		if segment == "" {
			return "", false
		}
		if (segment == "." || segment == "..") && rawSegments[i] != segment {
			return "", false
		}
	}
	clean := path.Clean(path.Join(path.Dir(modulePath), raw))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", false
	}
	return clean, true
}

func containsUnicodeControl(raw string) bool {
	for _, r := range raw {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func normalizePortablePath(raw string) (string, bool) {
	if raw == "" || strings.HasPrefix(raw, "/") || strings.Contains(raw, "\\") || strings.ContainsRune(raw, 0) || !utf8.ValidString(raw) {
		return "", false
	}
	if !norm.NFC.IsNormalString(raw) {
		return "", false
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", false
	}
	if !norm.NFC.IsNormalString(decoded) || strings.Count(decoded, "/") != strings.Count(raw, "/") {
		return "", false
	}
	if strings.HasPrefix(decoded, "/") || strings.Contains(decoded, "\\") || strings.ContainsRune(decoded, 0) {
		return "", false
	}
	for _, p := range strings.Split(decoded, "/") {
		if p == "" || p == "." || p == ".." {
			return "", false
		}
	}
	for _, p := range strings.Split(raw, "/") {
		if !validPortablePathSegment(p) {
			return "", false
		}
	}
	clean := path.Clean(raw)
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", false
	}
	return clean, true
}

func normalizeModulePath(raw string) (string, bool) {
	clean, ok := normalizePortablePath(raw)
	if !ok || clean != raw || !strings.HasSuffix(raw, ".ldl") {
		return "", false
	}
	parts := strings.Split(raw, "/")
	for i, p := range parts {
		if strings.Contains(p, ".ldl/") {
			return "", false
		}
		if i < len(parts)-1 {
			if strings.Contains(p, ".") || !isIdent(p) {
				return "", false
			}
			continue
		}
		base := strings.TrimSuffix(p, ".ldl")
		if base == "" || strings.Contains(base, ".") || !isIdent(base) {
			return "", false
		}
	}
	return clean, true
}

func normalizePath(raw string) (string, bool) {
	return normalizeModulePath(raw)
}

func validPortablePathSegment(segment string) bool {
	return segment != "" && segment != "." && segment != ".."
}

func resolveRelative(base, spec string) (string, bool) {
	if !(strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../")) || !strings.HasSuffix(spec, ".ldl") {
		return "", false
	}
	target := path.Clean(path.Join(path.Dir(base), spec))
	if target == "." || strings.HasPrefix(target, "../") || target == ".." || strings.HasPrefix(target, "/") {
		return "", false
	}
	return normalizeModulePath(target)
}

func packModulePath(segments []string, entry string) string {
	if len(segments) == 1 {
		return entry
	}
	return "modules/" + strings.Join(segments[1:], "/") + ".ldl"
}

func caseFoldCollisions(paths []string) [][2]string {
	seen := map[string]string{}
	var out [][2]string
	folder := cases.Fold()
	for _, p := range paths {
		fold := folder.String(p)
		if prev, ok := seen[fold]; ok && prev != p {
			out = append(out, [2]string{prev, p})
			continue
		}
		seen[fold] = p
	}
	return out
}

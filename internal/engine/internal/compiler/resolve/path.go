// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"net/url"
	"path"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

func normalizePath(raw string) (string, bool) {
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
	if strings.HasPrefix(decoded, "/") || strings.Contains(decoded, "\\") || strings.ContainsRune(decoded, 0) {
		return "", false
	}
	for _, p := range strings.Split(decoded, "/") {
		if p == "" || p == ".." {
			return "", false
		}
	}
	for _, p := range strings.Split(raw, "/") {
		if p == "" || !validModulePathSegment(p) {
			return "", false
		}
	}
	clean := path.Clean(raw)
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", false
	}
	return clean, true
}

func validModulePathSegment(segment string) bool {
	base := strings.TrimSuffix(segment, ".ldl")
	if base == "" {
		return false
	}
	return isIdent(base)
}

func resolveRelative(base, spec string) (string, bool) {
	if !(strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../")) || !strings.HasSuffix(spec, ".ldl") {
		return "", false
	}
	target := path.Clean(path.Join(path.Dir(base), spec))
	if target == "." || strings.HasPrefix(target, "../") || target == ".." || strings.HasPrefix(target, "/") {
		return "", false
	}
	return target, true
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
	for _, p := range paths {
		fold := strings.ToLower(p)
		if prev, ok := seen[fold]; ok && prev != p {
			out = append(out, [2]string{prev, p})
			continue
		}
		seen[fold] = p
	}
	return out
}

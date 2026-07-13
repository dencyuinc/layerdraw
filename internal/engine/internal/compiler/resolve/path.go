// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"net/url"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

func normalizePath(raw string) (string, bool) {
	if raw == "" || strings.HasPrefix(raw, "/") || strings.Contains(raw, "\\") || strings.ContainsRune(raw, 0) || !utf8.ValidString(raw) {
		return "", false
	}
	if !isCanonicalPathUnicode(raw) {
		return "", false
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", false
	}
	if strings.Contains(decoded, "..") {
		parts := strings.Split(decoded, "/")
		for _, p := range parts {
			if p == ".." {
				return "", false
			}
		}
	}
	for _, p := range strings.Split(raw, "/") {
		if p == "" {
			return "", false
		}
	}
	clean := path.Clean(raw)
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", false
	}
	return clean, true
}

func isCanonicalPathUnicode(raw string) bool {
	for _, r := range raw {
		if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Mc, r) {
			return false
		}
	}
	return true
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

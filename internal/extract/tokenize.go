package extract

import (
	"strings"
	"unicode"
)

// Split splits an identifier into sub-tokens by camelCase, PascalCase,
// snake_case, kebab-case. Returns lowercased pieces with length >= 2.
func Split(ident string) []string {
	if ident == "" {
		return nil
	}
	// snake / kebab first
	parts := splitDelims(ident, "_-.")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, splitCamel(p)...)
	}
	clean := out[:0]
	for _, t := range out {
		t = strings.ToLower(strings.TrimSpace(t))
		if len(t) < 2 {
			continue
		}
		if isAllDigits(t) {
			continue
		}
		clean = append(clean, t)
	}
	return clean
}

func splitDelims(s, delims string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return strings.ContainsRune(delims, r)
	})
}

func splitCamel(s string) []string {
	if s == "" {
		return nil
	}
	runes := []rune(s)
	var parts []string
	start := 0
	for i := 1; i < len(runes); i++ {
		prev, cur := runes[i-1], runes[i]
		// lower → upper:  fooBar
		if unicode.IsLower(prev) && unicode.IsUpper(cur) {
			parts = append(parts, string(runes[start:i]))
			start = i
			continue
		}
		// upper → upper → lower: HTTPHandler -> HTTP, Handler
		if unicode.IsUpper(prev) && unicode.IsUpper(cur) && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
			parts = append(parts, string(runes[start:i]))
			start = i
			continue
		}
		// letter ↔ digit boundary
		if unicode.IsLetter(prev) != unicode.IsLetter(cur) {
			parts = append(parts, string(runes[start:i]))
			start = i
		}
	}
	parts = append(parts, string(runes[start:]))
	return parts
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return s != ""
}

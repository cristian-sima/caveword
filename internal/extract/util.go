package extract

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
)

func splitLines(src []byte) []string {
	return strings.Split(string(src), "\n")
}

func snippet(lines []string, line, around int) string {
	start := line - 1 - around
	if start < 0 {
		start = 0
	}
	end := line - 1 + around
	if end >= len(lines) {
		end = len(lines) - 1
	}
	if start > end {
		return ""
	}
	return strings.Join(lines[start:end+1], "\n")
}

// normCtx returns trimmed neighborhood for sig hashing.
func normCtx(lines []string, line, around int) string {
	s := snippet(lines, line, around)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func HashCtx(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

// words extracts alphabetic tokens from prose; lowercased; len>=2.
func words(text string) []string {
	var out []string
	cur := strings.Builder{}
	flush := func() {
		if cur.Len() >= 2 {
			out = append(out, cur.String())
		}
		cur.Reset()
	}
	for _, r := range text {
		if unicode.IsLetter(r) {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

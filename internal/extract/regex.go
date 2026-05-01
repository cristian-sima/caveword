package extract

import (
	"regexp"
	"strings"
)

func init() {
	// Plain JS/TS — no JSX text masking needed.
	for _, ext := range []string{".js", ".ts", ".mjs", ".cjs"} {
		Register(ext, makeCFamily(jsKeywords, false))
	}
	// JSX/TSX — strip text content between JSX tags so user-facing
	// strings don't masquerade as identifiers.
	for _, ext := range []string{".jsx", ".tsx"} {
		Register(ext, makeCFamily(jsKeywords, true))
	}
	for _, ext := range []string{".java", ".c", ".cc", ".cpp", ".h", ".hpp", ".cs", ".rs", ".swift", ".kt", ".scala"} {
		Register(ext, makeCFamily(genericKeywords, false))
	}
	Register(".py", extractPython)
	Register(".rb", extractRuby)
	Register(".php", makeCFamily(genericKeywords, false))
}

var (
	identRe        = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]{1,}`)
	stringDoubleRe = regexp.MustCompile(`"(?:\\.|[^"\\])*"`)
	stringSingleRe = regexp.MustCompile(`'(?:\\.|[^'\\])*'`)
	stringBacktick = regexp.MustCompile("`(?:\\\\.|[^`\\\\])*`")
	commentLineRe  = regexp.MustCompile(`(?m)//[^\n]*`)
	commentBlockRe = regexp.MustCompile(`(?s)/\*.*?\*/`)
	pyCommentLine  = regexp.MustCompile(`(?m)#[^\n]*`)
	pyTripleDouble = regexp.MustCompile(`(?s)"""(.*?)"""`)
	pyTripleSingle = regexp.MustCompile(`(?s)'''(.*?)'''`)
	// jsxTextRe approximates JSX text content: from `>` (closing a
	// tag) up to the next `<` or `{`. Skipped: prop expressions and
	// nested elements. Lossy but cheap.
	jsxTextRe = regexp.MustCompile(`>[^<{]+(?:[<{])`)
)

// makeCFamily builds an extractor for languages with C-style comments and
// string literals. If jsx is true, JSX text content between tags is stripped
// before identifier scanning so user-facing strings don't pollute results.
func makeCFamily(keywords map[string]struct{}, jsx bool) Extractor {
	return func(path string, src []byte) ([]Finding, error) {
		text := string(src)
		lines := strings.Split(text, "\n")
		var out []Finding

		// Comments first (capture before stripping).
		collectMatches(commentLineRe, text, lines, "comment", path, &out)
		collectMatches(commentBlockRe, text, lines, "comment", path, &out)

		ranges := [][][]int{
			commentLineRe.FindAllStringIndex(text, -1),
			commentBlockRe.FindAllStringIndex(text, -1),
			stringDoubleRe.FindAllStringIndex(text, -1),
			stringSingleRe.FindAllStringIndex(text, -1),
			stringBacktick.FindAllStringIndex(text, -1),
		}
		if jsx {
			// Trim 1 from the right so we don't eat the next `<`/`{`.
			matches := jsxTextRe.FindAllStringIndex(text, -1)
			adj := make([][]int, 0, len(matches))
			for _, m := range matches {
				if m[1]-m[0] > 1 {
					adj = append(adj, []int{m[0] + 1, m[1] - 1})
				}
			}
			ranges = append(ranges, adj)
		}
		stripped := stripRanges(text, ranges...)

		for _, m := range identRe.FindAllStringIndex(stripped, -1) {
			ident := stripped[m[0]:m[1]]
			low := strings.ToLower(ident)
			if _, ok := keywords[low]; ok {
				continue
			}
			line, col := lineCol(stripped, m[0])
			for _, sub := range Split(ident) {
				out = append(out, Finding{
					File:    path,
					Line:    line,
					Col:     col,
					Token:   sub,
					Kind:    "ident",
					Snippet: snippet(lines, line, 3),
					Ctx:     normCtx(lines, line, 2),
				})
			}
		}
		return out, nil
	}
}

func extractPython(path string, src []byte) ([]Finding, error) {
	text := string(src)
	lines := strings.Split(text, "\n")
	var out []Finding

	collectMatches(pyCommentLine, text, lines, "comment", path, &out)
	collectMatches(pyTripleDouble, text, lines, "comment", path, &out)
	collectMatches(pyTripleSingle, text, lines, "comment", path, &out)

	stripped := stripRanges(text,
		pyCommentLine.FindAllStringIndex(text, -1),
		pyTripleDouble.FindAllStringIndex(text, -1),
		pyTripleSingle.FindAllStringIndex(text, -1),
		stringDoubleRe.FindAllStringIndex(text, -1),
		stringSingleRe.FindAllStringIndex(text, -1),
	)

	for _, m := range identRe.FindAllStringIndex(stripped, -1) {
		ident := stripped[m[0]:m[1]]
		low := strings.ToLower(ident)
		if _, ok := pyKeywords[low]; ok {
			continue
		}
		line, col := lineCol(stripped, m[0])
		for _, sub := range Split(ident) {
			out = append(out, Finding{
				File:    path,
				Line:    line,
				Col:     col,
				Token:   sub,
				Kind:    "ident",
				Snippet: snippet(lines, line, 3),
				Ctx:     normCtx(lines, line, 2),
			})
		}
	}
	return out, nil
}

func extractRuby(path string, src []byte) ([]Finding, error) {
	text := string(src)
	lines := strings.Split(text, "\n")
	var out []Finding

	collectMatches(pyCommentLine, text, lines, "comment", path, &out)

	stripped := stripRanges(text,
		pyCommentLine.FindAllStringIndex(text, -1),
		stringDoubleRe.FindAllStringIndex(text, -1),
		stringSingleRe.FindAllStringIndex(text, -1),
	)

	for _, m := range identRe.FindAllStringIndex(stripped, -1) {
		ident := stripped[m[0]:m[1]]
		low := strings.ToLower(ident)
		if _, ok := rbKeywords[low]; ok {
			continue
		}
		line, col := lineCol(stripped, m[0])
		for _, sub := range Split(ident) {
			out = append(out, Finding{
				File:    path,
				Line:    line,
				Col:     col,
				Token:   sub,
				Kind:    "ident",
				Snippet: snippet(lines, line, 3),
				Ctx:     normCtx(lines, line, 2),
			})
		}
	}
	return out, nil
}

func collectMatches(re *regexp.Regexp, text string, lines []string, kind, path string, out *[]Finding) {
	for _, m := range re.FindAllStringIndex(text, -1) {
		seg := text[m[0]:m[1]]
		line, col := lineCol(text, m[0])
		for _, w := range words(seg) {
			*out = append(*out, Finding{
				File:    path,
				Line:    line,
				Col:     col,
				Token:   strings.ToLower(w),
				Kind:    kind,
				Snippet: snippet(lines, line, 2),
				Ctx:     normCtx(lines, line, 1),
			})
		}
	}
}

func lineCol(s string, off int) (int, int) {
	if off > len(s) {
		off = len(s)
	}
	line := 1
	col := 1
	for i := 0; i < off; i++ {
		if s[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

// stripRanges replaces the given byte ranges with spaces, preserving offsets.
func stripRanges(s string, ranges ...[][]int) string {
	b := []byte(s)
	for _, group := range ranges {
		for _, r := range group {
			for i := r[0]; i < r[1] && i < len(b); i++ {
				if b[i] != '\n' {
					b[i] = ' '
				}
			}
		}
	}
	return string(b)
}

var jsKeywords = setOf(
	"abstract", "any", "as", "async", "await", "boolean", "break", "case", "catch",
	"class", "const", "constructor", "continue", "debugger", "declare", "default",
	"delete", "do", "else", "enum", "export", "extends", "false", "finally",
	"for", "from", "function", "get", "if", "implements", "import", "in", "instanceof",
	"interface", "is", "keyof", "let", "module", "namespace", "never", "new", "null",
	"number", "of", "package", "private", "protected", "public", "readonly", "require",
	"return", "set", "static", "string", "super", "switch", "symbol", "this", "throw",
	"true", "try", "type", "typeof", "undefined", "unknown", "var", "void", "while",
	"with", "yield",
)

var pyKeywords = setOf(
	"false", "none", "true", "and", "as", "assert", "async", "await", "break",
	"class", "continue", "def", "del", "elif", "else", "except", "finally", "for",
	"from", "global", "if", "import", "in", "is", "lambda", "nonlocal", "not", "or",
	"pass", "raise", "return", "try", "while", "with", "yield", "self", "cls",
	"print", "len", "range", "list", "dict", "set", "tuple", "str", "int", "float", "bool",
)

var rbKeywords = setOf(
	"alias", "and", "begin", "break", "case", "class", "def", "defined", "do",
	"else", "elsif", "end", "ensure", "false", "for", "if", "in", "module",
	"next", "nil", "not", "or", "redo", "rescue", "retry", "return", "self",
	"super", "then", "true", "undef", "unless", "until", "when", "while", "yield",
)

var genericKeywords = setOf(
	"abstract", "auto", "bool", "break", "case", "catch", "char", "class", "const",
	"continue", "default", "delete", "do", "double", "else", "enum", "extends", "extern",
	"false", "final", "finally", "float", "for", "goto", "if", "implements", "import",
	"in", "inline", "int", "interface", "let", "long", "namespace", "new", "null",
	"package", "private", "protected", "public", "register", "return", "short", "signed",
	"sizeof", "static", "struct", "super", "switch", "synchronized", "template", "this",
	"throw", "throws", "transient", "true", "try", "typedef", "typename", "union",
	"unsigned", "using", "virtual", "void", "volatile", "while", "fn", "match", "mod",
	"mut", "ref", "trait", "where", "self",
)

func setOf(items ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(items))
	for _, it := range items {
		m[it] = struct{}{}
	}
	return m
}

package extract

import (
	"path/filepath"
	"strings"
)

// MaxFileSize caps any single file scanned. Larger ones are skipped.
const MaxFileSize int64 = 512 * 1024 // 512 KiB

// ignoredDirs are directory names that are always skipped during walk.
var ignoredDirs = map[string]struct{}{
	".git":           {},
	".hg":            {},
	".svn":           {},
	".idea":          {},
	".vscode":        {},
	".caveword":      {},
	".ro-audit":      {}, // legacy state dir from when the tool was called ro-audit
	"node_modules":   {},
	"vendor":         {},
	"dist":           {},
	"build":          {},
	"out":            {},
	"target":         {},
	"bin":            {},
	"obj":            {},
	".next":          {},
	".nuxt":          {},
	".cache":         {},
	".turbo":         {},
	".parcel-cache":  {},
	".yarn":          {},
	".pnpm-store":    {},
	".gradle":        {},
	".mvn":           {},
	"coverage":       {},
	"__pycache__":    {},
	".venv":          {},
	"venv":           {},
	"env":            {},
	".tox":           {},
	".pytest_cache":  {},
	".mypy_cache":    {},
	".ruff_cache":    {},
	"site-packages":  {},
	"DerivedData":    {},
	"Pods":           {},
	".terraform":     {},
	".serverless":    {},
	"wailsjs":        {},
	"public":         {},
	"static":         {},
	"assets":         {},
	"testdata":       {},
}

// ignoredFileSuffixes are filename patterns that are always skipped.
var ignoredFileSuffixes = []string{
	".min.js", ".min.css", ".bundle.js", ".bundle.css",
	".map", ".lock", ".sum",
	".pb.go", "_pb.go", ".gen.go", "_gen.go", ".generated.go",
	".pb.cc", ".pb.h",
}

// ignoredExactNames are filenames that are always skipped.
var ignoredExactNames = map[string]struct{}{
	"package-lock.json": {},
	"yarn.lock":         {},
	"pnpm-lock.yaml":    {},
	"composer.lock":     {},
	"go.sum":            {},
	"Cargo.lock":        {},
	"Pipfile.lock":      {},
	"poetry.lock":       {},
}

// SkipDir reports whether a directory name should be skipped during walk.
func SkipDir(name string) bool {
	if name == "" {
		return false
	}
	if _, ok := ignoredDirs[name]; ok {
		return true
	}
	// Hidden dirs (".something") are skipped, except a small whitelist.
	if strings.HasPrefix(name, ".") && len(name) > 1 {
		return true
	}
	return false
}

// SkipFile reports whether a file path should be skipped before reading.
func SkipFile(path string) bool {
	base := filepath.Base(path)
	if _, ok := ignoredExactNames[base]; ok {
		return true
	}
	low := strings.ToLower(base)
	for _, suf := range ignoredFileSuffixes {
		if strings.HasSuffix(low, suf) {
			return true
		}
	}
	return false
}

// SkipPath checks whether any segment of the path is an ignored directory.
// Useful for diff-mode where the walker isn't pruning.
func SkipPath(p string) bool {
	parts := strings.Split(filepath.ToSlash(p), "/")
	for _, seg := range parts {
		if seg == "" {
			continue
		}
		if _, ok := ignoredDirs[seg]; ok {
			return true
		}
		if strings.HasPrefix(seg, ".") && len(seg) > 1 && seg != "." && seg != ".." {
			// allow hidden filenames at the leaf only; but hidden directories are skipped.
			if seg != filepath.Base(p) {
				return true
			}
		}
	}
	return false
}

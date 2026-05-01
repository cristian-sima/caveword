package extract

import (
	"path/filepath"
	"strings"
)

type Finding struct {
	File    string
	Line    int
	Col     int
	Token   string
	Kind    string // "ident" | "comment"
	Snippet string
	Ctx     string // normalized context for sig
}

type Extractor func(path string, src []byte) ([]Finding, error)

var registry = map[string]Extractor{}

func Register(ext string, fn Extractor) {
	registry[strings.ToLower(ext)] = fn
}

func For(path string) Extractor {
	return registry[strings.ToLower(filepath.Ext(path))]
}

func Supported() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}

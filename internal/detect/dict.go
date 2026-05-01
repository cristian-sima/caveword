package detect

import (
	_ "embed"
	"strings"
	"sync"
)

//go:embed words_alpha.txt
var wordsAlpha string

var (
	enDictOnce sync.Once
	enDict     map[string]struct{}
)

// EnDict returns a set of common English words. Used to short-circuit
// classification: if a token is a known English word, it cannot be Romanian.
func EnDict() map[string]struct{} {
	enDictOnce.Do(func() {
		enDict = make(map[string]struct{}, 400_000)
		for _, line := range strings.Split(wordsAlpha, "\n") {
			w := strings.TrimSpace(line)
			if w == "" {
				continue
			}
			enDict[strings.ToLower(w)] = struct{}{}
		}
	})
	return enDict
}

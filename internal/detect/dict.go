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

// EnDict returns the embedded English wordlist as a set. Used to
// short-circuit classification when English is the target language: a
// token already in the dictionary is accepted as on-target without
// consulting lingua.
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

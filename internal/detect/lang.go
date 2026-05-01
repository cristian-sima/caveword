package detect

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	lingua "github.com/pemistahl/lingua-go"
)

// CodeToLingua maps two-letter ISO 639-1 codes to lingua.Language values for
// the languages caveword can classify. Add more by extending this map and
// dropping a `dict_<code>.txt` file into one of the search paths.
var CodeToLingua = map[string]lingua.Language{
	"en": lingua.English,
	"ro": lingua.Romanian,
	"fr": lingua.French,
	"de": lingua.German,
	"es": lingua.Spanish,
	"it": lingua.Italian,
	"pt": lingua.Portuguese,
	"nl": lingua.Dutch,
	"pl": lingua.Polish,
	"ru": lingua.Russian,
	"uk": lingua.Ukrainian,
	"cs": lingua.Czech,
	"sk": lingua.Slovak,
	"hu": lingua.Hungarian,
	"tr": lingua.Turkish,
	"sv": lingua.Swedish,
	"no": lingua.Bokmal,
	"da": lingua.Danish,
	"fi": lingua.Finnish,
	"el": lingua.Greek,
	"bg": lingua.Bulgarian,
	"hr": lingua.Croatian,
	"sr": lingua.Serbian,
	"sl": lingua.Slovene,
	"lt": lingua.Lithuanian,
	"lv": lingua.Latvian,
	"et": lingua.Estonian,
	"ja": lingua.Japanese,
	"ko": lingua.Korean,
	"zh": lingua.Chinese,
	"ar": lingua.Arabic,
	"he": lingua.Hebrew,
	"fa": lingua.Persian,
	"hi": lingua.Hindi,
	"th": lingua.Thai,
	"vi": lingua.Vietnamese,
	"id": lingua.Indonesian,
	"ms": lingua.Malay,
}

// LangCode returns the canonical (lower-case) two-letter code for a name like
// "english" / "EN" / "en-US". Returns "" if unknown.
func LangCode(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '-'); i > 0 {
		s = s[:i]
	}
	if _, ok := CodeToLingua[s]; ok {
		return s
	}
	// Allow long names like "english" / "romanian"
	long := map[string]string{
		"english": "en", "romanian": "ro", "french": "fr", "german": "de",
		"spanish": "es", "italian": "it", "portuguese": "pt", "dutch": "nl",
		"polish": "pl", "russian": "ru", "ukrainian": "uk", "czech": "cs",
		"slovak": "sk", "hungarian": "hu", "turkish": "tr", "swedish": "sv",
		"norwegian": "no", "danish": "da", "finnish": "fi", "greek": "el",
		"bulgarian": "bg", "croatian": "hr", "serbian": "sr", "slovene": "sl",
		"lithuanian": "lt", "latvian": "lv", "estonian": "et",
		"japanese": "ja", "korean": "ko", "chinese": "zh", "arabic": "ar",
		"hebrew": "he", "persian": "fa", "hindi": "hi", "thai": "th",
		"vietnamese": "vi", "indonesian": "id", "malay": "ms",
	}
	if c, ok := long[s]; ok {
		return c
	}
	return ""
}

// LoadDict reads a wordlist file (one lowercased token per line) into a set.
// Empty lines and lines starting with '#' are ignored.
func LoadDict(path string) (map[string]struct{}, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make(map[string]struct{}, 1<<14)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		w := strings.TrimSpace(sc.Text())
		if w == "" || strings.HasPrefix(w, "#") {
			continue
		}
		out[strings.ToLower(w)] = struct{}{}
	}
	return out, sc.Err()
}

// DictSearchPaths returns the directories caveword looks in for `dict_<code>.txt`,
// in priority order. The repo-local one wins over the user-global one.
func DictSearchPaths(repoRoot string) []string {
	paths := []string{
		filepath.Join(repoRoot, ".caveword", "dicts"),
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".caveword", "dicts"))
	}
	return paths
}

// FindDict returns the first existing dict_<code>.txt across the search paths,
// or "" if none exist.
func FindDict(repoRoot, code string) string {
	for _, dir := range DictSearchPaths(repoRoot) {
		p := filepath.Join(dir, fmt.Sprintf("dict_%s.txt", code))
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

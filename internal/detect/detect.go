package detect

import (
	"fmt"
	"strings"
	"sync"

	lingua "github.com/pemistahl/lingua-go"
)

// Result is what Classify returns for a single token.
type Result struct {
	Flagged    bool    // token looks like one of the off-target languages
	Lang       string  // detected off-target language code (when Flagged)
	TargetConf float64 // lingua confidence for the target language
	OffConf    float64 // lingua confidence for the winning off-target language
	Reason     string  // why we did or didn't flag (allowlist / target-dict / lingua / …)
}

// Config is everything the Detector needs to know.
type Config struct {
	// Target is the ISO-639-1 code identifiers SHOULD be in (e.g. "en").
	Target string
	// Detect is the list of off-target languages to score against (e.g. ["ro", "fr"]).
	Detect []string
	// Dicts maps each language code to a wordlist set. The Target dict is
	// the most important: tokens present in it are accepted as-is.
	// Off-target dicts give a cheap, deterministic flag when a token is in
	// the dictionary of an off-target language but not in the target.
	Dicts map[string]map[string]struct{}
	// Allowlist holds project-domain short codes (brands, acronyms) that
	// should never be flagged regardless of language model.
	Allowlist []string
	// ExtraStopwords extends the built-in stop-list (short tech tokens).
	ExtraStopwords []string
	// MinLen is the minimum token length classified by lingua. Below this,
	// scoring is unreliable so the token is dropped.
	MinLen int
	// Margin is the lingua confidence margin the off-target language must
	// beat the target by before the token gets flagged.
	Margin float64
}

// Detector classifies single identifier tokens against a configured target /
// off-target set.
type Detector struct {
	cfg       Config
	det       lingua.LanguageDetector
	target    lingua.Language
	off       []lingua.Language
	allow     map[string]struct{}
	stopwords map[string]struct{}
}

var (
	defaultOnce sync.Once
	defaultDet  *Detector
	defaultErr  error
)

// Default returns a singleton detector configured for English vs Romanian
// using the embedded English dictionary. Useful when no project config is
// present.
func Default() *Detector {
	defaultOnce.Do(func() {
		defaultDet, defaultErr = New(Config{
			Target:    "en",
			Detect:    []string{"ro"},
			Dicts:     map[string]map[string]struct{}{"en": EnDict()},
			Allowlist: DefaultAllowlist(),
			MinLen:    4,
			Margin:    0.30,
		})
	})
	if defaultErr != nil {
		panic(defaultErr)
	}
	return defaultDet
}

// New builds a Detector from cfg.
func New(cfg Config) (*Detector, error) {
	if cfg.Target == "" {
		cfg.Target = "en"
	}
	if cfg.MinLen == 0 {
		cfg.MinLen = 4
	}
	if cfg.Margin == 0 {
		cfg.Margin = 0.30
	}
	target, ok := CodeToLingua[cfg.Target]
	if !ok {
		return nil, fmt.Errorf("unknown target language code %q", cfg.Target)
	}
	langs := []lingua.Language{target}
	off := make([]lingua.Language, 0, len(cfg.Detect))
	for _, c := range cfg.Detect {
		c = LangCode(c)
		if c == cfg.Target || c == "" {
			continue
		}
		l, ok := CodeToLingua[c]
		if !ok {
			return nil, fmt.Errorf("unknown detect language code %q", c)
		}
		langs = append(langs, l)
		off = append(off, l)
	}
	if len(langs) < 2 {
		// Lingua needs at least two languages to score; default to RO as
		// the off-target if the caller didn't pick any.
		ro := lingua.Romanian
		langs = append(langs, ro)
		off = append(off, ro)
	}
	det := lingua.NewLanguageDetectorBuilder().
		FromLanguages(langs...).
		WithPreloadedLanguageModels().
		Build()

	allow := setOf(cfg.Allowlist)
	stops := make(map[string]struct{}, len(builtinStopwords)+len(cfg.ExtraStopwords))
	for k, v := range builtinStopwords {
		stops[k] = v
	}
	for _, w := range cfg.ExtraStopwords {
		stops[strings.ToLower(strings.TrimSpace(w))] = struct{}{}
	}
	if cfg.Dicts == nil {
		cfg.Dicts = map[string]map[string]struct{}{}
	}
	if _, ok := cfg.Dicts[cfg.Target]; !ok && cfg.Target == "en" {
		cfg.Dicts["en"] = EnDict()
	}
	return &Detector{
		cfg:       cfg,
		det:       det,
		target:    target,
		off:       off,
		allow:     allow,
		stopwords: stops,
	}, nil
}

// Classify scores a single token against the configured languages.
func (d *Detector) Classify(token string) Result {
	t := strings.ToLower(strings.TrimSpace(token))
	if t == "" {
		return Result{Reason: "empty"}
	}
	if _, ok := d.allow[t]; ok {
		return Result{Reason: "allowlist"}
	}
	if _, ok := d.stopwords[t]; ok {
		return Result{Reason: "stopword"}
	}
	if len([]rune(t)) < d.cfg.MinLen {
		return Result{Reason: "too-short"}
	}
	if dict, ok := d.cfg.Dicts[d.cfg.Target]; ok {
		if _, hit := dict[t]; hit {
			return Result{Reason: "target-dict"}
		}
	}
	// Hard hit: token is in an off-target dict but not the target dict.
	for _, code := range d.cfg.Detect {
		code = LangCode(code)
		if code == "" || code == d.cfg.Target {
			continue
		}
		if dict, ok := d.cfg.Dicts[code]; ok {
			if _, hit := dict[t]; hit {
				return Result{Flagged: true, Lang: code, Reason: "off-dict"}
			}
		}
	}
	confs := d.det.ComputeLanguageConfidenceValues(t)
	var targetConf float64
	bestOff := struct {
		lang lingua.Language
		conf float64
	}{}
	for _, c := range confs {
		if c.Language() == d.target {
			targetConf = c.Value()
			continue
		}
		if c.Value() > bestOff.conf {
			bestOff.lang = c.Language()
			bestOff.conf = c.Value()
		}
	}
	r := Result{TargetConf: targetConf, OffConf: bestOff.conf}
	if bestOff.conf > targetConf+d.cfg.Margin {
		r.Flagged = true
		r.Lang = linguaToCode[bestOff.lang]
		r.Reason = "lingua"
	} else {
		r.Reason = "lingua-target-or-tie"
	}
	return r
}

// linguaToCode is the reverse of CodeToLingua, built lazily.
var linguaToCode = func() map[lingua.Language]string {
	m := make(map[lingua.Language]string, len(CodeToLingua))
	for c, l := range CodeToLingua {
		m[l] = c
	}
	return m
}()

func setOf(items []string) map[string]struct{} {
	m := make(map[string]struct{}, len(items))
	for _, it := range items {
		m[strings.ToLower(strings.TrimSpace(it))] = struct{}{}
	}
	return m
}

// builtinStopwords are short tech tokens or obvious English compounds that
// lingua mis-scores. They get dropped before the language model runs.
var builtinStopwords = map[string]struct{}{
	"id": {}, "ok": {}, "err": {}, "ctx": {}, "req": {}, "res": {}, "fn": {},
	"db": {}, "io": {}, "os": {}, "fs": {}, "ip": {}, "ui": {}, "ux": {},
	"http": {}, "https": {}, "url": {}, "uri": {}, "json": {}, "xml": {}, "html": {}, "css": {},
	"sql": {}, "tcp": {}, "udp": {}, "tls": {}, "ssh": {}, "ssl": {}, "csv": {}, "yaml": {}, "toml": {},
	"api": {}, "sdk": {}, "cli": {}, "gui": {}, "rpc": {}, "ws": {}, "uuid": {},
	"min": {}, "max": {}, "sum": {}, "len": {}, "num": {}, "str": {}, "int": {},
	"src": {}, "dst": {}, "tmp": {}, "buf": {}, "ptr": {}, "arr": {}, "map": {},
	"foo": {}, "bar": {}, "baz": {}, "todo": {}, "fixme": {}, "xxx": {}, "nb": {},
	"stderr": {}, "stdout": {}, "stdin": {},
	"sqlite": {}, "sqlite3": {},
	"regex": {}, "regexp": {},
	"metadata": {},
	"goroutine": {}, "goroutines": {}, "mutex": {}, "rwmutex": {},
	"jsx": {}, "tsx": {}, "ecma": {},
	"dedup": {}, "dedupe": {},
	"txns": {}, "txn": {},
	"pdfs": {}, "pdfa": {}, "epub": {},
	"setdefault": {}, "getdefault": {}, "asdict": {}, "fromdict": {},
	"appdata": {}, "localdata": {}, "userdata": {}, "metadatafile": {},
	"textline": {}, "textlines": {}, "textbox": {},
	"doctr": {}, "paddleocr": {}, "easyocr": {}, "pytesseract": {},
}

// DefaultAllowlist holds project-specific short codes that score high on
// language models but are not real off-target words. Keep this list small;
// real off-language identifiers should surface as findings and be reviewed
// per-occurrence.
func DefaultAllowlist() []string {
	return []string{
		// Project / brand
		"saga", "sidework", "sideview", "sidesand", "sidenote",
		// Government / accounting standards short codes
		"cif", "anaf", "spv", "anafspv", "tva", "cui", "iban",
		// Bank brand / SWIFT short codes
		"raiffeisen", "rzbr", "brd", "bcr", "ing", "alpha", "unicredit",
	}
}

package detect

import (
	"strings"
	"sync"

	lingua "github.com/pemistahl/lingua-go"
)

type Result struct {
	IsRomanian   bool
	RoConfidence float64
	EnConfidence float64
	Reason       string
}

type Detector struct {
	det      lingua.LanguageDetector
	allow    map[string]struct{}
	en       map[string]struct{}
	minLen   int
	roMargin float64
}

var (
	defaultOnce sync.Once
	defaultDet  *Detector
)

// Default returns a singleton detector with sane defaults.
func Default() *Detector {
	defaultOnce.Do(func() {
		defaultDet = New(DefaultAllowlist(), 4, 0.30)
	})
	return defaultDet
}

func New(allow []string, minLen int, roMargin float64) *Detector {
	languages := []lingua.Language{lingua.Romanian, lingua.English}
	det := lingua.NewLanguageDetectorBuilder().
		FromLanguages(languages...).
		WithPreloadedLanguageModels().
		Build()
	m := make(map[string]struct{}, len(allow))
	for _, a := range allow {
		m[strings.ToLower(a)] = struct{}{}
	}
	return &Detector{det: det, allow: m, en: EnDict(), minLen: minLen, roMargin: roMargin}
}

func (d *Detector) Classify(token string) Result {
	t := strings.ToLower(strings.TrimSpace(token))
	if t == "" {
		return Result{Reason: "empty"}
	}
	if _, ok := d.allow[t]; ok {
		return Result{Reason: "allowlist"}
	}
	if _, ok := stopwords[t]; ok {
		return Result{Reason: "stopword"}
	}
	if len(t) < d.minLen {
		return Result{Reason: "too-short"}
	}
	if _, ok := d.en[t]; ok {
		return Result{Reason: "en-dict"}
	}
	confs := d.det.ComputeLanguageConfidenceValues(t)
	var ro, en float64
	for _, c := range confs {
		switch c.Language() {
		case lingua.Romanian:
			ro = c.Value()
		case lingua.English:
			en = c.Value()
		}
	}
	r := Result{RoConfidence: ro, EnConfidence: en}
	if ro > en+d.roMargin {
		r.IsRomanian = true
		r.Reason = "lingua"
	} else {
		r.Reason = "lingua-en-or-tie"
	}
	return r
}

// stopwords are short tech tokens that aren't worth scoring as RO/EN.
// Common English words are filtered by the embedded English dictionary
// instead — keep this list to short identifiers, acronyms, and a few
// well-known English/tech terms lingua keeps mis-scoring as Romanian.
var stopwords = map[string]struct{}{
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
	// Technical English tokens not in the wordlist (compounds, abbreviations,
	// library/method names). NOT Romanian — adding here only because lingua
	// scores them RO due to single-token weakness.
	"dedup": {}, "dedupe": {},
	"txns": {}, "txn": {},
	"pdfs": {}, "pdfa": {}, "epub": {},
	"setdefault": {}, "getdefault": {}, "asdict": {}, "fromdict": {},
	"appdata": {}, "localdata": {}, "userdata": {}, "metadatafile": {},
	"textline": {}, "textlines": {}, "textbox": {},
	"doctr": {}, "paddleocr": {}, "easyocr": {}, "pytesseract": {},
}

// DefaultAllowlist is intentionally small. A token-level allowlist
// suppresses *signal* — it tells the tool to never flag a word as RO
// even when it appears as a code identifier, which is exactly the case
// we want to catch. Real RO domain terms (factura, cont, denumire, …)
// are reviewed on a per-finding basis and marked `domain_ok` in the
// verdict store; that records "this RO identifier here is accepted"
// without blanding away every future occurrence.
//
// What stays in this list: project / product / brand short codes that
// are not real Romanian words (they happen to score high on lingua RO)
// and short standardised acronyms.
func DefaultAllowlist() []string {
	return []string{
		// Project / brand short codes
		"saga", "sidework", "sideview", "sidesand", "sidenote",
		// Government / accounting standards short codes (all-caps acronyms
		// in source; here lowercased after tokenization)
		"cif", "anaf", "spv", "anafspv", "tva", "cui", "iban",
		// Bank brand / SWIFT short codes
		"raiffeisen", "rzbr", "brd", "bcr", "ing", "alpha", "unicredit",
	}
}

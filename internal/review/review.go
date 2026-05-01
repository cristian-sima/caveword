package review

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/cristian-sima/caveword/internal/store"
)

// ValidVerdicts are the labels Claude (or a human) may apply.
var ValidVerdicts = map[string]struct{}{
	"ro_confirmed": {}, // genuinely Romanian, candidate for renaming
	"en_actually":  {}, // false positive — really English
	"proper_noun":  {}, // person/place/brand — leave alone
	"domain_ok":    {}, // RO but accepted domain term — promote to allowlist
	"ambiguous":    {}, // can't tell from this snippet alone
}

// ExportItem is the JSON record sent out for review.
type ExportItem struct {
	Sig         string  `json:"sig"`
	File        string  `json:"file"`
	Line        int     `json:"line"`
	Col         int     `json:"col"`
	Token       string  `json:"token"`
	Kind        string  `json:"kind"`
	Snippet     string  `json:"snippet"`
	RoConf      float64 `json:"ro_conf"`
	EnConf      float64 `json:"en_conf"`
	Verdict     string  `json:"verdict,omitempty"`
	SuggestedEn string  `json:"suggested_en,omitempty"`
	Note        string  `json:"note,omitempty"`
}

// Export writes pending findings as JSON for offline / Claude review.
func Export(s *store.Store, w io.Writer, limit int) (int, error) {
	pending, err := s.ListPending(limit)
	if err != nil {
		return 0, err
	}
	items := make([]ExportItem, 0, len(pending))
	for _, p := range pending {
		items = append(items, ExportItem{
			Sig: p.Sig, File: p.File, Line: p.Line, Col: p.Col,
			Token: p.Token, Kind: p.Kind, Snippet: p.Snippet,
			RoConf: p.RoConf, EnConf: p.EnConf,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(items); err != nil {
		return 0, err
	}
	return len(items), nil
}

// Apply ingests reviewed items and writes verdicts.
func Apply(s *store.Store, r io.Reader, reviewer string) (int, error) {
	dec := json.NewDecoder(r)
	var items []ExportItem
	if err := dec.Decode(&items); err != nil {
		return 0, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	n := 0
	for _, it := range items {
		if it.Verdict == "" {
			continue
		}
		if _, ok := ValidVerdicts[it.Verdict]; !ok {
			return n, fmt.Errorf("invalid verdict %q for sig %s", it.Verdict, it.Sig)
		}
		v := store.Verdict{
			Sig: it.Sig, Verdict: it.Verdict,
			SuggestedEn: it.SuggestedEn, Note: it.Note,
			ReviewedAt: now, Reviewer: reviewer,
		}
		if err := s.SaveVerdict(v); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

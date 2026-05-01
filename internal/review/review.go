package review

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/cristian-sima/caveword/internal/store"
)

// ValidVerdicts are the canonical labels a reviewer (human or model)
// may apply to a finding.
var ValidVerdicts = map[string]struct{}{
	"confirmed":      {}, // genuinely off-target language; rename
	"false_positive": {}, // actually target language; tool was wrong
	"proper_noun":    {}, // person, brand, library — leave alone
	"domain_ok":      {}, // off-target but accepted (DB column, schema, …)
	"ambiguous":      {}, // can't tell from this snippet alone
}

// verdictAliases maps deprecated labels to their canonical equivalents,
// so review JSON files written by older versions of the tool keep working.
var verdictAliases = map[string]string{
	"ro_confirmed": "confirmed",
	"en_actually":  "false_positive",
}

// canonicalVerdict resolves a label through the alias table and returns the
// canonical name plus whether the label (after aliasing) is valid.
func canonicalVerdict(label string) (string, bool) {
	if v, ok := verdictAliases[label]; ok {
		label = v
	}
	_, ok := ValidVerdicts[label]
	return label, ok
}

// ExportItem is the JSON record sent out for review.
//
// JSON field names ro_conf / en_conf are kept stable for backward
// compatibility with reviewed batches written by earlier versions of the
// tool. Their semantics are now off-target / target lingua confidence.
type ExportItem struct {
	Sig         string  `json:"sig"`
	File        string  `json:"file"`
	Line        int     `json:"line"`
	Col         int     `json:"col"`
	Token       string  `json:"token"`
	Kind        string  `json:"kind"`
	Snippet     string  `json:"snippet"`
	OffConf     float64 `json:"ro_conf"`
	TargetConf  float64 `json:"en_conf"`
	Verdict     string  `json:"verdict,omitempty"`
	SuggestedEn string  `json:"suggested_en,omitempty"`
	Note        string  `json:"note,omitempty"`
}

// Export writes pending findings as JSON for offline review.
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
			OffConf: p.OffConf, TargetConf: p.TargetConf,
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
		canonical, ok := canonicalVerdict(it.Verdict)
		if !ok {
			return n, fmt.Errorf("invalid verdict %q for sig %s", it.Verdict, it.Sig)
		}
		v := store.Verdict{
			Sig: it.Sig, Verdict: canonical,
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

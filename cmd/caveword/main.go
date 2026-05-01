package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cristian-sima/caveword/internal/config"
	"github.com/cristian-sima/caveword/internal/detect"
	"github.com/cristian-sima/caveword/internal/diff"
	"github.com/cristian-sima/caveword/internal/extract"
	"github.com/cristian-sima/caveword/internal/review"
	"github.com/cristian-sima/caveword/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "scan":
		err = runScan(args)
	case "export":
		err = runExport(args)
	case "apply":
		err = runApply(args)
	case "status":
		err = runStatus(args)
	case "list":
		err = runList(args)
	case "dump":
		err = runDump(args)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `caveword — flag identifiers, paths, and comments that look
like the wrong language. Configurable target / off-target language pair
(default: target=en, detect=ro). See README for adding more languages.

Usage:
  caveword scan   [--repo PATH] [--diff BASE] [--ext .go,.ts,...] [--kinds ident,path]
  caveword export [--repo PATH] [--limit N] [-o FILE]
  caveword apply  [--repo PATH] [-i FILE] [--reviewer NAME]
  caveword status [--repo PATH]
  caveword list   [--repo PATH] [--pending] [--limit N]
  caveword dump   [--repo PATH] [--only reviewed|pending|all] [-o FILE]

State lives in <repo>/.caveword/verdicts.db. Add it to .gitignore.`)
}

func runScan(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	repo := fs.String("repo", ".", "repo root")
	base := fs.String("diff", "", "git base ref for diff-mode (empty = full scan)")
	extList := fs.String("ext", "", "comma-separated extensions to limit scan (default: all supported)")
	kindList := fs.String("kinds", "ident,path", "comma-separated finding kinds to keep (ident,comment,path)")
	target := fs.String("target", "", "target language ISO code (e.g. en); overrides config.json")
	detectFlag := fs.String("detect", "", "comma-separated off-target language codes (e.g. ro,fr)")
	margin := fs.Float64("margin", 0, "lingua margin off-target must beat target by; 0 keeps default")
	minLen := fs.Int("min-len", 0, "minimum token length to classify; 0 keeps default")
	verbose := fs.Bool("v", false, "verbose")
	dryRun := fs.Bool("dry-run", false, "just list which dirs/files would be scanned, then exit")
	fs.Parse(args)

	repoRoot, err := filepath.Abs(*repo)
	if err != nil {
		return err
	}
	allowedExt := parseExt(*extList)
	allowedKinds := parseKinds(*kindList)

	st, err := store.Open(repoRoot)
	if err != nil {
		return err
	}
	defer st.Close()

	det, err := buildDetector(repoRoot, *target, *detectFlag, *margin, *minLen, *verbose)
	if err != nil {
		return err
	}

	files, err := pickFiles(repoRoot, *base, allowedExt)
	if err != nil {
		return err
	}
	if *dryRun {
		printDryRun(repoRoot, files)
		return nil
	}
	if *verbose {
		fmt.Fprintf(os.Stderr, "scanning %d files\n", len(files))
	}

	scanStart := time.Now().UTC()
	var totalCandidates, flagged, carried int
	for _, f := range files {
		ex := extract.For(f)
		if ex == nil {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			if *verbose {
				fmt.Fprintf(os.Stderr, "skip %s: %v\n", f, err)
			}
			continue
		}
		findings, err := ex(f, src)
		if err != nil {
			if *verbose {
				fmt.Fprintf(os.Stderr, "parse %s: %v\n", f, err)
			}
			continue
		}
		rel, _ := filepath.Rel(repoRoot, f)
		rel = filepath.ToSlash(rel)
		// Path tokens — check file basename and directory segments.
		pathFindings := pathTokens(rel)
		findings = append(findings, pathFindings...)
		for _, fi := range findings {
			if allowedKinds != nil && !allowedKinds[fi.Kind] {
				continue
			}
			totalCandidates++
			res := det.Classify(fi.Token)
			if !res.Flagged {
				continue
			}
			flagged++
			ctxHash := store.CtxHash(fi.Ctx)
			sig := store.Sig(fi.Token, fi.Ctx)
			sf := store.Finding{
				Sig: sig, File: rel, Line: fi.Line, Col: fi.Col,
				Token: fi.Token, Kind: fi.Kind, Snippet: fi.Snippet,
				CtxHash: ctxHash,
				// ro_conf / en_conf in the schema map to off-target /
				// target lingua confidence respectively (the column names
				// predate the multi-language refactor).
				OffConf: res.OffConf, TargetConf: res.TargetConf,
			}
			if err := st.UpsertFinding(sf); err != nil {
				return err
			}
			// fuzzy carry verdict from rename / context shift
			if existing, _ := st.GetVerdict(sig); existing == nil {
				if v, _ := st.FuzzyCarry(sf, 2); v != nil {
					if err := st.SaveVerdict(*v); err != nil {
						return err
					}
					carried++
				}
			}
		}
	}

	if *base == "" {
		// Full scan: prune findings not seen this run.
		if _, err := st.PruneStale(scanStart.Add(-1 * time.Second)); err != nil {
			return err
		}
	}

	fmt.Printf("scanned: candidates=%d flagged=%d carried=%d\n", totalCandidates, flagged, carried)
	return nil
}

func runExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	repo := fs.String("repo", ".", "repo root")
	limit := fs.Int("limit", 200, "max findings to export")
	outPath := fs.String("o", "-", "output file (- for stdout)")
	fs.Parse(args)

	repoRoot, err := filepath.Abs(*repo)
	if err != nil {
		return err
	}
	st, err := store.Open(repoRoot)
	if err != nil {
		return err
	}
	defer st.Close()

	var w *os.File
	if *outPath == "-" {
		w = os.Stdout
	} else {
		w, err = os.Create(*outPath)
		if err != nil {
			return err
		}
		defer w.Close()
	}
	n, err := review.Export(st, w, *limit)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "exported %d findings\n", n)
	return nil
}

func runApply(args []string) error {
	fs := flag.NewFlagSet("apply", flag.ExitOnError)
	repo := fs.String("repo", ".", "repo root")
	inPath := fs.String("i", "-", "input file (- for stdin)")
	reviewer := fs.String("reviewer", "claude", "reviewer name")
	fs.Parse(args)

	repoRoot, err := filepath.Abs(*repo)
	if err != nil {
		return err
	}
	st, err := store.Open(repoRoot)
	if err != nil {
		return err
	}
	defer st.Close()

	var r *os.File
	if *inPath == "-" {
		r = os.Stdin
	} else {
		r, err = os.Open(*inPath)
		if err != nil {
			return err
		}
		defer r.Close()
	}
	n, err := review.Apply(st, r, *reviewer)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "applied %d verdicts\n", n)
	return nil
}

func runStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	repo := fs.String("repo", ".", "repo root")
	fs.Parse(args)
	repoRoot, _ := filepath.Abs(*repo)
	st, err := store.Open(repoRoot)
	if err != nil {
		return err
	}
	defer st.Close()
	total, reviewed, err := st.Counts()
	if err != nil {
		return err
	}
	fmt.Printf("findings: %d   reviewed: %d   pending: %d\n", total, reviewed, total-reviewed)
	return nil
}

func runList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	repo := fs.String("repo", ".", "repo root")
	pending := fs.Bool("pending", true, "only pending")
	limit := fs.Int("limit", 50, "max rows")
	fs.Parse(args)
	if !*pending {
		return errors.New("only --pending listing supported in MVP")
	}
	repoRoot, _ := filepath.Abs(*repo)
	st, err := store.Open(repoRoot)
	if err != nil {
		return err
	}
	defer st.Close()
	rows, err := st.ListPending(*limit)
	if err != nil {
		return err
	}
	for _, r := range rows {
		fmt.Printf("%s:%d  [%s]  %s   off=%.2f target=%.2f\n", r.File, r.Line, r.Kind, r.Token, r.OffConf, r.TargetConf)
	}
	return nil
}

func runDump(args []string) error {
	fs := flag.NewFlagSet("dump", flag.ExitOnError)
	repo := fs.String("repo", ".", "repo root")
	only := fs.String("only", "all", "filter: reviewed | pending | all")
	outPath := fs.String("o", "-", "output file (- for stdout)")
	fs.Parse(args)
	repoRoot, err := filepath.Abs(*repo)
	if err != nil {
		return err
	}
	st, err := store.Open(repoRoot)
	if err != nil {
		return err
	}
	defer st.Close()
	rows, err := st.ListAll()
	if err != nil {
		return err
	}
	type item struct {
		Sig         string  `json:"sig"`
		File        string  `json:"file"`
		Line        int     `json:"line"`
		Col         int     `json:"col"`
		Token       string  `json:"token"`
		Kind        string  `json:"kind"`
		Snippet     string  `json:"snippet"`
		// JSON keys ro_conf / en_conf retained for backward compatibility
		// with batches written by older versions of the tool. They now
		// hold off-target / target lingua confidence respectively.
		OffConf    float64 `json:"ro_conf"`
		TargetConf float64 `json:"en_conf"`
		Verdict     string  `json:"verdict,omitempty"`
		SuggestedEn string  `json:"suggested_en,omitempty"`
		Note        string  `json:"note,omitempty"`
		Reviewer    string  `json:"reviewer,omitempty"`
		ReviewedAt  string  `json:"reviewed_at,omitempty"`
		CarriedFrom string  `json:"carried_from,omitempty"`
	}
	keep := func(v string) bool {
		switch strings.ToLower(*only) {
		case "all", "":
			return true
		case "reviewed":
			return v != ""
		case "pending":
			return v == ""
		}
		return true
	}
	items := make([]item, 0, len(rows))
	for _, r := range rows {
		if !keep(r.Verdict) {
			continue
		}
		items = append(items, item{
			Sig: r.Sig, File: r.File, Line: r.Line, Col: r.Col,
			Token: r.Token, Kind: r.Kind, Snippet: r.Snippet,
			OffConf: r.OffConf, TargetConf: r.TargetConf,
			Verdict: r.Verdict, SuggestedEn: r.SuggestedEn, Note: r.Note,
			Reviewer: r.Reviewer, ReviewedAt: r.ReviewedAt, CarriedFrom: r.CarriedFrom,
		})
	}
	var w *os.File
	if *outPath == "-" {
		w = os.Stdout
	} else {
		w, err = os.Create(*outPath)
		if err != nil {
			return err
		}
		defer w.Close()
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(items); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "dumped %d items\n", len(items))
	return nil
}

// pickFiles returns the list of absolute file paths to scan.
func pickFiles(repoRoot, base string, allowedExt map[string]bool) ([]string, error) {
	var paths []string
	if base != "" {
		ps, err := diff.ChangedFiles(repoRoot, base)
		if err != nil {
			return nil, err
		}
		paths = ps
	} else {
		// Prefer git-tracked files (honors .gitignore). Fallback to walk.
		if ps, err := diff.TrackedFiles(repoRoot); err == nil && len(ps) > 0 {
			paths = ps
		} else {
			err := filepath.WalkDir(repoRoot, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					if extract.SkipDir(d.Name()) {
						return fs.SkipDir
					}
					return nil
				}
				paths = append(paths, p)
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
	}
	out := paths[:0]
	for _, p := range paths {
		rel, _ := filepath.Rel(repoRoot, p)
		if extract.SkipPath(rel) || extract.SkipFile(p) {
			continue
		}
		ext := strings.ToLower(filepath.Ext(p))
		if allowedExt != nil && !allowedExt[ext] {
			continue
		}
		if extract.For(p) == nil {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.Size() > extract.MaxFileSize {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// buildDetector loads the per-repo config (if present), applies CLI overrides,
// loads any project-local or user-global dictionaries for the target / detect
// languages, and returns a configured Detector.
func buildDetector(repoRoot, target, detectCSV string, margin float64, minLen int, verbose bool) (*detect.Detector, error) {
	cfg, err := config.Load(repoRoot)
	if err != nil {
		return nil, err
	}
	if target != "" {
		cfg.Target = target
	}
	if detectCSV != "" {
		cfg.Detect = splitCSV(detectCSV)
	}
	if margin != 0 {
		cfg.Margin = margin
	}
	if minLen != 0 {
		cfg.MinLen = minLen
	}
	if cfg.Target == "" {
		cfg.Target = "en"
	}
	if len(cfg.Detect) == 0 {
		cfg.Detect = []string{"ro"}
	}

	dicts := map[string]map[string]struct{}{}
	codes := append([]string{cfg.Target}, cfg.Detect...)
	for _, c := range codes {
		c = detect.LangCode(c)
		if c == "" {
			continue
		}
		if c == "en" {
			dicts["en"] = detect.EnDict()
			continue
		}
		path := detect.FindDict(repoRoot, c)
		if path == "" {
			if verbose {
				fmt.Fprintf(os.Stderr, "no dict_%s.txt found in %v — falling back to lingua only\n",
					c, detect.DictSearchPaths(repoRoot))
			}
			continue
		}
		d, err := detect.LoadDict(path)
		if err != nil {
			return nil, fmt.Errorf("load dict %s: %w", path, err)
		}
		dicts[c] = d
		if verbose {
			fmt.Fprintf(os.Stderr, "loaded %d entries from %s\n", len(d), path)
		}
	}

	allow := append(detect.DefaultAllowlist(), cfg.Allowlist...)

	return detect.New(detect.Config{
		Target:         cfg.Target,
		Detect:         cfg.Detect,
		Dicts:          dicts,
		Allowlist:      allow,
		ExtraStopwords: cfg.ExtraStopwords,
		MinLen:         cfg.MinLen,
		Margin:         cfg.Margin,
	})
}

func splitCSV(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// pathTokens emits findings for camelCase/snake-split tokens of the file
// basename (without extension) and each directory segment of the path.
// Path-token findings carry kind="path" and use a synthetic snippet so the
// reviewer can see which segment fired.
func pathTokens(rel string) []extract.Finding {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	if rel == "" {
		return nil
	}
	parts := strings.Split(rel, "/")
	if len(parts) == 0 {
		return nil
	}
	var out []extract.Finding
	for i, seg := range parts {
		base := seg
		isFile := i == len(parts)-1
		if isFile {
			ext := filepath.Ext(base)
			base = strings.TrimSuffix(base, ext)
		}
		for _, sub := range extract.Split(base) {
			out = append(out, extract.Finding{
				File:    rel,
				Line:    1,
				Col:     1,
				Token:   sub,
				Kind:    "path",
				Snippet: rel,
				Ctx:     "path:" + rel,
			})
		}
	}
	return out
}

// printDryRun groups files by top-level directory and by extension, with sizes.
func printDryRun(repoRoot string, files []string) {
	type dirStat struct {
		count int
		bytes int64
	}
	dirs := map[string]*dirStat{}
	exts := map[string]int{}
	var totalBytes int64
	for _, f := range files {
		rel, _ := filepath.Rel(repoRoot, f)
		rel = filepath.ToSlash(rel)
		parts := strings.SplitN(rel, "/", 2)
		top := parts[0]
		if len(parts) == 1 {
			top = "."
		}
		ds := dirs[top]
		if ds == nil {
			ds = &dirStat{}
			dirs[top] = ds
		}
		ds.count++
		if info, err := os.Stat(f); err == nil {
			ds.bytes += info.Size()
			totalBytes += info.Size()
		}
		exts[strings.ToLower(filepath.Ext(f))]++
	}
	keys := make([]string, 0, len(dirs))
	for k := range dirs {
		keys = append(keys, k)
	}
	sortStrings(keys)
	fmt.Printf("DRY RUN — %d files, %.1f KiB total\n\n", len(files), float64(totalBytes)/1024)
	fmt.Println("by top-level dir:")
	for _, k := range keys {
		ds := dirs[k]
		fmt.Printf("  %-30s %5d files   %7.1f KiB\n", k+"/", ds.count, float64(ds.bytes)/1024)
	}
	fmt.Println("\nby extension:")
	ekeys := make([]string, 0, len(exts))
	for k := range exts {
		ekeys = append(ekeys, k)
	}
	sortStrings(ekeys)
	for _, k := range ekeys {
		fmt.Printf("  %-10s %5d\n", k, exts[k])
	}
}

func sortStrings(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
}

func parseKinds(csv string) map[string]bool {
	csv = strings.TrimSpace(csv)
	if csv == "" || csv == "*" || csv == "all" {
		return nil
	}
	m := map[string]bool{}
	for _, k := range strings.Split(csv, ",") {
		k = strings.TrimSpace(strings.ToLower(k))
		if k == "" {
			continue
		}
		m[k] = true
	}
	return m
}

func parseExt(csv string) map[string]bool {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil
	}
	m := map[string]bool{}
	for _, e := range strings.Split(csv, ",") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		m[strings.ToLower(e)] = true
	}
	return m
}

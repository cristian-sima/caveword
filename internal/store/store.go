package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS findings (
  sig TEXT PRIMARY KEY,
  file TEXT NOT NULL,
  line INTEGER NOT NULL,
  col INTEGER NOT NULL,
  token TEXT NOT NULL,
  kind TEXT NOT NULL,
  snippet TEXT NOT NULL,
  ctx_hash TEXT NOT NULL,
  ro_conf REAL,
  en_conf REAL,
  last_seen TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_findings_file ON findings(file);
CREATE INDEX IF NOT EXISTS idx_findings_token ON findings(token);

CREATE TABLE IF NOT EXISTS verdicts (
  sig TEXT PRIMARY KEY,
  verdict TEXT NOT NULL,
  suggested_en TEXT,
  note TEXT,
  reviewed_at TEXT NOT NULL,
  reviewer TEXT,
  carried_from TEXT
);
CREATE INDEX IF NOT EXISTS idx_verdicts_token ON verdicts(verdict);
`

type Finding struct {
	Sig     string
	File    string
	Line    int
	Col     int
	Token   string
	Kind    string
	Snippet string
	CtxHash string
	// OffConf and TargetConf hold lingua confidence values for the
	// off-target winner and the target language respectively. They are
	// persisted in the legacy SQLite columns ro_conf / en_conf.
	OffConf    float64
	TargetConf float64
}

type Verdict struct {
	Sig          string
	Verdict      string
	SuggestedEn  string
	Note         string
	ReviewedAt   string
	Reviewer     string
	CarriedFrom  string
}

// Open opens (creates) a verdicts.db inside <repoRoot>/.caveword/.
func Open(repoRoot string) (*Store, error) {
	dir := filepath.Join(repoRoot, ".caveword")
	if err := ensureDir(dir); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dir, "verdicts.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Sig is the stable identity of a finding: token + normalized context hash.
// File and line are NOT included to survive moves/edits in unrelated code.
func Sig(token, ctxNormalized string) string {
	h := sha256.Sum256([]byte(token + "\x00" + ctxNormalized))
	return hex.EncodeToString(h[:16])
}

func CtxHash(ctxNormalized string) string {
	h := sha256.Sum256([]byte(ctxNormalized))
	return hex.EncodeToString(h[:8])
}

// UpsertFinding inserts or refreshes a finding.
func (s *Store) UpsertFinding(f Finding) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`
		INSERT INTO findings(sig,file,line,col,token,kind,snippet,ctx_hash,ro_conf,en_conf,last_seen)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(sig) DO UPDATE SET
		  file=excluded.file, line=excluded.line, col=excluded.col,
		  snippet=excluded.snippet, ctx_hash=excluded.ctx_hash,
		  ro_conf=excluded.ro_conf, en_conf=excluded.en_conf,
		  last_seen=excluded.last_seen
	`, f.Sig, f.File, f.Line, f.Col, f.Token, f.Kind, f.Snippet, f.CtxHash, f.OffConf, f.TargetConf, now)
	return err
}

func (s *Store) GetVerdict(sig string) (*Verdict, error) {
	row := s.db.QueryRow(`SELECT sig, verdict, IFNULL(suggested_en,''), IFNULL(note,''), reviewed_at, IFNULL(reviewer,''), IFNULL(carried_from,'') FROM verdicts WHERE sig=?`, sig)
	var v Verdict
	if err := row.Scan(&v.Sig, &v.Verdict, &v.SuggestedEn, &v.Note, &v.ReviewedAt, &v.Reviewer, &v.CarriedFrom); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &v, nil
}

func (s *Store) SaveVerdict(v Verdict) error {
	if v.ReviewedAt == "" {
		v.ReviewedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.db.Exec(`
		INSERT INTO verdicts(sig,verdict,suggested_en,note,reviewed_at,reviewer,carried_from)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(sig) DO UPDATE SET
		  verdict=excluded.verdict, suggested_en=excluded.suggested_en,
		  note=excluded.note, reviewed_at=excluded.reviewed_at,
		  reviewer=excluded.reviewer, carried_from=excluded.carried_from
	`, v.Sig, v.Verdict, v.SuggestedEn, v.Note, v.ReviewedAt, v.Reviewer, v.CarriedFrom)
	return err
}

// FuzzyCarry tries to find an existing verdict for a similar finding in the
// same file (token Levenshtein <= maxDist OR same ctx_hash).
// Returns the carried verdict (with new sig) if found.
func (s *Store) FuzzyCarry(f Finding, maxDist int) (*Verdict, error) {
	rows, err := s.db.Query(`
		SELECT f.sig, f.token, f.ctx_hash, v.verdict, IFNULL(v.suggested_en,''), IFNULL(v.note,''), IFNULL(v.reviewer,'')
		FROM findings f JOIN verdicts v ON f.sig = v.sig
		WHERE f.file = ? AND (f.ctx_hash = ? OR f.token = ?)
	`, f.File, f.CtxHash, f.Token)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var oldSig, oldTok, oldCtx, verdict, sugg, note, reviewer string
		if err := rows.Scan(&oldSig, &oldTok, &oldCtx, &verdict, &sugg, &note, &reviewer); err != nil {
			return nil, err
		}
		if oldCtx == f.CtxHash || levenshtein(oldTok, f.Token) <= maxDist {
			return &Verdict{
				Sig: f.Sig, Verdict: verdict, SuggestedEn: sugg,
				Note: note, Reviewer: reviewer, CarriedFrom: oldSig,
			}, nil
		}
	}
	return nil, nil
}

func (s *Store) ListPending(limit int) ([]Finding, error) {
	rows, err := s.db.Query(`
		SELECT f.sig, f.file, f.line, f.col, f.token, f.kind, f.snippet, f.ctx_hash, IFNULL(f.ro_conf,0), IFNULL(f.en_conf,0)
		FROM findings f LEFT JOIN verdicts v ON f.sig = v.sig
		WHERE v.sig IS NULL
		ORDER BY f.file, f.line LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Finding
	for rows.Next() {
		var f Finding
		if err := rows.Scan(&f.Sig, &f.File, &f.Line, &f.Col, &f.Token, &f.Kind, &f.Snippet, &f.CtxHash, &f.OffConf, &f.TargetConf); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// ListAll returns every finding joined with its verdict (if any).
// Useful for `ro-audit dump` to surface the SQLite store as JSON.
type FindingWithVerdict struct {
	Finding
	Verdict     string
	SuggestedEn string
	Note        string
	Reviewer    string
	ReviewedAt  string
	CarriedFrom string
}

func (s *Store) ListAll() ([]FindingWithVerdict, error) {
	rows, err := s.db.Query(`
		SELECT f.sig, f.file, f.line, f.col, f.token, f.kind, f.snippet, f.ctx_hash,
		       IFNULL(f.ro_conf,0), IFNULL(f.en_conf,0),
		       IFNULL(v.verdict,''), IFNULL(v.suggested_en,''), IFNULL(v.note,''),
		       IFNULL(v.reviewer,''), IFNULL(v.reviewed_at,''), IFNULL(v.carried_from,'')
		FROM findings f LEFT JOIN verdicts v ON f.sig = v.sig
		ORDER BY f.file, f.line`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FindingWithVerdict
	for rows.Next() {
		var fv FindingWithVerdict
		if err := rows.Scan(
			&fv.Sig, &fv.File, &fv.Line, &fv.Col, &fv.Token, &fv.Kind, &fv.Snippet, &fv.CtxHash,
			&fv.OffConf, &fv.TargetConf,
			&fv.Verdict, &fv.SuggestedEn, &fv.Note, &fv.Reviewer, &fv.ReviewedAt, &fv.CarriedFrom,
		); err != nil {
			return nil, err
		}
		out = append(out, fv)
	}
	return out, nil
}

func (s *Store) Counts() (total, reviewed int, err error) {
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM findings`).Scan(&total); err != nil {
		return
	}
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM verdicts`).Scan(&reviewed); err != nil {
		return
	}
	return
}

// PruneStale deletes findings not seen since the cutoff timestamp (RFC3339).
// Verdicts pointing to missing findings are left in place (history).
func (s *Store) PruneStale(cutoff time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM findings WHERE last_seen < ?`, cutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) Exec(query string, args ...any) error {
	_, err := s.db.Exec(query, args...)
	return err
}

func ensureDir(p string) error {
	return mkdirAll(p)
}

// debug helper, exported for ad-hoc queries from cmd
func (s *Store) DB() *sql.DB { return s.db }

// fmt helper to avoid importing fmt in callers when constructing errors.
func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}

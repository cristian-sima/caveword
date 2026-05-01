<p align="center">
  <img src="assets/logo.svg" width="360" alt="caveword" />
</p>

<p align="center">
  <em>Caveman speak only one tongue. Caveword make code do same.</em>
</p>

<p align="center">
  Static auditor that finds identifiers, file paths, and comments written
  in the wrong language &mdash; while leaving SQL, JSON, and other
  user-facing strings untouched.
</p>

---

## Example 1 &mdash; mixed French/English code &rarr; English only

A common drift: some identifiers are written in the developer's native
language while the rest of the codebase is in English.

**Before** &mdash; `caveword scan` flags `calculerSolde`, `estDébité`,
`montant`, `utilisateur`, `nom`, and `ageInAnnées`:

```js
function calculerSolde(transactions) {
  let total = 0;
  for (const tx of transactions) {
    if (tx.estDébité) {
      total -= tx.montant;
    } else {
      total += tx.montant;
    }
  }
  return total;
}

const utilisateur = {
  nom: "Alice",
  ageInAnnées: 30,
};
```

**After review** &mdash; verdicts applied, identifiers renamed:

```js
function calculateBalance(transactions) {
  let total = 0;
  for (const tx of transactions) {
    if (tx.isDebited) {
      total -= tx.amount;
    } else {
      total += tx.amount;
    }
  }
  return total;
}

const user = {
  name: "Alice",
  ageInYears: 30,
};
```

The `caveword apply` step records each finding's verdict
(`ro_confirmed`, `suggested_en` filled in) so a later rescan does not
reopen the same review.

## Example 2 &mdash; SQL, JSON, and user-facing strings stay put

Many projects intentionally keep some non-English text: database column
names that mirror a third-party schema, JSON tags that match an external
contract, UI strings shown to end users. Caveword does not touch any of
those &mdash; it masks string literals before scanning, and the verdict
store records `domain_ok` for tokens that genuinely belong as-is.

```go
// Account mirrors the SAGA chart-of-accounts row.
type Account struct {
    Name string `json:"denumire"` // <-- JSON tag is a string, not flagged
    Code string `json:"cont"`     //     same here
}

func loadChartOfAccounts(db *sql.DB) ([]Account, error) {
    rows, err := db.Query(`
        SELECT cont, denumire
        FROM   CONTURI
        WHERE  rang = 'D'
    `) // <-- SQL is a string literal, not flagged
    ...
}
```

```jsx
// React component &mdash; the JSX text is the user-facing label.
function AccountList({ items }) {
  return (
    <div>
      <h3>Lista conturilor</h3>{/* <-- JSX text, not flagged */}
      {items.map(a =>
        <div key={a.code}>{a.code} &mdash; {a.name}</div>
      )}
    </div>
  );
}
```

For these snippets `caveword scan` reports **zero findings**: the Go
field names and the function name are English, every Romanian token sits
inside a masked string or JSX text node.

A finding can also be marked `domain_ok` per occurrence, so when an
identifier really does have to stay in the source language (a SQL column
mapped 1:1 to a Go struct field, an external XML schema element name, a
test that exercises a literal third-party label) you record that decision
once and re-scans treat it as accepted.

---

## What it does

Caveword walks a repository, splits every identifier on camelCase /
snake\_case / kebab-case, masks string literals and (optionally) comment
text, and asks a language classifier whether the surviving tokens look
like the project's chosen target language. Anything that does not is
written to a per-repo SQLite store as a "finding" with a stable signature
(token + normalized surrounding context).

You then triage findings into one of five verdicts:

| Verdict | Meaning |
|---------|---------|
| `ro_confirmed` | Genuinely off-language; rename. Fill `suggested_en`. |
| `en_actually` | False positive (split residue, abbreviation, etc.). |
| `proper_noun` | Person, brand, library &mdash; leave alone. |
| `domain_ok` | Off-language but accepted (DB column, third-party schema). |
| `ambiguous` | Cannot decide from this snippet. |

Verdicts are keyed by the finding's signature, so a rescan after
unrelated edits carries verdicts forward automatically. Renames or
small context shifts within the same file fall through to a fuzzy carry
(token Levenshtein &le; 2 or identical normalized-context hash) so you
do not re-review a finding just because a function moved.

The default classification model is **Romanian vs English** &mdash; that
is the case the tool was built for. The architecture is language-agnostic
(two embedded dictionaries plus a Lingua model), so adding French,
German, or any other pair is a small change in `internal/detect`.

## Languages parsed

| Ext | Extractor |
|-----|-----------|
| `.go` | `go/parser` (full AST) |
| `.ts .tsx .js .jsx .mjs .cjs` | regex + JSX-text masking on tsx/jsx |
| `.py` | regex + triple-string handling |
| `.rb` | regex |
| `.java .c .cc .cpp .h .hpp .cs .rs .swift .kt .scala .php` | C-family regex |

Identifiers, comments, **and path tokens** (directory names, file
basenames) are all classified. Strings are masked by language-aware
regular expressions (Go uses the standard library AST, which handles
strings natively).

## Install

```bash
go install github.com/cristian-sima/caveword/cmd/caveword@latest
```

Pure-Go dependencies only &mdash; no cgo, no native libraries to ship.

## Usage

```text
caveword scan   [--repo PATH] [--diff BASE] [--ext .go,.ts,...] [--kinds ident,path] [--dry-run] [-v]
caveword export [--repo PATH] [--limit N] [-o FILE]
caveword apply  [--repo PATH] [-i FILE] [--reviewer NAME]
caveword status [--repo PATH]
caveword list   [--repo PATH] [--pending] [--limit N]
caveword dump   [--repo PATH] [--only reviewed|pending|all] [-o FILE]
```

State lives in `<repo>/.caveword/verdicts.db` (SQLite). Add `.caveword/`
to `.gitignore` &mdash; the verdicts are per-developer state, not source
to commit.

### A typical first pass

```bash
cd my-project
caveword scan --repo . --dry-run     # confirm scan scope (no node_modules etc.)
caveword scan --repo . -v
caveword status --repo .
caveword export --repo . --limit 50 -o batch.json
# review batch.json &mdash; fill in verdict + suggested_en + note
caveword apply --repo . -i batch-reviewed.json --reviewer human
```

### As a PR gate

```bash
caveword scan --repo . --diff main
caveword export --repo . --limit 100 -o batch.json
```

`--diff <base>` only re-scans files changed against the given branch, so
running this in CI on each PR is cheap.

### Filtering kinds

Findings carry a `kind` field: `ident`, `comment`, or `path`. Comments
generate a lot of noise in repos that intentionally mix languages
(English code, native-language documentation), so the default scan keeps
only `ident,path`. To include comments:

```bash
caveword scan --repo . --kinds ident,comment,path
```

## How findings stay stable

A finding's signature is `sha256(token + normalized_context)` where
`normalized_context` is the surrounding &plusmn;2 lines with whitespace
stripped. Adding a function above, renaming an unrelated variable, or
reformatting blank lines does not change the signature, so verdicts
carry over.

When the signature does change (the token itself was renamed, or the
two-line neighbourhood was rewritten), the store falls back to a fuzzy
match scoped to the same file: identical context hash *or* token
Levenshtein &le; 2 inherits the prior verdict and records the
`carried_from` link.

## What is filtered out before classification

- Hidden directories, `node_modules`, `vendor`, `dist`, `build`, `target`,
  `.next`, framework caches, `wailsjs`, `public`, `static`, `assets`,
  `testdata`, &hellip;
- Generated / lock files: `package-lock.json`, `go.sum`, `*.min.js`,
  `*.bundle.js`, `*.map`, `*.pb.go`, `*.gen.go`, `*.lock`, &hellip;
- Files larger than 512 KiB.
- Words present in the embedded English dictionary (`dwyl/english-words`
  &mdash; ~370k entries).
- A small list of stop-words (acronyms, tech tokens like `stderr`,
  `sqlite`, `regex`, &hellip;).
- A tiny project allowlist for product / brand short codes.

The allowlist is intentionally short. Domain-specific tokens are
recorded as `domain_ok` per finding, not blanket-suppressed &mdash;
that way, a real off-language identifier that happens to share a name
with a domain term still surfaces.

## Layout

```
caveword/
  cmd/caveword/main.go       # CLI: scan / export / apply / status / list / dump
  internal/
    extract/                 # parsers, tokenizer, ignore filter
    detect/                  # lingua-go + English wordlist + allowlist
    store/                   # SQLite, sig, fuzzy-carry
    diff/                    # git ls-files + git diff modes
    review/                  # JSON export / apply
  assets/logo.svg
```

## Stack

- Go 1.22
- [`github.com/pemistahl/lingua-go`](https://github.com/pemistahl/lingua-go) &mdash; language classifier
- [`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite) &mdash; pure-Go SQLite (no cgo, Windows-friendly)
- [`dwyl/english-words`](https://github.com/dwyl/english-words) `words_alpha.txt` &mdash; embedded via `go:embed`

## License

MIT &mdash; see [LICENSE](LICENSE).

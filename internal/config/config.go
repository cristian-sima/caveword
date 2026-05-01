package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the on-disk shape of a per-repo caveword configuration. Saved as
// JSON at <repo>/.caveword/config.json. Every field is optional; missing keys
// fall back to library defaults.
type Config struct {
	// Target is the ISO-639-1 code identifiers SHOULD be in.
	Target string `json:"target,omitempty"`
	// Detect lists the off-target languages to score against.
	Detect []string `json:"detect,omitempty"`
	// Margin is how far the off-target language must beat the target by
	// before a token is flagged. 0 keeps the library default (0.30).
	Margin float64 `json:"margin,omitempty"`
	// MinLen is the minimum token length to classify with lingua.
	// 0 keeps the library default (4).
	MinLen int `json:"min_token_len,omitempty"`
	// Allowlist extends the library's allowlist with project-domain short
	// codes (brands, acronyms) that should never be flagged.
	Allowlist []string `json:"allowlist,omitempty"`
	// ExtraStopwords extends the library's stop-list (technical tokens
	// that aren't real words but score high on language models).
	ExtraStopwords []string `json:"stopwords,omitempty"`
}

const fileName = "config.json"

// Path returns the canonical config path inside <repo>/.caveword/.
func Path(repoRoot string) string {
	return filepath.Join(repoRoot, ".caveword", fileName)
}

// Load reads the config from <repo>/.caveword/config.json. Returns a zero
// Config (no error) if the file does not exist; that's a valid state.
func Load(repoRoot string) (Config, error) {
	var c Config
	p := Path(repoRoot)
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c, nil
		}
		return c, fmt.Errorf("read %s: %w", p, err)
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("parse %s: %w", p, err)
	}
	return c, nil
}

// Save writes c to <repo>/.caveword/config.json (creating parent dirs).
func Save(repoRoot string, c Config) error {
	p := Path(repoRoot)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

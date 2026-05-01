package diff

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
)

// TrackedFiles returns repo-relative paths of all tracked files (honors .gitignore).
func TrackedFiles(repoRoot string) ([]string, error) {
	out, err := run("git", "-C", repoRoot, "ls-files")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		paths = append(paths, filepath.Join(repoRoot, l))
	}
	return paths, nil
}

// ChangedFiles returns repo-relative paths changed between base..HEAD plus
// the working-tree changes. Empty base means scan everything (caller decides).
func ChangedFiles(repoRoot, base string) ([]string, error) {
	args := []string{"-C", repoRoot, "diff", "--name-only", "--diff-filter=ACMR"}
	if base != "" {
		args = append(args, base+"...HEAD")
	}
	out, err := run("git", args...)
	if err != nil {
		return nil, err
	}
	wt, err := run("git", "-C", repoRoot, "diff", "--name-only", "--diff-filter=ACMR", "HEAD")
	if err != nil {
		return nil, err
	}
	untracked, err := run("git", "-C", repoRoot, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	var paths []string
	for _, blob := range []string{out, wt, untracked} {
		for _, l := range strings.Split(strings.TrimSpace(blob), "\n") {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			if _, ok := seen[l]; ok {
				continue
			}
			seen[l] = struct{}{}
			paths = append(paths, filepath.Join(repoRoot, l))
		}
	}
	return paths, nil
}

func run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

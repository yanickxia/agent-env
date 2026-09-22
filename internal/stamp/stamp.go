// Package stamp implements the --skip-unchanged state file (state.tsv) and
// the per-entry signature. The on-disk format and the signature payload are
// byte-compatible with the zsh agent-skills-sync implementation, so existing
// state.tsv rows keep matching.
package stamp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yanickxia/agent-env/internal/config"
)

// Store is a state.tsv file.
type Store struct {
	Path string
}

// Signature computes the sha256 fingerprint over the entry's declared shape.
// The optional profiles/repoRoot pair is only non-empty for project scope,
// where stamps are per-repo and must include the effective profile selection.
func Signature(source, agents, skills, mode, scope, installer, envPayload, profiles, repoRoot string) string {
	payload := strings.Join([]string{source, agents, skills, mode, scope, installer, envPayload}, "|")
	if repoRoot != "" {
		payload += "|" + profiles + "|" + repoRoot
	}
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// Row is one (source, scope) pair recorded in the state file. It deliberately
// drops the signature: prune only needs the ownership record.
type Row struct {
	Source string
	Scope  string
}

// Lookup returns the stored signature for (source, scope), or ok=false when
// absent or the state file does not exist.
func (s Store) Lookup(source, scope string) (string, bool) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) >= 3 && parts[0] == source && parts[1] == scope {
			return parts[2], true
		}
	}
	return "", false
}

// List returns every (source, scope) row whose scope equals scopePrefix or
// starts with scopePrefix+"/", in file order. A missing file yields no rows.
// The delimiter-aware prefix lets callers select a scope family (for example
// "project:" selects "project:/repo/a") without letting "project:/repo" also
// match the unrelated "project:/repo2".
func (s Store) List(scopePrefix string) []Row {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return nil
	}
	family := scopePrefix + "/"
	var rows []Row
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) >= 3 && (parts[1] == scopePrefix || strings.HasPrefix(parts[1], family)) {
			rows = append(rows, Row{Source: parts[0], Scope: parts[1]})
		}
	}
	return rows
}

// Delete removes every row whose exact (source, scope) is listed, leaving all
// other bytes untouched (rows are cut at line boundaries, including their
// trailing newline). A missing file is a no-op. Deleting nothing is a no-op.
func (s Store) Delete(rows []Row) error {
	if len(rows) == 0 {
		return nil
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("%s: cannot read state file %s: %v", config.Prog, s.Path, err)
	}

	drop := make(map[string]bool, len(rows))
	for _, r := range rows {
		drop[r.Source+"\t"+r.Scope] = true
	}

	var b strings.Builder
	rest := string(data)
	for len(rest) > 0 {
		idx := strings.IndexByte(rest, '\n')
		var line, tail string
		if idx < 0 {
			line, rest, tail = rest, "", ""
		} else {
			line, rest, tail = rest[:idx], rest[idx+1:], "\n"
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= 2 && drop[parts[0]+"\t"+parts[1]] {
			continue
		}
		b.WriteString(line)
		b.WriteString(tail)
	}
	return writeAtomic(s.Path, b.String())
}

// Write atomically upserts one (source, scope) -> signature row, preserving
// every other line exactly as the zsh awk-based implementation did.
func (s Store) Write(source, scope, sig string) error {
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("%s: cannot create state directory %s: %v", config.Prog, dir, err)
	}

	existing, err := os.ReadFile(s.Path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s: cannot read state file %s: %v", config.Prog, s.Path, err)
	}

	var b strings.Builder
	if len(existing) > 0 {
		text := string(existing)
		for len(text) > 0 {
			idx := strings.IndexByte(text, '\n')
			var line string
			if idx < 0 {
				line = text
				text = ""
			} else {
				line = text[:idx]
				text = text[idx+1:]
			}
			parts := strings.Split(line, "\t")
			if len(parts) >= 2 && parts[0] == source && parts[1] == scope {
				continue
			}
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	b.WriteString(source)
	b.WriteByte('\t')
	b.WriteString(scope)
	b.WriteByte('\t')
	b.WriteString(sig)
	b.WriteByte('\n')

	return writeAtomic(s.Path, b.String())
}

// writeAtomic replaces path with content via a same-directory temp file and
// rename, creating the parent directory when needed.
func writeAtomic(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("%s: cannot create state directory %s: %v", config.Prog, dir, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("%s: cannot create temp state file: %v", config.Prog, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("%s: cannot write temp state file: %v", config.Prog, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("%s: cannot write temp state file: %v", config.Prog, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("%s: cannot replace state file %s: %v", config.Prog, path, err)
	}
	return nil
}

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

	tmp, err := os.CreateTemp(dir, filepath.Base(s.Path)+".*")
	if err != nil {
		return fmt.Errorf("%s: cannot create temp state file: %v", config.Prog, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(b.String()); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("%s: cannot write temp state file: %v", config.Prog, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("%s: cannot write temp state file: %v", config.Prog, err)
	}
	if err := os.Rename(tmpName, s.Path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("%s: cannot replace state file %s: %v", config.Prog, s.Path, err)
	}
	return nil
}

package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SkillLock is the read-only view of the skills CLI lock file. agent-env never
// writes it; it only reverse-maps a source to the skills it installed so prune
// and ls know which directories belong to a source. Entries carry both the
// normalized `source` and the original `sourceUrl` (the skills CLI normalizes
// `owner/repo` out of a URL), so matching consults both.
type SkillLock struct {
	Version int                    `json:"version"`
	Skills  map[string]skillRecord `json:"skills"`
}

type skillRecord struct {
	Source    string `json:"source"`
	SourceURL string `json:"sourceUrl"`
}

// LockPath returns the skills CLI lock path for a scope: the repo lock lives
// at the repo root (next to .agents/), the global lock under ~/.agents/.
func LockPath(home, projectRoot string, global bool) string {
	if global {
		return filepath.Join(home, ".agents", ".skill-lock.json")
	}
	return filepath.Join(projectRoot, "skills-lock.json")
}

// ReadLock loads a lock file, returning nil when it is absent or unreadable.
// A malformed lock is treated like a missing one: callers must never act on a
// file they could not understand.
func ReadLock(path string) *SkillLock {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var l SkillLock
	if err := json.Unmarshal(data, &l); err != nil || l.Skills == nil {
		return nil
	}
	return &l
}

// SkillsFor returns the sorted skill names the lock associates with declared
// (matching either the normalized source or sourceUrl), or nil when the lock
// has no record for it.
func (l *SkillLock) SkillsFor(declared string) []string {
	if l == nil {
		return nil
	}
	var names []string
	for name, rec := range l.Skills {
		if sourceMatches(declared, rec.Source) || sourceMatches(declared, rec.SourceURL) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// sourceMatches reports whether a manifest source and a lock source (or URL)
// denote the same upstream. Declared sources may be URLs while the lock stores
// a normalized owner/repo, so a canonical-owner/repo comparison backs up the
// exact match.
func sourceMatches(declared, lockSource string) bool {
	declared = strings.TrimSpace(declared)
	lockSource = strings.TrimSpace(lockSource)
	if declared == "" || lockSource == "" {
		return false
	}
	if declared == lockSource {
		return true
	}
	return canonSource(declared) == canonSource(lockSource)
}

// canonSource reduces a source to a comparable form: scp-style git hosts keep
// their host (git@host:owner/repo), while URL and bare forms collapse to
// owner/repo. A trailing .git is dropped either way.
func canonSource(s string) string {
	s = strings.TrimSuffix(strings.TrimSpace(s), ".git")
	if strings.HasPrefix(s, "git@") {
		return strings.ToLower(s)
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
		if j := strings.IndexByte(s, '/'); j >= 0 {
			s = s[j+1:]
		}
	}
	return strings.ToLower(s)
}

func ownerRepo(s string) string {
	parts := strings.Split(s, "/")
	if len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return s
}

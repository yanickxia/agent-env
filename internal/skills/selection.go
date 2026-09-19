package skills

import (
	"strings"
)

// splitTrimNonEmpty splits a comma-joined field, trimming each token and
// dropping empty ones — the shape the zsh loops operate on.
func splitTrimNonEmpty(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// profilesIntersect reports whether any profile in the comma-joined left also
// appears in the comma-joined right.
func profilesIntersect(left, right string) bool {
	rightSet := map[string]bool{}
	for _, tok := range strings.Split(right, ",") {
		tok = strings.TrimSpace(tok)
		if tok != "" {
			rightSet[tok] = true
		}
	}
	for _, item := range strings.Split(left, ",") {
		item = strings.TrimSpace(item)
		if item != "" && rightSet[item] {
			return true
		}
	}
	return false
}

// selectEntryAgents applies the CLI --agent filter first, then the repo config
// narrows repo-level (non-global) entries only. It returns the final
// comma-joined agent list and ok=false when the entry should be skipped.
func (r *runner) selectEntryAgents(global bool, agentsRaw string) (string, bool) {
	if strings.TrimSpace(agentsRaw) == "" {
		if len(r.f.Agents) > 0 {
			return "", false
		}
		return "", true
	}

	agents := splitTrimNonEmpty(agentsRaw)

	if len(r.f.Agents) > 0 {
		kept := []string{}
		for _, item := range agents {
			for _, f := range r.f.Agents {
				if item == f {
					kept = append(kept, item)
					break
				}
			}
		}
		if len(kept) == 0 {
			return "", false
		}
		agents = kept
	}

	if !global && r.repo != nil && len(r.repo.Agents) > 0 {
		repoSet := map[string]bool{}
		for _, tok := range r.repo.Agents {
			repoSet[tok] = true
		}
		kept := []string{}
		for _, item := range agents {
			if repoSet[item] {
				kept = append(kept, item)
			}
		}
		if len(kept) == 0 {
			return "", false
		}
		agents = kept
	}

	return strings.Join(agents, ","), true
}

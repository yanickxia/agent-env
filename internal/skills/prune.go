package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yanickxia/agent-env/internal/config"
	"github.com/yanickxia/agent-env/internal/stamp"
)

// pruneTarget describes the scope a prune run owns: which stamp scope to
// enumerate, which skills CLI lock reverses the source→skill mapping, and the
// store directory whose skill subdirectories are candidates for removal.
type pruneTarget struct {
	global      bool
	stampScope  string
	lockPath    string
	skillsDir   string
	claudeLinks string // per-skill claude symlink dir (repo runs only; "" otherwise)
}

// pruneTargetFor derives the prune target from the current context, aligning
// with --force's scope awareness: a repo run owns repo-level (project) installs,
// a --no-repo run owns global (user) installs.
func (r *runner) pruneTargetFor() pruneTarget {
	if r.f.NoRepo {
		return pruneTarget{
			global:     true,
			stampScope: "user",
			lockPath:   lockPath(r.opts.Home, r.opts.ProjectRoot, true),
			skillsDir:  filepath.Join(r.opts.Home, ".agents", "skills"),
		}
	}
	return pruneTarget{
		global:      false,
		stampScope:  "project:" + r.opts.ProjectRoot,
		lockPath:    lockPath(r.opts.Home, r.opts.ProjectRoot, false),
		skillsDir:   filepath.Join(r.opts.ProjectRoot, ".agents", "skills"),
		claudeLinks: filepath.Join(r.opts.ProjectRoot, ".claude", "skills"),
	}
}

// activeEntry is a manifest source the current run considers active, together
// with the skills it declares. wildcard marks `skills = ["*"]`, whose concrete
// names are only knowable through the lock.
type activeEntry struct {
	source   string
	skills   []string
	wildcard bool
}

// pruneActiveEntries returns the manifest entries the current run considers
// active for the target scope. It deliberately ignores the CLI --agent/--skill
// narrowing filters: those only restrict what gets (re)installed, they must
// never make a declared entry look retired and cause its store to be deleted.
// Global entries are always active in a global run; repo-level entries are
// gated by the effective profiles/names and the repo config's agents.
func (r *runner) pruneActiveEntries(globalScope bool) []activeEntry {
	entries, err := config.ParseManifest(r.opts.ManifestPath)
	if err != nil {
		return nil
	}
	active := []activeEntry{}
	for _, entry := range entries {
		if entry.Global != globalScope {
			continue
		}
		if !entry.Global {
			named := entry.Name != "" && stringInList(r.effectiveNames, entry.Name)
			profiled := len(entry.Profiles) > 0 && profilesIntersect(entry.ProfilesRaw, r.effectiveProf)
			if !named && !profiled {
				continue
			}
			// Repo config agents narrow repo-level entries independently of
			// the CLI --agent filter.
			if r.repo != nil && len(r.repo.Agents) > 0 && !agentsIntersect(entry.AgentsRaw, r.repo.Agents) {
				continue
			}
		}
		source := config.ExpandSource(r.opts.Home, strings.TrimSpace(entry.Source))
		names := splitTrimNonEmpty(entry.SkillsRaw)
		wildcard := false
		for _, n := range names {
			if n == "*" {
				wildcard = true
				break
			}
		}
		active = append(active, activeEntry{source: source, skills: names, wildcard: wildcard})
	}
	sort.Slice(active, func(i, j int) bool { return active[i].source < active[j].source })
	return active
}

// pruneOwners maps each skill name an active entry supplies to that entry's
// source. Declared names are authoritative; a `skills = ["*"]` entry falls back
// to the lock, which is the only place its concrete names are recorded.
func pruneOwners(entries []activeEntry, lock *skillLock) map[string][]string {
	owners := map[string][]string{}
	for _, e := range entries {
		names := e.skills
		if e.wildcard {
			names = lock.skillsFor(e.source)
		}
		for _, n := range names {
			if n == "*" {
				continue
			}
			owners[n] = appendUnique(owners[n], e.source)
		}
	}
	return owners
}

func agentsIntersect(raw string, repoAgents []string) bool {
	agents := splitTrimNonEmpty(raw)
	if len(agents) == 0 {
		// No declared agents: the entry installs for the manifest defaults, so
		// the repo agent narrowing leaves it untouched (mirrors selection.go).
		return true
	}
	for _, a := range agents {
		if stringInList(repoAgents, a) {
			return true
		}
	}
	return false
}

// prune removes store directories and stamps for sources that agent-env
// installed but that are no longer active in this context. It never touches
// installs that carry no stamp (manual `npx skills add` runs). In dry-run mode
// it only reports the plan and changes nothing.
func (r *runner) prune(mode string) error {
	target := r.pruneTargetFor()
	store := stamp.Store{Path: r.opts.StatePath}
	rows := store.List(target.stampScope)
	if len(rows) == 0 {
		fmt.Fprintf(r.out, "nothing to prune (%s)\n", target.stampScope)
		return nil
	}

	entries := r.pruneActiveEntries(target.global)
	activeSet := make(map[string]bool, len(entries))
	for _, e := range entries {
		activeSet[e.source] = true
	}

	lock := readLock(target.lockPath)
	owners := pruneOwners(entries, lock)

	dryRun := mode == CmdDryRun
	var stale []stamp.Row
	removedPaths := 0
	for _, row := range rows {
		if activeSet[row.Source] {
			continue
		}
		stale = append(stale, row)

		skills := lock.skillsFor(row.Source)
		if len(skills) == 0 {
			verb := "pruned"
			if dryRun {
				verb = "would prune"
			}
			fmt.Fprintf(r.out, "%s: %s (stamp only; no lock record)\n", verb, row.Source)
			continue
		}
		for _, skill := range skills {
			if len(owners[skill]) > 0 {
				fmt.Fprintf(r.out, "keep (shared with active source): %s\n", skill)
				continue
			}
			dir := filepath.Join(target.skillsDir, skill)
			verb := "pruned"
			if dryRun {
				verb = "would prune"
			}
			fmt.Fprintf(r.out, "%s: %s (source %s) -> %s\n", verb, skill, row.Source, dir)
			removedPaths++
			if dryRun {
				continue
			}
			if err := os.RemoveAll(dir); err != nil {
				fmt.Fprintf(r.errw, "%s: cannot remove %s: %v\n", config.Prog, dir, err)
				continue
			}
			r.removeClaudeLink(target.claudeLinks, skill)
		}
	}

	if len(stale) == 0 {
		fmt.Fprintf(r.out, "nothing to prune (%s)\n", target.stampScope)
		return nil
	}
	if dryRun {
		fmt.Fprintf(r.out, "would prune %d path(s), %d stamp(s) [%s]\n", removedPaths, len(stale), target.stampScope)
		return nil
	}

	if err := store.Delete(stale); err != nil {
		return err
	}
	fmt.Fprintf(r.out, "pruned %d path(s), %d stamp(s) [%s]\n", removedPaths, len(stale), target.stampScope)
	return nil
}

// removeClaudeLink deletes <claudeDir>/<skill> only when it is a symlink that
// points at the pruned store directory. Real directories and unrelated links
// are left alone.
func (r *runner) removeClaudeLink(claudeDir, skill string) {
	if claudeDir == "" {
		return
	}
	link := filepath.Join(claudeDir, skill)
	fi, err := os.Lstat(link)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return
	}
	dest, err := os.Readlink(link)
	if err != nil {
		return
	}
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(claudeDir, dest)
	}
	if filepath.Base(filepath.Clean(dest)) != skill {
		return
	}
	if err := os.Remove(link); err != nil {
		fmt.Fprintf(r.errw, "%s: cannot remove symlink %s: %v\n", config.Prog, link, err)
	}
}

package inventory

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/yanickxia/agent-env/internal/config"
	"github.com/yanickxia/agent-env/internal/skills"
)

// Options mirrors the resolved-path style of the skills/mcp Options: the
// caller (internal/cli) resolves every path from the environment.
type Options struct {
	ManifestPath   string
	RepoConfigPath string
	ProjectRoot    string
	StartDir       string
	Home           string

	Stdout io.Writer
	Stderr io.Writer

	LookupEnv func(string) string
}

// Filters is the ls CLI surface.
type Filters struct {
	Providers []string // --provider, canonicalized; empty = all
	Declared  bool     // --declared: manifest view (uninstalled entries count)
	NoRepo    bool     // --no-repo: repo view shows nothing
}

// Run executes `agent-env ls`. Errors are already user-facing.
func Run(opts Options, f Filters) error {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.LookupEnv == nil {
		opts.LookupEnv = os.Getenv
	}

	// Validate --provider before anything else so a typo never produces an
	// empty-looking output.
	providers := f.Providers
	if len(providers) == 0 {
		providers = ProviderNames()
	} else {
		seen := map[string]bool{}
		for _, p := range providers {
			if !ValidProvider(p) {
				return fmt.Errorf("%s: unknown provider '%s' (expected codex, claude-code, opencode)", config.Prog, p)
			}
		}
		providers = providers[:0:0]
		for _, p := range f.Providers {
			canon := CanonicalProvider(p)
			if !seen[canon] {
				seen[canon] = true
				providers = append(providers, canon)
			}
		}
		// Keep the canonical display order regardless of flag order.
		ordered := []string{}
		for _, p := range ProviderNames() {
			if seen[p] {
				ordered = append(ordered, p)
			}
		}
		providers = ordered
	}

	repo, layers, hasRepo, err := loadRepoSelection(opts, f.NoRepo)
	if err != nil {
		return err
	}
	eff, effNames := effectiveSelection(repo, hasRepo)

	entries, err := config.ParseManifest(opts.ManifestPath)
	if err != nil {
		return err
	}
	manifestServers, err := manifestServerEntries(opts)
	if err != nil {
		return err
	}
	r := &lsRunner{
		opts:     opts,
		entries:  entries,
		servers:  manifestServers,
		repo:     repo,
		layers:   layers,
		hasRepo:  hasRepo,
		eff:      eff,
		effNames: effNames,
		declared: f.Declared,
	}
	if f.Declared {
		r.lockGlobal = skills.ReadLock(skills.LockPath(opts.Home, opts.ProjectRoot, true))
		if !f.NoRepo {
			r.lockProject = skills.ReadLock(skills.LockPath(opts.Home, opts.ProjectRoot, false))
		}
	}

	for _, p := range providers {
		if !f.NoRepo {
			r.printProviderSection(p, false)
		}
		r.printProviderSection(p, true)
	}
	return nil
}

// effectiveProfiles mirrors skills.computeEffectiveProfiles: repo profiles
// (or --profile-less emptiness), deduped and byte-sorted.
func effectiveSelection(repo *config.RepoConfig, hasRepo bool) (profiles string, names []string) {
	if repo == nil {
		return "", nil
	}
	unique := []string{}
	for _, item := range repo.Profiles {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !contains(unique, item) {
			unique = append(unique, item)
		}
	}
	sort.Strings(unique)
	return strings.Join(unique, ","), repo.Names
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// manifestServerEntries parses [[servers]] with a no-secrets resolver so ls
// never fails on an unresolved ${VAR} and never needs secrets.toml. Unlike
// the mcp domain, a config without a [[servers]] table is fine: ls is a
// cross-domain view, a skills-only config must still list.
func manifestServerEntries(opts Options) ([]config.Server, error) {
	servers, err := config.ParseServers(opts.ManifestPath, func(string) string { return "" })
	if err != nil {
		if strings.Contains(err.Error(), "must contain a \"servers\" array") {
			return nil, nil
		}
		return nil, err
	}
	return servers, nil
}

// loadRepoSelection discovers and merges .agent-env.toml layers, mirroring
// skills.loadRepo. --no-repo skips the walk entirely.
func loadRepoSelection(opts Options, noRepo bool) (*config.RepoConfig, []string, bool, error) {
	if noRepo {
		return nil, nil, false, nil
	}
	start := opts.StartDir
	if start == "" {
		start = opts.ProjectRoot
	}
	explicit := opts.RepoConfigPath
	if explicit == config.DefaultRepoConfigPath(opts.ProjectRoot) {
		explicit = ""
	}
	return config.LoadRepoSelection(
		explicit, start, opts.LookupEnv,
		"install details and hooks belong in the global manifest")
}

// --- per-provider rendering --------------------------------------------------

type lsRunner struct {
	opts     Options
	entries  []config.Install
	servers  []config.Server
	repo     *config.RepoConfig
	layers   []string
	hasRepo  bool
	eff      string
	effNames []string

	declared    bool
	lockGlobal  *skills.SkillLock
	lockProject *skills.SkillLock

	symlinkNoticeShown bool
}

// printProviderSection prints one provider's section for one scope. Sections
// with no installed and no declared entries are omitted entirely so the
// default output stays compact.
func (r *lsRunner) printProviderSection(provider string, global bool) {
	scope := "repo"
	target := r.opts.ProjectRoot
	if global {
		scope = "global"
		target = r.opts.Home
	}

	declaredSkills, declaredServers := r.declaredFor(provider, global)
	installedSkills := listSkillsDir(skillDir(r.opts.Home, r.opts.ProjectRoot, provider, global))
	installedMCP, _ := mcpKeyOf(r.opts.Home, r.opts.ProjectRoot, provider, global)

	if len(declaredSkills) == 0 && len(declaredServers) == 0 &&
		len(installedSkills) == 0 && len(installedMCP) == 0 {
		return
	}

	out := r.opts.Stdout
	fmt.Fprintf(out, "%s (%s):\n", providerLabel(provider), scope)

	if global && provider == "claude-code" && r.claudeSkillsIsSymlink() {
		if !r.symlinkNoticeShown {
			fmt.Fprintf(r.opts.Stderr, "%s: ~/.claude/skills is a symlink to the skills store; claude sees the store directly\n", config.Prog)
			r.symlinkNoticeShown = true
		}
	}

	// skills: installed set annotated by declared state, then (declared but)
	// not-installed ones when --declared.
	skillRows := r.mergeSkillRows(provider, global, installedSkills, declaredSkills)
	if len(skillRows) > 0 {
		fmt.Fprintln(out, "  skills:")
		for _, row := range skillRows {
			fmt.Fprintf(out, "    %s%s\n", row.name, row.suffix)
		}
	}

	mcpRows := r.mergeServerRows(installedMCP, declaredServers)
	if len(mcpRows) > 0 {
		fmt.Fprintln(out, "  mcp:")
		for _, row := range mcpRows {
			fmt.Fprintf(out, "    %s%s\n", row.name, row.suffix)
		}
	}
	_ = target
}

type nameRow struct {
	name   string
	order  int // installed first, then declared-only; alphabetical inside groups
	suffix string
}

// mergeSkillRows merges the installed and declared skill sets. A skill shows
// "(not installed)" only under --declared; without it the installed set is
// the whole view. Entries gated out of the current context (skipped by
// apply) are never listed.
func (r *lsRunner) mergeSkillRows(provider string, global bool, installed []string, declared []skillDecl) []nameRow {
	installedSet := map[string]bool{}
	for _, name := range installed {
		installedSet[name] = true
	}
	declaredSet := map[string]bool{}
	wildcardSet := map[string]bool{}
	for _, d := range declared {
		declaredSet[d.name] = true
		if d.wildcard {
			wildcardSet[d.name] = true
		}
	}

	rows := []nameRow{}
	if r.declared {
		// Union: installed first (marked with their declared state), then
		// declared-only (marked not installed).
		names := append([]string(nil), installed...)
		sort.Strings(names)
		for _, name := range names {
			row := nameRow{name: name, order: 0, suffix: ""}
			switch {
			case !declaredSet[name]:
				row.suffix = "  (no active declaration)"
			case wildcardSet[name]:
				row.suffix = "  (wildcard source)"
			}
			rows = append(rows, row)
		}
		extra := []string{}
		for name := range declaredSet {
			if !installedSet[name] {
				extra = append(extra, name)
			}
		}
		sort.Strings(extra)
		for _, name := range extra {
			rows = append(rows, nameRow{name: name, order: 1, suffix: "  (not installed)"})
		}
		return rows
	}

	for _, name := range installed {
		rows = append(rows, nameRow{name: name})
	}
	return rows
}

// mergeServerRows merges installed MCP server names with the declared set.
func (r *lsRunner) mergeServerRows(installed []string, declared []string) []nameRow {
	installedSet := map[string]bool{}
	for _, name := range installed {
		installedSet[name] = true
	}
	declaredSet := map[string]bool{}
	for _, name := range declared {
		declaredSet[name] = true
	}

	rows := []nameRow{}
	if r.declared {
		names := append([]string(nil), installed...)
		sort.Strings(names)
		for _, name := range names {
			row := nameRow{name: name, order: 0, suffix: ""}
			if !declaredSet[name] {
				row.suffix = "  (no active declaration)"
			}
			rows = append(rows, row)
		}
		extra := []string{}
		for name := range declaredSet {
			if !installedSet[name] {
				extra = append(extra, name)
			}
		}
		sort.Strings(extra)
		for _, name := range extra {
			rows = append(rows, nameRow{name: name, order: 1, suffix: "  (not installed)"})
		}
		return rows
	}

	for _, name := range installed {
		rows = append(rows, nameRow{name: name})
	}
	return rows
}

// skillDecl is one declared skill name, with the wildcard flag of its source
// entry (a `skills = ["*"]` source's concrete names come from the lock).
type skillDecl struct {
	name     string
	wildcard bool
}

// declaredFor returns the skills and MCP servers the current context would
// install for provider in one scope, mirroring the apply gating exactly:
// global entries always, repo-level entries gated by profile/name/agents.
func (r *lsRunner) declaredFor(provider string, global bool) ([]skillDecl, []string) {
	skills := []skillDecl{}
	servers := []string{}

	for _, entry := range r.entries {
		if entry.Global != global {
			continue
		}
		if !global {
			named := entry.Name != "" && contains(r.effNames, entry.Name)
			profiled := len(entry.Profiles) > 0 && profilesIntersect(entry.ProfilesRaw, r.eff)
			if !named && !profiled {
				continue
			}
			if r.repo != nil && len(r.repo.Agents) > 0 && !agentsIntersectRaw(entry.AgentsRaw, r.repo.Agents) {
				continue
			}
		}
		if !agentsListTargetsProvider(splitTrim(entry.AgentsRaw), provider) {
			continue
		}
		names := splitTrim(entry.SkillsRaw)
		wildcard := false
		for _, n := range names {
			if n == "*" {
				wildcard = true
				break
			}
		}
		if wildcard {
			names = r.lockSkillsFor(entry, global)
		}
		for _, n := range names {
			if n == "*" || n == "" {
				continue
			}
			skills = append(skills, skillDecl{name: n, wildcard: wildcard})
		}
	}

	for _, s := range r.servers {
		if s.Global != global {
			continue
		}
		if !global {
			named := s.Name != "" && contains(r.effNames, s.Name)
			profiled := len(s.Profiles) > 0 && profilesIntersect(strings.Join(s.Profiles, ","), r.eff)
			if !named && !profiled {
				continue
			}
		}
		// Repo-config agents narrow repo-level entries only (mcp semantics:
		// a declared agents list intersecting repo agents; an undeclared
		// agents field means "manifest defaults" and is never narrowed).
		if !global && r.repo != nil && len(r.repo.Agents) > 0 && len(s.Agents) > 0 {
			hit := false
			for _, a := range s.Agents {
				if contains(r.repo.Agents, a) {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
		}
		if !agentsListTargetsProvider(s.Agents, provider) {
			continue
		}
		servers = append(servers, s.Name)
	}

	sort.Slice(skills, func(i, j int) bool { return skills[i].name < skills[j].name })
	sort.Strings(servers)
	return skills, servers
}

// lockSkillsFor resolves the concrete skill names of a wildcard source from
// the skills CLI lock of the matching scope. No lock record means the source
// has never been installed anywhere: nothing concrete to show, so only the
// wildcard annotation survives on installed entries.
func (r *lsRunner) lockSkillsFor(entry config.Install, global bool) []string {
	lock := r.lockProject
	if global {
		lock = r.lockGlobal
	}
	return lock.SkillsFor(entry.Source)
}

func (r *lsRunner) claudeSkillsIsSymlink() bool {
	dir := skillDir(r.opts.Home, r.opts.ProjectRoot, "claude-code", true)
	fi, err := os.Lstat(dir)
	return err == nil && fi.Mode()&os.ModeSymlink != 0
}

// agentsListTargetsProvider reports whether an agents list (already split)
// contains the provider's agent name. An empty list means "the manifest
// defaults", which for this repo's convention targets every provider.
func agentsListTargetsProvider(agents []string, provider string) bool {
	if len(agents) == 0 {
		return true
	}
	if provider == "claude-code" {
		return contains(agents, "claude-code") || contains(agents, "claude")
	}
	return contains(agents, provider)
}

// profilesIntersect mirrors skills.profilesIntersect (comma-joined left).
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

// agentsIntersectRaw mirrors skills.agentsIntersect: empty declared agents
// means the entry installs for the manifest defaults and is never narrowed.
func agentsIntersectRaw(raw string, repoAgents []string) bool {
	agents := splitTrim(raw)
	if len(agents) == 0 {
		return true
	}
	for _, a := range agents {
		if contains(repoAgents, a) {
			return true
		}
	}
	return false
}

func splitTrim(raw string) []string {
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

package skills

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/yanickxia/agent-env/internal/config"
	"github.com/yanickxia/agent-env/internal/stamp"
)

func (r *runner) anyFilterSeen() bool {
	return r.f.ScopeSeen || r.f.AgentSeen || r.f.SkillSeen || r.f.ProfileSeen
}

func (r *runner) cmdList() error {
	if r.anyFilterSeen() {
		return fmt.Errorf("%s: filters are supported for apply and dry-run, not list", config.Prog)
	}
	data, err := os.ReadFile(r.opts.ManifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s: manifest not found: %s", config.Prog, r.opts.ManifestPath)
		}
		return fmt.Errorf("%s: cannot read manifest %s: %v", config.Prog, r.opts.ManifestPath, err)
	}
	_, err = r.out.Write(data)
	return err
}

func (r *runner) cmdProfiles() error {
	if r.anyFilterSeen() {
		return fmt.Errorf("%s: filters are supported for apply and dry-run, not profiles", config.Prog)
	}
	entries, err := config.ParseManifest(r.opts.ManifestPath)
	if err != nil {
		return err
	}
	profiles := []string{}
	for _, entry := range entries {
		for _, p := range entry.Profiles {
			profiles = appendUnique(profiles, p)
		}
	}
	if len(profiles) == 0 {
		return fmt.Errorf("%s: no profiles declared in %s", config.Prog, r.opts.ManifestPath)
	}
	sort.Strings(profiles)
	for _, p := range profiles {
		fmt.Fprintln(r.out, p)
	}
	return nil
}

func (r *runner) cmdApply() error {
	if r.f.SkipUnchanged && (r.f.AgentSeen || r.f.SkillSeen) {
		return fmt.Errorf("%s: --skip-unchanged cannot be combined with --agent/--skill/--skills; stamps are only read/written by complete runs", config.Prog)
	}
	// zsh would launch fzf here. Interactive selection is deliberately not
	// ported, so fail clearly instead of silently syncing everything.
	if r.f.Command == CmdApply && !r.f.NonInteractive && !r.anyFilterSeen() && !r.f.SkipUnchanged {
		return fmt.Errorf("%s: interactive selection is not supported yet; pass --non-interactive or an explicit filter (--scope/--agent/--skill/--profile)", config.Prog)
	}

	if err := r.loadRepo(); err != nil {
		return err
	}
	r.computeEffectiveProfiles()
	r.detectClaudeSymlink()

	if err := r.projectApplyGating(); err != nil {
		return err
	}
	return r.processManifest(r.f.Command)
}

func (r *runner) cmdResolve() error {
	if err := r.loadRepo(); err != nil {
		return err
	}
	r.computeEffectiveProfiles()
	if err := r.resolveStatusGating(); err != nil {
		return err
	}
	return r.resolveManifest(r.f.Command)
}

func (r *runner) projectScopeOnly() bool {
	return len(r.f.Scopes) == 1 && r.f.Scopes[0] == "project"
}

func (r *runner) projectApplyGating() error {
	if r.repoMissing && len(r.f.Profiles) == 0 && r.projectScopeOnly() {
		return fmt.Errorf("%s: no .agent-env.toml in %s and no --profile given\n"+
			"  initialize one with: agent-env init <profile>... [--agent AGENT]... [--apply]\n"+
			"  or select profiles for this run only: agent-env skills %s --scope project --profile <profile>",
			config.Prog, r.opts.ProjectRoot, r.f.Command)
	}
	return nil
}

func (r *runner) resolveStatusGating() error {
	if r.repoMissing && len(r.f.Profiles) == 0 {
		return fmt.Errorf("%s: resolve/status need a repo config or --profile; no .agent-env.toml in %s\n"+
			"  initialize one with: agent-env init <profile>... [--agent AGENT]... [--apply]",
			config.Prog, r.opts.ProjectRoot)
	}
	for _, sf := range r.f.Scopes {
		if sf != "project" {
			return fmt.Errorf("%s: resolve/status are project-scoped; --scope '%s' is not supported here", config.Prog, sf)
		}
	}
	if r.f.SkillSeen {
		return fmt.Errorf("%s: --skill/--skills filters are supported for apply and dry-run, not resolve/status", config.Prog)
	}
	if r.f.SkipUnchanged {
		return fmt.Errorf("%s: --skip-unchanged is an apply flag; status reads stamps without it", config.Prog)
	}
	return nil
}

func (r *runner) processManifest(mode string) error {
	entries, err := config.ParseManifest(r.opts.ManifestPath)
	if err != nil {
		return err
	}

	projectRoot := r.opts.ProjectRoot
	store := stamp.Store{Path: r.opts.StatePath}

	entryCount := 0
	processedCount := 0
	noSelectionSkips := 0

	for _, entry := range entries {
		entryCount++
		source := config.ExpandSource(r.opts.Home, strings.TrimSpace(entry.Source))

		if !r.scopeMatches(entry.Scope) {
			continue
		}

		if entry.Scope == "project" {
			if r.effectiveProf == "" {
				noSelectionSkips++
				continue
			}
			if strings.TrimSpace(entry.ProfilesRaw) == "" {
				continue
			}
			if !profilesIntersect(entry.ProfilesRaw, r.effectiveProf) {
				continue
			}
		}

		selectedAgents, ok := r.selectEntryAgents(entry.Scope, entry.AgentsRaw)
		if !ok {
			continue
		}
		agents := splitTrimNonEmpty(selectedAgents)

		skills := append([]string(nil), entry.Skills...)
		if len(r.f.Skills) > 0 {
			set := map[string]bool{}
			for _, s := range skills {
				if s == "*" {
					continue
				}
				set[s] = true
			}
			matched := []string{}
			for _, sel := range r.f.Skills {
				if set[sel] {
					matched = append(matched, sel)
				}
			}
			if len(matched) == 0 {
				continue
			}
			skills = matched
		}

		agentsRaw := strings.TrimSpace(entry.AgentsRaw)
		skillsRaw := strings.TrimSpace(entry.SkillsRaw)

		stampScope := entry.Scope
		entrySig := ""
		if r.f.SkipUnchanged {
			if entry.Scope == "project" {
				stampScope = "project:" + projectRoot
				entrySig = stamp.Signature(source, selectedAgents, skillsRaw, entry.Mode, entry.Scope, entry.Installer, entry.EnvRaw, r.effectiveProf, projectRoot)
			} else {
				entrySig = stamp.Signature(source, agentsRaw, skillsRaw, entry.Mode, entry.Scope, entry.Installer, entry.EnvRaw, "", "")
			}
			if value, found := store.Lookup(source, stampScope); found && value == entrySig {
				fmt.Fprintf(r.out, "# skip (unchanged): %s [%s]\n", source, stampScope)
				r.runPostInstall(mode, source, entry.PostInstall)
				processedCount++
				continue
			}
		}

		ec, err := r.buildCommands(source, entry, agents, skills)
		if err != nil {
			return err
		}

		hasAgentFlag := containsArg(ec.npx, "-a")
		if !hasAgentFlag && ec.claudeSkipped && len(ec.aiden) == 0 {
			fmt.Fprintf(r.errw, "%s: no agent to install for source %s: claude-code was skipped (~/.claude/skills is a symlink) and no other agent remains\n", config.Prog, source)
		}

		installedViaNpx := false
		installedViaAiden := false

		if hasAgentFlag {
			if _, err := lookPath("node"); err != nil {
				return fmt.Errorf("%s: missing required command(s) for npx skills: node/npx", config.Prog)
			}
			if _, err := lookPath("npx"); err != nil {
				return fmt.Errorf("%s: missing required command(s) for npx skills: node/npx", config.Prog)
			}
			runCmd := ec.npx
			if len(ec.envArgs) > 0 {
				runCmd = append([]string{"env"}, append(append([]string{}, ec.envArgs...), ec.npx...)...)
			}
			r.printScopedCommand(entry.Scope, projectRoot, runCmd)
			if mode == CmdApply {
				if err := r.runScopedCommand(entry.Scope, projectRoot, runCmd); err != nil {
					fmt.Fprintf(r.errw, "%s: npx skills failed for %s: %v\n", config.Prog, source, err)
				} else {
					installedViaNpx = true
				}
			}
		}

		if len(ec.aiden) > 0 {
			r.printScopedCommand(entry.Scope, projectRoot, ec.aiden)
			if mode == CmdApply {
				if _, err := lookPath("aiden"); err != nil {
					fmt.Fprintf(r.errw, "%s: missing required command for aiden: aiden (skipped for source: %s)\n", config.Prog, source)
				} else if err := r.runScopedCommand(entry.Scope, projectRoot, ec.aiden); err != nil {
					fmt.Fprintf(r.errw, "%s: aiden skills failed for %s: %v\n", config.Prog, source, err)
				} else {
					installedViaAiden = true
				}
			}
		}

		if r.f.SkipUnchanged && mode == CmdApply && (installedViaNpx || installedViaAiden) {
			if err := store.Write(source, stampScope, entrySig); err != nil {
				return err
			}
		}

		r.runPostInstall(mode, source, entry.PostInstall)
		processedCount++
	}

	if noSelectionSkips > 0 {
		fmt.Fprintf(r.errw, "%s: no .agent-env.toml in %s and no --profile given; skipped %d project entries (use agent-env init or --profile)\n",
			config.Prog, projectRoot, noSelectionSkips)
	}
	if entryCount == 0 {
		return fmt.Errorf("%s: no active entries found in %s", config.Prog, r.opts.ManifestPath)
	}
	if processedCount == 0 {
		return fmt.Errorf("%s: no entries match the selected filters%s", config.Prog, r.filterDetail())
	}
	if r.postInstallFailures > 0 {
		return fmt.Errorf("%s: %d post_install hook(s) failed", config.Prog, r.postInstallFailures)
	}
	return nil
}

func (r *runner) filterDetail() string {
	detail := ""
	if len(r.f.Scopes) > 0 {
		detail += " scope=" + strings.Join(r.f.Scopes, ",")
	}
	if len(r.f.Agents) > 0 {
		detail += " agent=" + strings.Join(r.f.Agents, ",")
	}
	if len(r.f.Skills) > 0 {
		detail += " skills=" + strings.Join(r.f.Skills, ",")
	}
	return detail
}

func (r *runner) resolveManifest(mode string) error {
	entries, err := config.ParseManifest(r.opts.ManifestPath)
	if err != nil {
		return err
	}

	projectRoot := r.opts.ProjectRoot
	store := stamp.Store{Path: r.opts.StatePath}

	fmt.Fprintf(r.out, "repo: %s\n", projectRoot)
	fmt.Fprintf(r.out, "profiles: %s\n", r.effectiveProf)
	if r.repo != nil && len(r.repo.Agents) > 0 {
		fmt.Fprintf(r.out, "agents: %s\n", strings.Join(r.repo.Agents, ","))
	} else {
		fmt.Fprintln(r.out, "agents: (manifest defaults)")
	}
	fmt.Fprintln(r.out)
	fmt.Fprintln(r.out, "skills:")

	printed := 0
	for _, entry := range entries {
		if entry.Scope != "project" {
			continue
		}
		if strings.TrimSpace(entry.ProfilesRaw) == "" {
			continue
		}
		if !profilesIntersect(entry.ProfilesRaw, r.effectiveProf) {
			continue
		}
		resolvedAgents, ok := r.selectEntryAgents("project", entry.AgentsRaw)
		if !ok {
			continue
		}
		source := config.ExpandSource(r.opts.Home, strings.TrimSpace(entry.Source))
		skillsRaw := strings.TrimSpace(entry.SkillsRaw)
		printed++

		for _, item := range splitTrimNonEmpty(skillsRaw) {
			fmt.Fprintf(r.out, "  %s\n", item)
			fmt.Fprintf(r.out, "    source: %s\n", source)
			fmt.Fprintf(r.out, "    scope: %s\n", entry.Scope)
			if resolvedAgents != "" {
				fmt.Fprintf(r.out, "    agents: %s\n", resolvedAgents)
			}
			if mode == CmdStatus {
				sig := stamp.Signature(source, resolvedAgents, skillsRaw, entry.Mode, entry.Scope, entry.Installer, entry.EnvRaw, r.effectiveProf, projectRoot)
				if value, found := store.Lookup(source, "project:"+projectRoot); found && value == sig {
					fmt.Fprintln(r.out, "    stamp: unchanged")
				} else {
					fmt.Fprintln(r.out, "    stamp: not installed (or changed)")
				}
			}
		}
	}
	if printed == 0 {
		fmt.Fprintln(r.out, "  (no matching project entries)")
	}
	return nil
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

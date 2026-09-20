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
	return r.f.AgentSeen || r.f.SkillSeen || r.f.ProfileSeen || r.f.NoRepo
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
			if config.IsGlobalProfile(p) {
				continue
			}
			profiles = appendUnique(profiles, p)
		}
	}
	if len(profiles) == 0 {
		return fmt.Errorf("%s: no selectable profiles declared in %s (\"global\" is a reserved keyword and is excluded)", config.Prog, r.opts.ManifestPath)
	}
	sort.Strings(profiles)
	for _, p := range profiles {
		fmt.Fprintln(r.out, p)
	}
	return nil
}

func (r *runner) cmdApply() error {
	if r.f.NoRepo && r.f.ProfileSeen {
		return fmt.Errorf("%s: --no-repo cannot be combined with --profile/--profiles; --no-repo only installs global entries, drop --profile", config.Prog)
	}
	if r.f.SkipUnchanged && (r.f.AgentSeen || r.f.SkillSeen) {
		return fmt.Errorf("%s: --skip-unchanged cannot be combined with --agent/--skill/--skills; stamps are only read/written by complete runs", config.Prog)
	}
	// Interactive selection is deliberately not ported, so fail clearly instead
	// of silently syncing everything.
	if r.f.Command == CmdApply && !r.f.NonInteractive && !r.anyFilterSeen() && !r.f.SkipUnchanged {
		return fmt.Errorf("%s: interactive selection is not supported yet; pass --non-interactive or an explicit filter (--agent/--skill/--profile)", config.Prog)
	}

	if err := r.loadRepo(); err != nil {
		return err
	}
	r.computeEffectiveProfiles()
	r.detectClaudeSymlink()
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

func (r *runner) resolveStatusGating() error {
	if r.f.NoRepo {
		return fmt.Errorf("%s: --no-repo is supported for apply and dry-run, not resolve/status", config.Prog)
	}
	if r.repoMissing && len(r.f.Profiles) == 0 {
		start := r.opts.StartDir
		if start == "" {
			start = r.opts.ProjectRoot
		}
		return fmt.Errorf("%s: resolve/status need a repo config or --profile; no .agent-env.toml found from %s up to /\n"+
			"  initialize one with: agent-env init <profile>... [--agent AGENT]... [--apply]",
			config.Prog, start)
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
	repoLevelSkips := 0

	for _, entry := range entries {
		entryCount++
		source := config.ExpandSource(r.opts.Home, strings.TrimSpace(entry.Source))

		if !entry.Global {
			named := entry.Name != "" && stringInList(r.effectiveNames, entry.Name)
			profiled := len(entry.Profiles) > 0 && profilesIntersect(entry.ProfilesRaw, r.effectiveProf)
			if !named && !profiled {
				if !r.f.NoRepo && r.effectiveProf == "" && len(r.effectiveNames) == 0 {
					repoLevelSkips++
				}
				continue
			}
		}

		selectedAgents, ok := r.selectEntryAgents(entry.Global, entry.AgentsRaw)
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

		// The stamp payload keeps its historical "user"/"project" vocabulary:
		// global entries derive "user", repo-level entries derive "project".
		stampScope := "project:" + projectRoot
		signatureScope := "project"
		if entry.Global {
			stampScope = "user"
			signatureScope = "user"
		}

		// Global entries carry a context-free "user" stamp, so their lookup runs
		// on every apply (not just under --skip-unchanged): a matching stamp
		// means the global install is already in sync and npx is not re-run.
		// Repo-level "project" stamps stay opt-in via --skip-unchanged.
		entrySig := ""
		if r.f.SkipUnchanged || entry.Global {
			if entry.Global {
				entrySig = stamp.Signature(source, agentsRaw, skillsRaw, entry.Mode, signatureScope, entry.Installer, entry.EnvRaw, "", "")
			} else {
				entrySig = stamp.Signature(source, selectedAgents, skillsRaw, entry.Mode, signatureScope, entry.Installer, entry.EnvRaw, r.effectiveProf, projectRoot)
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
			r.printScopedCommand(entry.Global, projectRoot, runCmd)
			if mode == CmdApply {
				if err := r.runScopedCommand(entry.Global, projectRoot, runCmd); err != nil {
					fmt.Fprintf(r.errw, "%s: npx skills failed for %s: %v\n", config.Prog, source, err)
				} else {
					installedViaNpx = true
				}
			}
		}

		if len(ec.aiden) > 0 {
			r.printScopedCommand(entry.Global, projectRoot, ec.aiden)
			if mode == CmdApply {
				if _, err := lookPath("aiden"); err != nil {
					fmt.Fprintf(r.errw, "%s: missing required command for aiden: aiden (skipped for source: %s)\n", config.Prog, source)
				} else if err := r.runScopedCommand(entry.Global, projectRoot, ec.aiden); err != nil {
					fmt.Fprintf(r.errw, "%s: aiden skills failed for %s: %v\n", config.Prog, source, err)
				} else {
					installedViaAiden = true
				}
			}
		}

		// Global [user] stamps are refreshed on every apply so the next plain
		// apply can skip. A narrowed run (--agent/--skill/--skills) must not
		// write: the user signature describes the declared shape, not the
		// narrowed install set, so writing would falsely mark it in sync.
		writeStamp := r.f.SkipUnchanged
		if entry.Global && len(r.f.Agents) == 0 && len(r.f.Skills) == 0 {
			writeStamp = true
		}
		if writeStamp && mode == CmdApply && (installedViaNpx || installedViaAiden) {
			if err := store.Write(source, stampScope, entrySig); err != nil {
				return err
			}
		}

		r.runPostInstall(mode, source, entry.PostInstall)
		processedCount++
	}

	if repoLevelSkips > 0 {
		start := r.opts.StartDir
		if start == "" {
			start = r.opts.ProjectRoot
		}
		fmt.Fprintf(r.errw, "%s: no .agent-env.toml found from %s up to / and no --profile given; skipped %d repo-level entries (use agent-env init or --profile)\n",
			config.Prog, start, repoLevelSkips)
	}
	// Zero active entries (empty manifest, or everything filtered/gated out) is
	// success: print an informational line and stop. Manifest-level protection is
	// enforced by the parser.
	if entryCount == 0 || processedCount == 0 {
		fmt.Fprintf(r.errw, "%s: no active entries; nothing to install%s\n", config.Prog, r.filterDetail())
		return nil
	}
	if r.postInstallFailures > 0 {
		return fmt.Errorf("%s: %d post_install hook(s) failed", config.Prog, r.postInstallFailures)
	}
	return nil
}

func (r *runner) filterDetail() string {
	detail := ""
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
	fmt.Fprintf(r.out, "names: %s\n", strings.Join(r.effectiveNames, ","))
	if r.repo != nil && len(r.repo.Agents) > 0 {
		fmt.Fprintf(r.out, "agents: %s\n", strings.Join(r.repo.Agents, ","))
	} else {
		fmt.Fprintln(r.out, "agents: (manifest defaults)")
	}
	fmt.Fprintln(r.out, "layers:")
	if len(r.repoLayers) == 0 {
		fmt.Fprintln(r.out, "  (none)")
	} else {
		for _, p := range r.repoLayers {
			fmt.Fprintf(r.out, "  %s\n", p)
		}
	}
	fmt.Fprintln(r.out)
	fmt.Fprintln(r.out, "skills:")

	printed := 0
	for _, entry := range entries {
		if entry.Global {
			continue
		}
		named := entry.Name != "" && stringInList(r.effectiveNames, entry.Name)
		profiled := len(entry.Profiles) > 0 && profilesIntersect(entry.ProfilesRaw, r.effectiveProf)
		if !named && !profiled {
			continue
		}
		resolvedAgents, ok := r.selectEntryAgents(false, entry.AgentsRaw)
		if !ok {
			continue
		}
		source := config.ExpandSource(r.opts.Home, strings.TrimSpace(entry.Source))
		skillsRaw := strings.TrimSpace(entry.SkillsRaw)
		printed++

		for _, item := range splitTrimNonEmpty(skillsRaw) {
			fmt.Fprintf(r.out, "  %s\n", item)
			fmt.Fprintf(r.out, "    source: %s\n", source)
			if entry.Name != "" {
				fmt.Fprintf(r.out, "    name: %s\n", entry.Name)
			}
			fmt.Fprintf(r.out, "    profiles: %s\n", entry.ProfilesRaw)
			if resolvedAgents != "" {
				fmt.Fprintf(r.out, "    agents: %s\n", resolvedAgents)
			}
			if mode == CmdStatus {
				sig := stamp.Signature(source, resolvedAgents, skillsRaw, entry.Mode, "project", entry.Installer, entry.EnvRaw, r.effectiveProf, projectRoot)
				if value, found := store.Lookup(source, "project:"+projectRoot); found && value == sig {
					fmt.Fprintln(r.out, "    stamp: unchanged")
				} else {
					fmt.Fprintln(r.out, "    stamp: not installed (or changed)")
				}
			}
		}
	}
	if printed == 0 {
		fmt.Fprintln(r.out, "  (no matching repo-level entries)")
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

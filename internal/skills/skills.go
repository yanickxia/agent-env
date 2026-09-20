// Package skills implements the skills domain: manifest-driven installs,
// profile selection, repo-config narrowing, stamp-based skip-unchanged, and
// the read-only resolve/status/profiles/list views. Behaviour mirrors the
// retired zsh agent-skills-sync script, which was the normative specification.
package skills

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yanickxia/agent-env/internal/config"
)

// Command names.
const (
	CmdApply    = "apply"
	CmdDryRun   = "dry-run"
	CmdList     = "list"
	CmdProfiles = "profiles"
	CmdResolve  = "resolve"
	CmdStatus   = "status"
)

// Options carries resolved paths and output sinks. Empty path fields are
// resolved from the environment by the caller (see internal/cli).
type Options struct {
	ManifestPath   string
	StatePath      string
	RepoConfigPath string
	ProjectRoot    string
	StartDir       string
	Home           string
	Force          bool
	Stdout         io.Writer
	Stderr         io.Writer
}

// Filters is the normalized CLI filter set plus the flags that gate them.
type Filters struct {
	Command string

	Agents   []string
	Profiles []string
	Skills   []string

	AgentSeen   bool
	ProfileSeen bool
	SkillSeen   bool

	NonInteractive bool
	Interactive    bool
	SkipUnchanged  bool
	NoRepo         bool
}

type runner struct {
	opts Options
	f    Filters
	out  io.Writer
	errw io.Writer

	repo           *config.RepoConfig
	repoMissing    bool
	repoLoaded     bool
	repoLayers     []string
	effectiveProf  string
	effectiveList  []string
	effectiveNames []string

	claudeSkillsIsSymlink bool
	claudeSkipNoticeShown bool
	postInstallDone       map[string]bool
	postInstallFailures   int
}

// Run executes one skills command. It returns an error whose message is
// already user-facing (prefixed with "agent-env:") whenever the command
// should exit non-zero.
func Run(opts Options, f Filters) error {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	r := &runner{
		opts:            opts,
		f:               f,
		out:             opts.Stdout,
		errw:            opts.Stderr,
		postInstallDone: map[string]bool{},
	}
	if err := r.normalizeFilters(); err != nil {
		return err
	}
	if r.f.Interactive && r.f.NonInteractive {
		return fmt.Errorf("%s: --interactive and --non-interactive cannot be used together", config.Prog)
	}
	if r.f.Interactive {
		return fmt.Errorf("%s: --interactive is not supported by agent-env (fzf selection was not ported); pass --non-interactive or explicit --agent/--skill/--profile filters", config.Prog)
	}

	switch f.Command {
	case CmdList:
		return r.cmdList()
	case CmdProfiles:
		return r.cmdProfiles()
	case CmdApply, CmdDryRun:
		return r.cmdApply()
	case CmdResolve, CmdStatus:
		return r.cmdResolve()
	default:
		return fmt.Errorf("%s: unknown skills command %q", config.Prog, f.Command)
	}
}

// normalizeFilters trims/dedupes/validates the raw flag values, mirroring the
// zsh _add_*_filters helpers.
func (r *runner) normalizeFilters() error {
	agents := []string{}
	for _, raw := range r.f.Agents {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			continue
		}
		agents = appendUnique(agents, tok)
	}
	r.f.Agents = agents

	profiles := []string{}
	for _, raw := range r.f.Profiles {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			continue
		}
		if !config.KebabCase(tok) {
			return fmt.Errorf("%s: invalid profile name '%s' (expected kebab-case, e.g. ark-mlops)", config.Prog, tok)
		}
		if config.IsGlobalProfile(tok) {
			return fmt.Errorf("%s: \"global\" is a reserved profile keyword and cannot be selected with --profile", config.Prog)
		}
		profiles = appendUnique(profiles, tok)
	}
	r.f.Profiles = profiles

	skills := []string{}
	for _, raw := range r.f.Skills {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			continue
		}
		skills = appendUnique(skills, tok)
	}
	r.f.Skills = skills
	return nil
}

func appendUnique(list []string, item string) []string {
	for _, existing := range list {
		if existing == item {
			return list
		}
	}
	return append(list, item)
}

// loadRepo resolves the repo selection once per project-aware command. An
// explicit RepoConfigPath (or AGENT_ENV_REPO_CONFIG) is single-file mode;
// otherwise .agent-env.toml files are discovered from StartDir up to "/" and
// merged (nearest first).
func (r *runner) loadRepo() error {
	if r.repoLoaded {
		return nil
	}
	r.repoLoaded = true

	// --no-repo is an explicit declaration of "no repo context": skip the
	// .agent-env.toml walk (and AGENT_ENV_REPO_CONFIG/explicit path) entirely so
	// an ancestor config can never leak repo-level entries into a global run.
	if r.f.NoRepo {
		return nil
	}

	start := r.opts.StartDir
	if start == "" {
		start = r.opts.ProjectRoot
	}
	explicit := r.opts.RepoConfigPath
	if explicit == config.DefaultRepoConfigPath(r.opts.ProjectRoot) {
		explicit = ""
	}
	cfg, layers, found, err := config.LoadRepoSelection(
		explicit, start, os.Getenv,
		"install details and hooks belong in the global manifest")
	if err != nil {
		return err
	}
	r.repoLayers = layers
	if !found {
		r.repoMissing = true
		return nil
	}
	r.repo = cfg
	return nil
}

// computeEffectiveProfiles mirrors compute_effective_profiles: --profile wins
// over the repo config, the result is deduped and byte-sorted so the stamp
// payload is insensitive to declaration order.
func (r *runner) computeEffectiveProfiles() {
	var selected []string
	if len(r.f.Profiles) > 0 {
		selected = r.f.Profiles
	} else if r.repo != nil {
		selected = r.repo.Profiles
	}

	unique := []string{}
	for _, item := range selected {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		unique = appendUnique(unique, item)
	}
	sort.Strings(unique)
	r.effectiveList = unique
	r.effectiveProf = strings.Join(unique, ",")

	// Names come only from the repo config (there is no CLI --name for skills).
	names := []string{}
	if r.repo != nil {
		for _, n := range r.repo.Names {
			names = appendUnique(names, n)
		}
	}
	r.effectiveNames = names
}

func (r *runner) detectClaudeSymlink() {
	if r.opts.Home == "" {
		return
	}
	if fi, err := os.Lstat(filepath.Join(r.opts.Home, ".claude", "skills")); err == nil {
		r.claudeSkillsIsSymlink = fi.Mode()&os.ModeSymlink != 0
	}
}

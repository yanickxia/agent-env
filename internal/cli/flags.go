package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/yanickxia/agent-env/internal/skills"
)

// ExitError carries an explicit process exit code and a fully-formatted
// message (empty means "print nothing extra"). Silent errors from cobra are
// turned into this type at the CLI boundary.
type ExitError struct {
	Code int
	Msg  string
}

func (e *ExitError) Error() string { return e.Msg }

// filterFlags collects the raw flag values. Splitting/trimming/validation
// happens either here (comma expansion, matching zsh) or in skills.Run
// (profile validation). The *Seen flags use pflag.Changed so an explicit
// empty value still counts as a filter being present.
type filterFlags struct {
	agents   []string
	profile  []string
	profiles []string
	skill    []string
	skills   []string
	noInter  bool
	yes      bool
	noInter2 bool
	skipUnch bool
	interact bool
	noRepo   bool
	force    bool
	prune    bool
}

// addFilterFlags registers the full filter surface on every skills
// subcommand; individual commands reject the flags they do not support so the
// error mirrors the domain semantics.
func addFilterFlags(cmd *cobra.Command, f *filterFlags) {
	fl := cmd.Flags()
	fl.StringArrayVar(&f.agents, "agent", nil, "only install for AGENT (repeatable/comma-separated)")
	fl.StringArrayVar(&f.profile, "profile", nil, "temporary profile filter (repeatable/comma-separated)")
	fl.StringArrayVar(&f.profiles, "profiles", nil, "comma-separated alias for --profile")
	fl.StringArrayVar(&f.skill, "skill", nil, "only install the named skill (repeatable)")
	fl.StringArrayVar(&f.skills, "skills", nil, "comma-separated multi-skill filter")
	fl.BoolVar(&f.noInter, "non-interactive", false, "disable prompting")
	fl.BoolVarP(&f.yes, "yes", "y", false, "alias for --non-interactive")
	fl.BoolVar(&f.noInter2, "no-interactive", false, "alias for --non-interactive")
	fl.BoolVar(&f.skipUnch, "skip-unchanged", false, "skip entries whose stamp matches the last apply")
	fl.BoolVar(&f.interact, "interactive", false, "interactive selection (not supported)")
	fl.BoolVar(&f.noRepo, "no-repo", false, "no repo context: skip .agent-env.toml discovery and install global entries only (mutually exclusive with --profile)")
	fl.BoolVar(&f.force, "force", false, "reinstall the entries owned by the current context even when stamps match: repo-level entries in a repo run, global entries with --no-repo (repairs interrupted installs); overrides --skip-unchanged")
	fl.BoolVar(&f.prune, "prune", false, "after apply/dry-run, clean up stamped installs whose source is no longer active in this context (repo-level in a repo run, global with --no-repo); manual installs without a stamp are never touched")
}

func (f *filterFlags) toFilters(cmd *cobra.Command, command string) skills.Filters {
	fl := skills.Filters{
		Command:        command,
		NonInteractive: f.noInter || f.yes || f.noInter2,
		Interactive:    f.interact,
		SkipUnchanged:  f.skipUnch,
		NoRepo:         f.noRepo,
	}
	fl.Agents = flattenComma(f.agents)
	fl.Profiles = append(flattenComma(f.profile), flattenComma(f.profiles)...)
	fl.Skills = append(append([]string{}, f.skill...), flattenComma(f.skills)...)

	fl.AgentSeen = cmd.Flags().Changed("agent")
	fl.ProfileSeen = cmd.Flags().Changed("profile") || cmd.Flags().Changed("profiles")
	fl.SkillSeen = cmd.Flags().Changed("skill") || cmd.Flags().Changed("skills")
	return fl
}

// flattenComma expands repeated and comma-separated values into tokens.
// Empty tokens are dropped by skills.Run's normalizer.
func flattenComma(raw []string) []string {
	out := []string{}
	for _, r := range raw {
		for _, tok := range strings.Split(r, ",") {
			out = append(out, tok)
		}
	}
	return out
}

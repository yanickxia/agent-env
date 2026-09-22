package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yanickxia/agent-env/internal/mcp"
	"github.com/yanickxia/agent-env/internal/skills"
)

// allFlags collects the flags shared by both domains for the top-level
// apply/dry-run commands. Domain-specific filters (--skill, --name, ...) are
// deliberately not defined here, so cobra rejects them as unknown flags.
type allFlags struct {
	agents   []string
	profile  []string
	profiles []string
	noInter  bool
	yes      bool
	noInter2 bool
	skipUnch bool
	noRepo   bool
	force    bool
	prune    bool
}

func addAllFlags(cmd *cobra.Command, f *allFlags) {
	fl := cmd.Flags()
	fl.StringArrayVar(&f.agents, "agent", nil, "only sync for AGENT (repeatable/comma-separated)")
	fl.StringArrayVar(&f.profile, "profile", nil, "temporary profile filter (repeatable/comma-separated)")
	fl.StringArrayVar(&f.profiles, "profiles", nil, "comma-separated alias for --profile")
	fl.BoolVar(&f.noInter, "non-interactive", false, "disable prompting (required for apply)")
	fl.BoolVarP(&f.yes, "yes", "y", false, "alias for --non-interactive")
	fl.BoolVar(&f.noInter2, "no-interactive", false, "alias for --non-interactive")
	fl.BoolVar(&f.skipUnch, "skip-unchanged", false, "skills only: skip entries whose stamp matches the last apply (MCP ignores this)")
	fl.BoolVar(&f.force, "force", false, "skills only: reinstall the entries owned by the current context even when stamps match (repo-level in a repo run, global with --no-repo; repairs interrupted installs); overrides --skip-unchanged, ignored by MCP")
	fl.BoolVar(&f.prune, "prune", false, "skills only: after apply/dry-run, clean up stamped installs whose source is no longer active (repo-level in a repo run, global with --no-repo); manual installs are never touched. Ignored by MCP, whose managed regions are already fully replaced")
	fl.BoolVar(&f.noRepo, "no-repo", false, "no repo context: skip .agent-env.toml discovery and install global entries only (mutually exclusive with --profile)")
}

// sharedFilters is the normalized, domain-neutral filter set passed to both
// skills.Run and mcp.Run.
type sharedFilters struct {
	command        string
	agents         []string
	profiles       []string
	agentSeen      bool
	profileSeen    bool
	nonInteractive bool
	skipUnchanged  bool
	noRepo         bool
	force          bool
	prune          bool
}

func (f *allFlags) toShared(cmd *cobra.Command, command string) sharedFilters {
	return sharedFilters{
		command:        command,
		agents:         flattenComma(f.agents),
		profiles:       append(flattenComma(f.profile), flattenComma(f.profiles)...),
		agentSeen:      cmd.Flags().Changed("agent"),
		profileSeen:    cmd.Flags().Changed("profile") || cmd.Flags().Changed("profiles"),
		nonInteractive: f.noInter || f.yes || f.noInter2,
		skipUnchanged:  f.skipUnch,
		noRepo:         f.noRepo,
		force:          f.force,
		prune:          f.prune,
	}
}

// runAll runs the skills domain, then the MCP domain, regardless of whether
// the first failed. It returns both errors so the caller can aggregate them.
func runAll(sf sharedFilters, sOpts skills.Options, mOpts mcp.Options) (skillsErr, mcpErr error) {
	out := sOpts.Stdout
	if out == nil {
		out = os.Stdout
	}

	sOpts.Force = sf.force
	sOpts.Prune = sf.prune

	fmt.Fprintln(out, "=== skills ===")
	skillsErr = skills.Run(sOpts, skills.Filters{
		Command:        sf.command,
		Agents:         sf.agents,
		Profiles:       sf.profiles,
		AgentSeen:      sf.agentSeen,
		ProfileSeen:    sf.profileSeen,
		NonInteractive: sf.nonInteractive,
		SkipUnchanged:  sf.skipUnchanged,
		NoRepo:         sf.noRepo,
	})

	fmt.Fprintln(out, "=== mcp ===")
	mcpErr = mcp.Run(mOpts, mcp.Filters{
		Command:        sf.command,
		Agents:         sf.agents,
		Profiles:       sf.profiles,
		AgentSeen:      sf.agentSeen,
		ProfileSeen:    sf.profileSeen,
		NonInteractive: sf.nonInteractive,
		NoRepo:         sf.noRepo,
	})

	return skillsErr, mcpErr
}

func newAllCmd(command, short string) *cobra.Command {
	var f allFlags
	c := &cobra.Command{
		Use:   command + " [--agent AGENT]... [--profile PROFILE]... [--non-interactive] [--skip-unchanged] [--force] [--prune] [--no-repo]",
		Short: short,
		Long: "Run both domains in one shot: skills first, then MCP. Both domains always run,\n" +
			"even when the first one fails; the exit code is non-zero when either fails.\n\n" +
			"Only flags shared by both domains are accepted here. --skip-unchanged, --force\n" +
			"and --prune are passed to skills and ignored by MCP (MCP's managed regions are\n" +
			"already fully replaced on every apply, so no prune is needed). Domain-specific\n" +
			"filters such as --skill or --name live only on the `skills` / `mcp` subcommands.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sf := f.toShared(cmd, command)
			if sf.noRepo && sf.profileSeen {
				return &ExitError{Code: 1, Msg: "agent-env: --no-repo cannot be combined with --profile/--profiles; --no-repo only installs global entries, drop --profile"}
			}
			if command == skills.CmdApply && !sf.nonInteractive {
				return &ExitError{Code: 1, Msg: "agent-env: apply requires --non-interactive (interactive selection is not supported)"}
			}
			sErr, mErr := runAll(sf, resolveOptions(), resolveMCPOptions())
			if sErr == nil && mErr == nil {
				return nil
			}
			msgs := []string{}
			if sErr != nil {
				msgs = append(msgs, sErr.Error())
			}
			if mErr != nil {
				msgs = append(msgs, mErr.Error())
			}
			return &ExitError{Code: 1, Msg: strings.Join(msgs, "\n")}
		},
	}
	addAllFlags(c, &f)
	return c
}

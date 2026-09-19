package cli

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/yanickxia/agent-env/internal/config"
	"github.com/yanickxia/agent-env/internal/mcp"
)

// mcpFilterFlags collects the raw mcp flag values. --name is repeatable and
// literal; --names is its comma-separated alias.
type mcpFilterFlags struct {
	scopes   []string
	agents   []string
	name     []string
	names    []string
	profile  []string
	profiles []string
	noInter  bool
	yes      bool
	noInter2 bool
	interact bool
}

func addMCPFilterFlags(cmd *cobra.Command, f *mcpFilterFlags) {
	fl := cmd.Flags()
	fl.StringArrayVar(&f.scopes, "scope", nil, "user, project, or all (repeatable/comma-separated)")
	fl.StringArrayVar(&f.agents, "agent", nil, "only sync for AGENT (repeatable/comma-separated)")
	fl.StringArrayVar(&f.name, "name", nil, "only sync the named MCP server (repeatable)")
	fl.StringArrayVar(&f.names, "names", nil, "comma-separated alias for --name")
	fl.StringArrayVar(&f.profile, "profile", nil, "temporary profile filter (repeatable/comma-separated)")
	fl.StringArrayVar(&f.profiles, "profiles", nil, "comma-separated alias for --profile")
	fl.BoolVar(&f.noInter, "non-interactive", false, "disable prompting")
	fl.BoolVarP(&f.yes, "yes", "y", false, "alias for --non-interactive")
	fl.BoolVar(&f.noInter2, "no-interactive", false, "alias for --non-interactive")
	fl.BoolVar(&f.interact, "interactive", false, "interactive selection (not supported)")
}

func (f *mcpFilterFlags) toFilters(cmd *cobra.Command, command string) mcp.Filters {
	fl := mcp.Filters{
		Command:        command,
		NonInteractive: f.noInter || f.yes || f.noInter2,
		Interactive:    f.interact,
	}
	fl.Scopes = flattenComma(f.scopes)
	fl.Agents = flattenComma(f.agents)
	fl.Names = append(append([]string{}, f.name...), flattenComma(f.names)...)
	fl.Profiles = append(flattenComma(f.profile), flattenComma(f.profiles)...)

	fl.ScopeSeen = cmd.Flags().Changed("scope")
	fl.AgentSeen = cmd.Flags().Changed("agent")
	fl.ProfileSeen = cmd.Flags().Changed("profile") || cmd.Flags().Changed("profiles")
	return fl
}

func newMCPCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "mcp",
		Short: "Manage MCP servers",
		Long:  "Manage MCP servers declared in the unified config's [[servers]] table.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	c.AddCommand(
		newMCPActionCmd("apply", "Sync MCP servers for command-driven agents (default)", mcp.CmdApply),
		newMCPActionCmd("dry-run", "Print the commands and config blocks without writing them", mcp.CmdDryRun),
		newMCPActionCmd("list", "Print the current config", mcp.CmdList),
		newMCPActionCmd("profiles", "Print the distinct profiles declared by config servers (sorted)", mcp.CmdProfiles),
		newMCPUpsertStdinCmd(),
	)
	return c
}

func newMCPActionCmd(use, short, command string) *cobra.Command {
	var f mcpFilterFlags
	c := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := resolveMCPOptions()
			fl := f.toFilters(cmd, command)
			if err := mcp.Run(opts, fl); err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			return nil
		},
	}
	addMCPFilterFlags(c, &f)
	return c
}

func newMCPUpsertStdinCmd() *cobra.Command {
	var f mcpFilterFlags
	c := &cobra.Command{
		Use:   "upsert-stdin",
		Short: "Upsert an agent's user-level marker block from stdin to stdout (chezmoi modify_)",
		Long: "Read a base file from stdin and print it with --agent's user-level marker\n" +
			"block upserted. Never touches the filesystem; `apply --scope user` uses the\n" +
			"same core, so their outputs are byte-identical.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := resolveMCPOptions()
			fl := f.toFilters(cmd, mcp.CmdUpsertStdin)
			if err := mcp.Run(opts, fl); err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			return nil
		},
	}
	addMCPFilterFlags(c, &f)
	return c
}

func resolveMCPOptions() mcp.Options {
	getenv := os.Getenv
	home := config.DefaultHome()
	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}
	root := config.ProjectRoot(wd)
	return mcp.Options{
		ManifestPath:   config.ManifestPath(getenv, home),
		SecretsPath:    config.SecretsPath(getenv, home),
		RepoConfigPath: config.RepoConfigPath(getenv, root),
		ProjectRoot:    root,
		Home:           home,
		WorkDir:        wd,
		ClaudeJSON:     config.ClaudeJSONPath(getenv, home),
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
		Stdin:          os.Stdin,
		LookupEnv:      getenv,
	}
}

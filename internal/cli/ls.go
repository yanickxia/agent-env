package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yanickxia/agent-env/internal/config"
	"github.com/yanickxia/agent-env/internal/inventory"
)

func newLsCmd() *cobra.Command {
	var providers []string
	var declared bool
	var noRepo bool
	c := &cobra.Command{
		Use:   "ls",
		Short: "List installed skills and MCP servers per provider (read-only)",
		Long: "List what is actually installed on disk for each provider, plus the\n" +
			"declared state from the unified config and the repo selection.\n\n" +
			"Providers: codex, claude-code (alias: claude), opencode. Default: all three.\n" +
			"  skills: entries of the provider's skills directory (global and repo scope)\n" +
			"  mcp:    agent-env managed MCP servers in the provider's config files\n" +
			"          (codex/opencode: managed marker block only; claude-code: mcpServers)\n\n" +
			"--declared also lists entries the config declares for the current\n" +
			"context but that are not installed yet, and annotates installed\n" +
			"entries that no active declaration owns (stale). Wildcard sources\n" +
			"(skills = [\"*\"]) resolve their concrete skill names from the skills\n" +
			"CLI lock files.\n\n" +
			"--no-repo hides the repo scope (global only), like apply --no-repo.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := resolveLsOptions()
			f := inventory.Filters{
				Declared: declared,
				NoRepo:   noRepo,
			}
			if cmd.Flags().Changed("provider") {
				f.Providers = flattenComma(providers)
				for _, p := range f.Providers {
					p = strings.TrimSpace(p)
					if p == "" {
						continue
					}
					if !inventory.ValidProvider(p) {
						return &ExitError{Code: 1, Msg: "agent-env: unknown provider '" + p + "' (expected codex, claude-code, opencode)"}
					}
				}
			}
			if err := inventory.Run(opts, f); err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			return nil
		},
	}
	c.Flags().StringArrayVar(&providers, "provider", nil, "only list for PROVIDER (repeatable/comma-separated: codex, claude-code, opencode)")
	c.Flags().BoolVar(&declared, "declared", false, "also list declared-but-not-installed entries and annotate stale ones")
	c.Flags().BoolVar(&noRepo, "no-repo", false, "no repo context: show the global scope only")
	return c
}

func resolveLsOptions() inventory.Options {
	getenv := os.Getenv
	home := config.DefaultHome()
	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}
	root := config.ProjectRoot(wd)
	return inventory.Options{
		ManifestPath:   config.ManifestPath(getenv, home),
		RepoConfigPath: config.RepoConfigPath(getenv, root),
		ProjectRoot:    root,
		StartDir:       wd,
		Home:           home,
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
		LookupEnv:      getenv,
	}
}

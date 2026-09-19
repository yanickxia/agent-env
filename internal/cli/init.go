package cli

import (
	"errors"
	"os"

	"github.com/spf13/cobra"
	"github.com/yanickxia/agent-env/internal/config"
	"github.com/yanickxia/agent-env/internal/repoinit"
)

func newInitCmd() *cobra.Command {
	var (
		agents []string
		names  []string
		apply  bool
		dryRun bool
	)
	c := &cobra.Command{
		Use:   "init PROFILE... [--name N]... [--agent A]... [--apply] [--dry-run]",
		Short: "Create or update the repo-level .agent-env.toml",
		Long: `Create or update the repo-level .agent-env.toml for the current repository.
The file records which profiles and named entries agent-env should install here.

Arguments:
  PROFILE              One or more kebab-case profile names to enable for this
                       repository (e.g. base, ark-mlops). Invalid names are
                       rejected and repeated names are de-duplicated.

Options:
  --name NAME          Select individual entries by name (repeatable and/or
                       comma-separated). A name matches both a [[installs]] and a
                       [[servers]] entry with that name. Names bypass the profile
                       grouping. At least one PROFILE or --name is required.
  --agent AGENT        Agent backend(s) for this repository (repeatable/comma-separated).
  --apply              After writing, run
                       agent-env skills apply --non-interactive --skip-unchanged.
  --dry-run            Print the path and content without writing.

Behavior:
  The repository root is resolved with git rev-parse --show-toplevel (falling
  back to the current directory). The config path defaults to
  <repo_root>/.agent-env.toml; AGENT_ENV_REPO_CONFIG overrides it
  (AGENT_SKILLS_REPO_CONFIG stays a compatible alias).

  If the config already exists it is parsed and merged: existing profiles and
  agents come first, followed by the CLI values (de-duplicated, order
  preserved). Existing mode and [vars] are preserved as-is. The file is then
  rewritten atomically (temp file in the same directory + rename); comments are
  not preserved.

Repository config schema (only these keys are permitted):
  profiles = ["base", "ark-mlops"]   # optional string array of kebab-case profiles
  names    = ["clickup", "playwright"]  # optional string array of entry names
  agents   = ["codex", "opencode"]   # optional string array, non-empty entries
  mode     = "symlink"               # optional; exactly "symlink" or "copy"
  [vars]                             # optional table of install settings
  team = "ark"                       # string / number / boolean / string array

  Repo config may only SELECT entries for the repository. It must not carry
  installation details or hooks: keys such as post_install, source or skills
  are rejected on purpose so a checked-in config cannot run commands.

Environment:
  AGENT_ENV_REPO_CONFIG   Override the repo config path (full path).
  AGENT_SKILLS_SYNC_BIN   Override the agent-env executable used by --apply
                          (defaults to the running agent-env binary).`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := resolveInitOptions()
			fl := repoinit.Filters{
				Profiles: args,
				Names:    flattenComma(names),
				Agents:   flattenComma(agents),
				Apply:    apply,
				DryRun:   dryRun,
			}
			if err := repoinit.Run(opts, fl); err != nil {
				var afe *repoinit.ApplyFailedError
				if errors.As(err, &afe) {
					// The child already reported its own diagnostics.
					return &ExitError{Code: afe.Code}
				}
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			return nil
		},
	}
	c.Flags().StringArrayVar(&agents, "agent", nil, "agent backend(s) for this repo (repeatable/comma-separated)")
	c.Flags().StringArrayVar(&names, "name", nil, "select individual entries by name (repeatable/comma-separated)")
	c.Flags().BoolVar(&apply, "apply", false, "after writing, run: agent-env skills apply --non-interactive --skip-unchanged")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print the path and content without writing")
	return c
}

func resolveInitOptions() repoinit.Options {
	getenv := os.Getenv
	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}
	root := config.ProjectRoot(wd)
	return repoinit.Options{
		RepoConfigPath: config.RepoConfigPath(getenv, root),
		ProjectRoot:    root,
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
		LookupEnv:      getenv,
	}
}

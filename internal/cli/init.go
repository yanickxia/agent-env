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
		apply  bool
		dryRun bool
	)
	c := &cobra.Command{
		Use:   "init PROFILE... [--agent AGENT]... [--apply] [--dry-run]",
		Short: "Create or update the repo-level .agent-env.toml",
		Long: `Create or update the repo-level .agent-env.toml for the current repository.
The file records which skill profiles agent-env should install here.

Arguments:
  PROFILE              One or more kebab-case profile names to enable for this
                       repository (e.g. base, ark-mlops). At least one is
                       required. Invalid names are rejected and repeated names
                       are de-duplicated, preserving order.

Options:
  --agent AGENT        Agent backend(s) for this repository (e.g. codex,
                       opencode, trae). Repeatable and/or comma-separated
                       (--agent codex,opencode). Tokens are trimmed and
                       de-duplicated in order. Optional.
  --apply              After writing the config, run
                       agent-env skills apply --non-interactive --skip-unchanged
                       from the repository root (repo-level profiles gate the
                       install). The sync exit code is propagated.
  --dry-run            Print the path and the full content that would be written,
                       but do not touch the file. Combined with --apply, also
                       print the sync command without running it.

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
  profiles = ["base", "ark-mlops"]   # string array of kebab-case profile names
  agents   = ["codex", "opencode"]   # optional string array, non-empty entries
  mode     = "symlink"               # optional; exactly "symlink" or "copy"
  [vars]                             # optional table of install settings
  team = "ark"                       # string / number / boolean / string array

  Repo config may only SELECT profiles for the repository. It must not carry
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

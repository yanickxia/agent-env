package cli

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/yanickxia/agent-env/internal/add"
	"github.com/yanickxia/agent-env/internal/config"
	"github.com/yanickxia/agent-env/internal/skills"
)

func newSkillsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "skills",
		Short: "Manage agent skills",
		Long: "Manage agent skills declared in the unified config's [[installs]] table.\n\n" +
			"Repo selection is discovered from the current directory up to /: every\n" +
			".agent-env.toml found is merged (nearest first). AGENT_ENV_REPO_CONFIG (or\n" +
			"the legacy AGENT_SKILLS_REPO_CONFIG) pins a single file instead of walking.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	c.AddCommand(
		newSkillsActionCmd("apply", "Install skills declared in the config (default)", skills.CmdApply),
		newSkillsActionCmd("dry-run", "Print the commands without executing them", skills.CmdDryRun),
		newSkillsActionCmd("list", "Print the current config", skills.CmdList),
		newSkillsActionCmd("profiles", "Print the distinct profiles declared by config entries (sorted)", skills.CmdProfiles),
		newSkillsActionCmd("resolve", "Read-only: show the project entries this repo would install", skills.CmdResolve),
		newSkillsActionCmd("status", "Read-only: show the stored repo-level stamps for this repo", skills.CmdStatus),
		newSkillsAddCmd(),
	)
	return c
}

func newSkillsAddCmd() *cobra.Command {
	var dryRun, noRepo bool
	c := &cobra.Command{
		Use:   "add NAME...",
		Short: "Record named skill entries in .agent-env.toml and install them",
		Long: "Resolve each NAME against the global config's [[installs]] entries (by the\n" +
			"name field only), record it in <repo>/.agent-env.toml's names list, then run\n" +
			"agent-env skills apply --non-interactive --skip-unchanged so the entry is\n" +
			"installed (npm install --save semantics).\n\n" +
			"All names are resolved before anything is modified: an unknown name fails\n" +
			"the whole command without side effects. Entries whose profiles contain the\n" +
			"reserved \"global\" keyword install everywhere already, so they are noted but\n" +
			"not recorded. --dry-run prints the planned config change and install\n" +
			"commands without touching anything.\n\n" +
			"add requires a repo context: outside a repository (or with --no-repo) it\n" +
			"fails; use `agent-env skills apply --no-repo` for global-only installs.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			so := resolveOptions()
			getenv := os.Getenv
			wd := so.StartDir
			inRepo := config.InGitRepo(wd) ||
				getenv("AGENT_ENV_REPO_CONFIG") != "" ||
				getenv("AGENT_SKILLS_REPO_CONFIG") != ""
			opts := add.Options{
				Domain:         add.DomainSkills,
				ManifestPath:   so.ManifestPath,
				StatePath:      so.StatePath,
				RepoConfigPath: so.RepoConfigPath,
				ProjectRoot:    so.ProjectRoot,
				StartDir:       so.StartDir,
				Home:           so.Home,
				Stdout:         os.Stdout,
				Stderr:         os.Stderr,
				LookupEnv:      getenv,
				InRepo:         inRepo,
			}
			fl := add.Filters{Names: flattenComma(args), DryRun: dryRun, NoRepo: noRepo}
			if err := add.Run(opts, fl); err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print the config change and install commands without writing or installing")
	c.Flags().BoolVar(&noRepo, "no-repo", false, "rejected: add records into a repo config and needs a repo context")
	return c
}

func newSkillsActionCmd(use, short, command string) *cobra.Command {
	var f filterFlags
	c := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := resolveOptions()
			opts.Force = f.force
			opts.Prune = f.prune
			fl := f.toFilters(cmd, command)
			if err := skills.Run(opts, fl); err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			return nil
		},
	}
	addFilterFlags(c, &f)
	return c
}

func resolveOptions() skills.Options {
	getenv := os.Getenv
	home := config.DefaultHome()
	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}
	root := config.ProjectRoot(wd)
	return skills.Options{
		ManifestPath:   config.ManifestPath(getenv, home),
		StatePath:      config.StatePath(getenv, home),
		RepoConfigPath: config.RepoConfigPath(getenv, root),
		ProjectRoot:    root,
		StartDir:       wd,
		Home:           home,
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
	}
}

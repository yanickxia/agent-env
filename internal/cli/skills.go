package cli

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/yanickxia/agent-env/internal/config"
	"github.com/yanickxia/agent-env/internal/skills"
)

func newSkillsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "skills",
		Short: "Manage agent skills",
		Long:  "Manage agent skills declared in the unified config's [[installs]] table.",
		Args:  cobra.NoArgs,
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
		newSkillsActionCmd("status", "Read-only: show the stored project-scope stamps for this repo", skills.CmdStatus),
	)
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
		Home:           home,
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
	}
}

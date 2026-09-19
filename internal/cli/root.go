// Package cli is the cobra command tree for agent-env.
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/yanickxia/agent-env/internal/version"
)

// Execute runs the command tree and returns the process exit code.
func Execute() int {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		var ee *ExitError
		if errors.As(err, &ee) {
			if ee.Msg != "" {
				fmt.Fprintln(os.Stderr, ee.Msg)
			}
			return ee.Code
		}
		fmt.Fprintf(os.Stderr, "agent-env: %v\n", err)
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "agent-env",
		Short: "Unified manager for agent skills and MCP servers",
		Long: "agent-env manages agent skills ([[installs]]) and MCP servers ([[servers]])\n" +
			"from one binary and one unified config:\n\n" +
			"  agent-env apply     run both domains (skills, then MCP)\n" +
			"  agent-env skills    manage skills only\n" +
			"  agent-env mcp       manage MCP servers only\n" +
			"  agent-env init      create or update the repo-level .agent-env.toml",
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       version.Version,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	root.CompletionOptions.DisableDefaultCmd = true

	root.AddCommand(
		newAllCmd("apply", "Run skills and MCP sync (skills first, then MCP)"),
		newAllCmd("dry-run", "Print skills and MCP commands without executing them"),
		newSkillsCmd(),
		newMCPCmd(),
		newInitCmd(),
		newVersionCmd(),
	)
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the agent-env version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "agent-env %s\n", version.Version)
			return nil
		},
	}
}

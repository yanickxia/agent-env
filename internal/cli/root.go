// Package cli is the cobra command tree for agent-env.
package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yanickxia/agent-env/internal/updatecheck"
	"github.com/yanickxia/agent-env/internal/version"
)

// Execute runs the command tree and returns the process exit code.
func Execute() int {
	root := newRootCmd()

	// Start the update check before the command runs; it is best-effort and
	// only ever writes to stderr, so stdout pipes (upsert-stdin) stay clean.
	noticeCh := updatecheck.Start(updatecheck.Options{
		Current: version.Version,
		Skip:    isUpdateCommand(os.Args),
	})

	err := root.Execute()
	printUpdateNotice(noticeCh)

	if err != nil {
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

// isUpdateCommand reports whether the invocation is `agent-env update`, which
// must not trigger a check for an even newer release.
func isUpdateCommand(args []string) bool {
	for _, a := range args[1:] {
		if a == "update" {
			return true
		}
		if !strings.HasPrefix(a, "-") {
			return false
		}
	}
	return false
}

// printUpdateNotice waits briefly for the async check and prints at most one
// line to stderr. It never affects the exit code.
func printUpdateNotice(ch <-chan string) {
	select {
	case msg := <-ch:
		if msg != "" {
			fmt.Fprintln(os.Stderr, msg)
		}
	case <-time.After(300 * time.Millisecond):
	}
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
			"  agent-env init      create or update the repo-level .agent-env.toml\n" +
			"  agent-env update    upgrade this binary to the latest release",
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
		newUpdateCmd(),
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

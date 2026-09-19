package cli

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/yanickxia/agent-env/internal/selfupdate"
	"github.com/yanickxia/agent-env/internal/version"
)

func newUpdateCmd() *cobra.Command {
	var target string
	c := &cobra.Command{
		Use:   "update [--version vX.Y.Z]",
		Short: "Download and replace this binary with a newer release",
		Long: "Download the agent-env release for this platform, verify its sha256, and\n" +
			"atomically replace the running binary. Without --version the latest release\n" +
			"is used. This is the day-to-day upgrade path; install.zsh remains the\n" +
			"bootstrap installer.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := selfupdate.Options{
				Current: version.Version,
				Version: target,
				Stdout:  cmd.OutOrStdout(),
				Getenv:  os.Getenv,
			}
			if err := selfupdate.Run(opts); err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			return nil
		},
	}
	c.Flags().StringVar(&target, "version", "", "install a specific tag instead of the latest (e.g. v0.3.0)")
	return c
}

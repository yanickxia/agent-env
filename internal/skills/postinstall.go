package skills

import (
	"fmt"
	"os/exec"

	"github.com/yanickxia/agent-env/internal/config"
)

// runPostInstall executes the per-entry dependency hooks via `zsh -c`,
// deduplicated per entry and failure-counted. dry-run only prints them.
func (r *runner) runPostInstall(mode, source string, hooks []config.PostInstall) {
	if len(hooks) == 0 {
		return
	}
	payload := config.Install{PostInstall: hooks}.PostInstallPayload()
	key := source + config.SepGS + payload
	if r.postInstallDone[key] {
		return
	}
	r.postInstallDone[key] = true

	for _, hook := range hooks {
		run := hook.Run
		guard := hook.IfMissing

		if guard != "" {
			if _, err := lookPath(guard); err == nil {
				fmt.Fprintf(r.out, "# post_install: %s already present, skipping: %s\n", guard, run)
				continue
			}
		}

		printCommand(r.out, []string{"zsh", "-c", run})

		if mode != CmdApply {
			continue
		}

		cmd := exec.Command("zsh", "-c", run)
		cmd.Stdout = r.out
		cmd.Stderr = r.errw
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(r.errw, "%s: post_install failed for %s: %s\n", config.Prog, source, run)
			r.postInstallFailures++
		}
	}
}

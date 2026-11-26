package skills

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/yanickxia/agent-env/internal/config"
)

// hookShells is the search order for the shell that runs post_install hooks.
// zsh comes first so machines that have it keep the historical behaviour;
// bash and then sh cover minimal systems such as a fresh Debian. Hooks should
// be written in POSIX sh so they behave the same under all three.
var hookShells = []string{"zsh", "bash", "sh"}

// resolveHookShell returns the name and full path of the first shell in
// hookShells found on PATH, memoized per run. An empty path means none was
// found.
func (r *runner) resolveHookShell() (name, path string) {
	if r.hookShellResolved {
		return r.hookShellName, r.hookShellPath
	}
	r.hookShellResolved = true
	for _, name := range hookShells {
		if path, err := lookPath(name); err == nil {
			r.hookShellName, r.hookShellPath = name, path
			break
		}
	}
	return r.hookShellName, r.hookShellPath
}

// runPostInstall executes the per-entry dependency hooks, deduplicated per
// entry and failure-counted. dry-run only prints them.
//
// Trust model: hooks from the global manifest are the user's own declarations
// and always run. Hooks declared inline in a .agent-env.toml (entry.RepoPath
// is set) come from a checked-in file or any ancestor directory, so they only
// run under --allow-hooks; otherwise each is listed as skipped on stdout and
// summarized on stderr, without failing the run.
func (r *runner) runPostInstall(mode, source string, entry config.Install) {
	hooks := entry.PostInstall
	if len(hooks) == 0 {
		return
	}
	payload := entry.PostInstallPayload()
	key := source + config.SepGS + payload
	if r.postInstallDone[key] {
		return
	}
	r.postInstallDone[key] = true

	allowed := entry.RepoPath == "" || r.f.AllowHooks

	for _, hook := range hooks {
		run := hook.Run
		guard := hook.IfMissing

		if guard != "" {
			if _, err := lookPath(guard); err == nil {
				fmt.Fprintf(r.out, "# post_install: %s already present, skipping: %s\n", guard, run)
				continue
			}
		}

		if !allowed {
			fmt.Fprintf(r.out, "# post_install: skipped (declared in %s; pass --allow-hooks to run): %s\n", entry.RepoPath, run)
			r.postInstallSkipped++
			continue
		}

		name, path := r.resolveHookShell()
		if path == "" {
			if !r.hookShellWarned {
				r.hookShellWarned = true
				fmt.Fprintf(r.errw, "%s: post_install needs a shell; none of %s found on PATH\n", config.Prog, strings.Join(hookShells, ", "))
			}
			fmt.Fprintf(r.out, "# post_install: cannot run without a shell: %s\n", run)
			if mode == CmdApply {
				r.postInstallFailures++
			}
			continue
		}

		printCommand(r.out, []string{name, "-c", run})

		if mode != CmdApply {
			continue
		}

		cmd := exec.Command(path, "-c", run)
		cmd.Stdout = r.out
		cmd.Stderr = r.errw
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(r.errw, "%s: post_install failed for %s (%s -c): %s\n", config.Prog, source, name, run)
			r.postInstallFailures++
		}
	}
}

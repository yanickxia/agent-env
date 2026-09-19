package skills

import (
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/yanickxia/agent-env/internal/config"
)

// lookPath is exec.LookPath, indirected for tests.
var lookPath = exec.LookPath

type entryCommands struct {
	npx           []string
	aiden         []string
	envArgs       []string
	claudeSkipped bool
}

// buildCommands constructs the installer command lines for one entry,
// matching the zsh process_manifest logic (agents, skills, user -g, mode,
// the claude-code symlink special case, env prefixing, and the aiden path).
func (r *runner) buildCommands(source string, entry config.Install, agents, skills []string) (entryCommands, error) {
	ec := entryCommands{envArgs: entry.EnvArgs()}

	cmd := []string{"npx", "--yes", "skills", "add", source}
	if entry.Global {
		cmd = append(cmd, "-g")
	}

	switch entry.Mode {
	case "", "symlink":
		// default
	case "copy":
		cmd = append(cmd, "--copy")
	default:
		return ec, fmt.Errorf("%s: invalid mode '%s' for source: %s", config.Prog, entry.Mode, source)
	}

	wantsAiden := false
	for _, agent := range agents {
		if agent == "" {
			continue
		}
		switch {
		case agent == "aiden":
			wantsAiden = true
		case agent == "claude-code" && r.claudeSkillsIsSymlink:
			ec.claudeSkipped = true
			r.printClaudeSkipNotice()
		default:
			cmd = append(cmd, "-a", agent)
		}
	}

	if wantsAiden {
		aiden := []string{"aiden", "skills", "add", source}
		if entry.Global {
			aiden = append(aiden, "-g")
		}
		for _, skill := range skills {
			if skill == "" {
				continue
			}
			if skill == "*" {
				break
			}
			aiden = append(aiden, "--skill", skill)
		}
		aiden = append(aiden, "-y")
		ec.aiden = aiden
	}

	for _, skill := range skills {
		if skill == "" {
			continue
		}
		cmd = append(cmd, "--skill", skill)
	}
	cmd = append(cmd, "-y")

	ec.npx = cmd
	return ec, nil
}

func (r *runner) printClaudeSkipNotice() {
	if r.claudeSkipNoticeShown {
		return
	}
	r.claudeSkipNoticeShown = true
	fmt.Fprintf(r.errw, "%s: ~/.claude/skills is a symlink to the skills store; not passing -a claude-code (claude sees the store directly)\n", config.Prog)
}

// printScopedCommand prefixes the line with `cd <root> && ` for repo-level
// (non-global) entries, whose installers run inside the repo.
func (r *runner) printScopedCommand(global bool, projectRoot string, args []string) {
	if !global {
		fmt.Fprintf(r.out, "cd %s && ", zshQuote(projectRoot))
		printCommand(r.out, args)
		return
	}
	printCommand(r.out, args)
}

// runScopedCommand runs repo-level entries inside the repo root; global entries
// run in the current directory.
func (r *runner) runScopedCommand(global bool, projectRoot string, args []string) error {
	if len(args) == 0 {
		return nil
	}
	cmd := exec.Command(args[0], args[1:]...)
	if !global {
		cmd.Dir = projectRoot
	}
	cmd.Stdout = r.out
	cmd.Stderr = r.errw
	return cmd.Run()
}

// printCommand renders args the way zsh `printf '%q '` does: a trailing space
// after every argument, then a newline.
func printCommand(w io.Writer, args []string) {
	for _, a := range args {
		fmt.Fprintf(w, "%s ", zshQuote(a))
	}
	fmt.Fprintln(w)
}

const zshSafe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-"

// zshQuote approximates zsh's %q quoting: safe characters pass through, every
// other character is backslash-escaped. The commands agent-env emits are all
// in the safe set except glob metacharacters such as '*'.
func zshQuote(s string) string {
	if s == "" {
		return "''"
	}
	needs := false
	for _, r := range s {
		if !strings.ContainsRune(zshSafe, r) {
			needs = true
			break
		}
	}
	if !needs {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(zshSafe, r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('\\')
			b.WriteRune(r)
		}
	}
	return b.String()
}

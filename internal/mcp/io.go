package mcp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// printCommand renders args with zsh '%q'-style quoting, applying redaction
// first so secrets never leak into dry-run output.
func (r *runner) printCommand(args []string) {
	for _, a := range args {
		fmt.Fprintf(r.out, "%s ", zshQuote(r.redact(a)))
	}
	fmt.Fprintln(r.out)
}

// printBlock prints a rendered config block the way zsh does through command
// substitution + `print -r --`: redacted, trailing newlines stripped, and
// terminated with exactly one newline.
func (r *runner) printBlock(text string) {
	fmt.Fprintln(r.out, strings.TrimRight(r.redact(text), "\n"))
}

func (r *runner) runCommand(args []string) error {
	if len(args) == 0 {
		return nil
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = r.opts.WorkDir
	cmd.Stdout = r.out
	cmd.Stderr = r.errw
	return cmd.Run()
}

var lookPath = exec.LookPath

const zshSafe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-"

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

func writeProjectBlock(path, rendered, begin, end string) error {
	old := readFileOrEmpty(path)
	merged := upsertManagedBlock(old, rendered, begin, end)
	return writePreservingMode(path, []byte(merged), 0o644)
}

func writeOpencodeProjectBlock(path, rendered string) error {
	old := readFileOrEmpty(path)
	merged := upsertOpencodeManagedBlock(old, rendered)
	return writePreservingMode(path, []byte(merged), 0o644)
}

func writePreservingMode(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("%s: cannot create %s: %v", "agent-env", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return fmt.Errorf("%s: cannot write %s: %v", "agent-env", path, err)
	}
	return nil
}

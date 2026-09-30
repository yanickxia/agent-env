package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func findSub(t *testing.T, cmd *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, sub := range cmd.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	t.Fatalf("subcommand %q not found under %s", name, cmd.Name())
	return nil
}

func TestAddIsRegisteredOnBothDomains(t *testing.T) {
	root := newRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil { // root with no args prints help
		t.Fatalf("root help failed: %v", err)
	}
	skillsCmd := findSub(t, root, "skills")
	mcpCmd := findSub(t, root, "mcp")
	if got := findSub(t, skillsCmd, "add"); got == nil {
		t.Fatal("skills add missing")
	}
	if got := findSub(t, mcpCmd, "add"); got == nil {
		t.Fatal("mcp add missing")
	}
}

func TestAddRequiresAtLeastOneName(t *testing.T) {
	for _, args := range [][]string{{"skills", "add"}, {"mcp", "add"}} {
		root := newRootCmd()
		var out, errb bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&errb)
		root.SetArgs(args)
		if err := root.Execute(); err == nil {
			t.Fatalf("%v without a NAME must fail", args)
		} else if !strings.Contains(err.Error(), "at least 1 arg") {
			t.Fatalf("%v error must be arg-count validation, got: %v", args, err)
		}
	}
}

func TestAddHelpRenders(t *testing.T) {
	for _, args := range [][]string{{"skills", "add", "--help"}, {"mcp", "add", "--help"}} {
		root := newRootCmd()
		var out, errb bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&errb)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v help failed: %v", args, err)
		}
		rendered := out.String()
		if !strings.Contains(rendered, "add NAME...") || !strings.Contains(rendered, "--dry-run") {
			t.Fatalf("%v help must document NAME args and --dry-run, got:\n%s", args, rendered)
		}
	}
}

package skills

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yanickxia/agent-env/internal/config"
)

func reflectEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBuildCommands(t *testing.T) {
	cases := []struct {
		name        string
		entry       config.Install
		agents      []string
		skills      []string
		symlink     bool
		wantNpx     []string
		wantAiden   []string
		wantSkipped bool
		wantEnv     []string
	}{
		{
			name:    "global symlink default",
			entry:   config.Install{Source: "example/x", Global: true, Mode: "symlink"},
			agents:  []string{"codex"},
			skills:  []string{"a"},
			wantNpx: []string{"npx", "--yes", "skills", "add", "example/x", "-g", "-a", "codex", "--skill", "a", "-y"},
		},
		{
			name:    "repo-level copy",
			entry:   config.Install{Source: "example/x", Mode: "copy"},
			agents:  []string{"codex"},
			skills:  []string{"a"},
			wantNpx: []string{"npx", "--yes", "skills", "add", "example/x", "--copy", "-a", "codex", "--skill", "a", "-y"},
		},
		{
			name:    "wildcard passes through",
			entry:   config.Install{Source: "example/x", Global: true, Mode: "symlink"},
			agents:  []string{"codex"},
			skills:  []string{"*"},
			wantNpx: []string{"npx", "--yes", "skills", "add", "example/x", "-g", "-a", "codex", "--skill", "*", "-y"},
		},
		{
			name: "env args preserved",
			entry: config.Install{
				Source: "example/x", Global: true, Mode: "symlink",
				Env: []config.EnvVar{{Key: "K", Value: "V"}},
			},
			agents:  []string{"codex"},
			skills:  nil,
			wantNpx: []string{"npx", "--yes", "skills", "add", "example/x", "-g", "-a", "codex", "-y"},
			wantEnv: []string{"K=V"},
		},
		{
			name:      "aiden uses its own CLI",
			entry:     config.Install{Source: "example/x", Global: true, Mode: "symlink"},
			agents:    []string{"aiden", "codex"},
			skills:    []string{"a"},
			wantNpx:   []string{"npx", "--yes", "skills", "add", "example/x", "-g", "-a", "codex", "--skill", "a", "-y"},
			wantAiden: []string{"aiden", "skills", "add", "example/x", "-g", "--skill", "a", "-y"},
		},
		{
			name:        "claude-code skipped when store is a symlink",
			entry:       config.Install{Source: "example/x", Global: true, Mode: "symlink"},
			agents:      []string{"claude-code", "codex"},
			skills:      []string{"a"},
			symlink:     true,
			wantNpx:     []string{"npx", "--yes", "skills", "add", "example/x", "-g", "-a", "codex", "--skill", "a", "-y"},
			wantSkipped: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			r := &runner{out: &out, errw: &errb, opts: Options{Home: "/home/u"}}
			r.claudeSkillsIsSymlink = tc.symlink

			ec, err := r.buildCommands(tc.entry.Source, tc.entry, tc.agents, tc.skills)
			if err != nil {
				t.Fatalf("buildCommands error: %v", err)
			}
			if !reflectEqual(ec.npx, tc.wantNpx) {
				t.Fatalf("npx cmd:\nwant %q\ngot  %q", tc.wantNpx, ec.npx)
			}
			if !reflectEqual(ec.aiden, tc.wantAiden) {
				t.Fatalf("aiden cmd:\nwant %q\ngot  %q", tc.wantAiden, ec.aiden)
			}
			if !reflectEqual(ec.envArgs, tc.wantEnv) {
				t.Fatalf("env args: want %q got %q", tc.wantEnv, ec.envArgs)
			}
			if ec.claudeSkipped != tc.wantSkipped {
				t.Fatalf("claudeSkipped = %v, want %v", ec.claudeSkipped, tc.wantSkipped)
			}
			if tc.symlink && !strings.Contains(errb.String(), "symlink to the skills store") {
				t.Fatalf("expected claude symlink notice, stderr=%q", errb.String())
			}
		})
	}
}

func TestBuildCommandsInvalidMode(t *testing.T) {
	var out, errb bytes.Buffer
	r := &runner{out: &out, errw: &errb, opts: Options{Home: "/home/u"}}
	_, err := r.buildCommands("example/x", config.Install{Source: "example/x", Global: true, Mode: "bogus"}, []string{"codex"}, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid mode 'bogus'") {
		t.Fatalf("want invalid mode error, got %v", err)
	}
}

// A symlinked ~/.claude/skills must suppress claude-code in a real dry-run.
func TestDryRunClaudeSymlinkViaTempDir(t *testing.T) {
	f := newFixture(t)
	store := filepath.Join(f.dir, "central-skills")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(f.home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(store, filepath.Join(f.home, ".claude", "skills")); err != nil {
		t.Fatal(err)
	}

	f.writeManifest(`
[[installs]]
source = "example/x"
agents = ["claude-code", "codex"]
skills = ["lane"]
profiles = ["global"]
`)
	out, errb, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if strings.Contains(out, "-a claude-code") {
		t.Fatalf("claude-code must be suppressed:\n%s", out)
	}
	if !strings.Contains(out, "-a codex") {
		t.Fatalf("codex must remain:\n%s", out)
	}
	if !strings.Contains(errb, "symlink to the skills store") {
		t.Fatalf("expected notice on stderr, got %q", errb)
	}
}

func TestZshQuote(t *testing.T) {
	cases := map[string]string{
		"plain":        "plain",
		"a/b:c@d.e":    "a/b:c@d.e",
		"*":            "\\*",
		"a b":          "a\\ b",
		"":             "''",
		"brave-search": "brave-search",
		"git@code.byted.org:byteapi/bytedcli.git": "git@code.byted.org:byteapi/bytedcli.git",
	}
	for in, want := range cases {
		if got := zshQuote(in); got != want {
			t.Fatalf("zshQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

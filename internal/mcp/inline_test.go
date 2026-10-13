package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const inlineMCP = `
agents = ["codex", "pi", "omp", "opencode", "trae"]
[[servers]]
name = "local"
command = "local-command"
args = ["${TOKEN}"]
env = { API_KEY = "${TOKEN}" }
`

func TestInlineMCPApplyWithoutGlobalManifestPreservesUserConfigs(t *testing.T) {
	for _, body := range []string{"", "installs = []"} {
		t.Run(body, func(t *testing.T) {
			f := newFixture(t)
			if body != "" {
				f.writeManifest(body)
			}
			f.writeRepo(inlineMCP)
			f.writeSecrets(`token = "local-secret"`)
			userFiles := []string{
				filepath.Join(f.home, ".codex", "config.toml"),
				filepath.Join(f.home, ".pi", "agent", "mcp.json"),
				filepath.Join(f.home, ".omp", "agent", "mcp.json"),
				filepath.Join(f.home, ".trae", "traecli.yaml"),
				filepath.Join(f.home, ".config", "opencode", "opencode.jsonc"),
				f.claude,
			}
			for _, path := range userFiles {
				f.write(path, "user config must stay byte-for-byte unchanged\n")
			}
			out, _, err := f.run(Filters{Command: CmdDryRun})
			if err != nil {
				t.Fatal(err)
			}
			assertNotContains(t, out, "local-secret")
			assertContains(t, out, "***redacted***")
			_, _, err = f.run(Filters{Command: CmdApply, NonInteractive: true, Agents: []string{"codex", "pi", "omp", "opencode", "trae"}, AgentSeen: true})
			if err != nil {
				t.Fatal(err)
			}
			for _, rel := range []string{".codex/config.toml", ".pi/mcp.json", ".omp/mcp.json", ".opencode/opencode.jsonc", ".trae/traecli.yaml"} {
				content := readFile(t, filepath.Join(f.dir, rel))
				assertContains(t, content, "local-command")
				assertContains(t, content, "local-secret")
			}
			for _, path := range userFiles {
				if got := readFile(t, path); got != "user config must stay byte-for-byte unchanged\n" {
					t.Fatalf("local-only run modified %s: %s", path, got)
				}
			}
		})
	}
}

func TestInlineMCPSelectionAndFilters(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    Filters
		want bool
	}{
		{"automatic", Filters{Command: CmdDryRun}, true},
		{"profile", Filters{Command: CmdDryRun, Profiles: []string{"other"}, ProfileSeen: true}, true},
		{"matching name", Filters{Command: CmdDryRun, Names: []string{"local"}}, true},
		{"other name", Filters{Command: CmdDryRun, Names: []string{"other"}}, false},
		{"agent", Filters{Command: CmdDryRun, Agents: []string{"opencode"}, AgentSeen: true}, true},
		{"no repo", Filters{Command: CmdDryRun, NoRepo: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.writeRepo(inlineMCP)
			f.writeManifest("servers = []")
			out, errb, err := f.run(tc.f)
			if err != nil || strings.Contains(out, "local-command") != tc.want || strings.Contains(errb, "no .agent-env.toml") {
				t.Fatalf("out=%s stderr=%s err=%v", out, errb, err)
			}
			if tc.name == "agent" {
				assertNotContains(t, out, "Codex project MCP")
			}
		})
	}
}

func TestInlineMCPLayerOverridesAndGlobalIsolation(t *testing.T) {
	f := newFixture(t)
	child := filepath.Join(f.dir, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	f.writeRepo(`
agents = ["codex"]
[[servers]]
name = "same"
command = "parent-command"
`)
	f.write(filepath.Join(child, ".agent-env.toml"), `
[[servers]]
name = "same"
command = "child-command"
`)
	f.writeManifest(`
[[servers]]
name = "same"
agents = ["codex"]
command = "manifest-command"
profiles = ["base"]
[[servers]]
name = "same"
agents = ["codex"]
command = "global-command"
profiles = ["global"]
`)
	opts := f.opts
	opts.ProjectRoot = child
	opts.WorkDir = child
	opts.RepoConfigPath = filepath.Join(child, ".agent-env.toml")
	_, _, err := f.runWith(opts, Filters{Command: CmdApply, NonInteractive: true, Profiles: []string{"base"}})
	if err != nil {
		t.Fatal(err)
	}
	project := readFile(t, filepath.Join(child, ".codex", "config.toml"))
	assertContains(t, project, "child-command")
	assertNotContains(t, project, "parent-command")
	assertNotContains(t, project, "manifest-command")
	user := readFile(t, filepath.Join(f.home, ".codex", "config.toml"))
	assertContains(t, user, "global-command")
	assertNotContains(t, user, "child-command")
	// chezmoi's user-only pipe never loads inline definitions.
	out, _, err := f.runWith(opts, Filters{Command: CmdUpsertStdin, Agents: []string{"codex"}})
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, out, "global-command")
	assertNotContains(t, out, "child-command")
}

func TestInlineMCPCLIProjectScope(t *testing.T) {
	f := newFixture(t)
	f.writeRepo(`
agents = ["claude", "aiden"]
[[servers]]
name = "local"
command = "local-command"
`)
	log := filepath.Join(f.dir, "cli.log")
	for _, name := range []string{"claude", "aiden"} {
		installLogStub(t, filepath.Join(f.dir, "stub"), name, log)
	}
	_, _, err := f.run(Filters{Command: CmdApply, NonInteractive: true})
	if err != nil {
		t.Fatal(err)
	}
	calls := readFile(t, log)
	assertContains(t, calls, "mcp add local --scope project -- local-command")
	assertContains(t, calls, "mcp add -s project local -- local-command")
	assertNotContains(t, calls, "global")
}

package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const nameServerManifest = `
[[servers]]
name = "playwright"
agents = ["codex"]
type = "stdio"
command = "npx"

[[servers]]
name = "browserless"
agents = ["codex"]
type = "stdio"
command = "npx"

[[servers]]
name = "bytedance-srv"
agents = ["codex"]
type = "stdio"
command = "npx"
profiles = ["bytedance"]
`

func TestOrphanServersSkippedWithoutSelection(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(nameServerManifest)
	out, errb, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	for _, absent := range []string{"playwright", "browserless", "bytedance-srv"} {
		if strings.Contains(out, absent) {
			t.Fatalf("%s must be skipped without selection:\n%s", absent, out)
		}
	}
	if !strings.Contains(errb, "skipped 3 repo-level servers") {
		t.Fatalf("want 3 skips, got %q", errb)
	}
}

func TestRepoNamesSelectOrphanServers(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeRepo("names = [\"playwright\"]\n")
	f.writeManifest(nameServerManifest)
	out, _, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, out, "playwright")
	assertNotContains(t, out, "browserless")
	assertNotContains(t, out, "bytedance-srv")
}

func TestRepoProfilesAndNamesCombinedMCP(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeRepo("profiles = [\"bytedance\"]\nnames = [\"browserless\"]\n")
	f.writeManifest(nameServerManifest)
	out, _, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, out, "bytedance-srv")
	assertContains(t, out, "browserless")
	assertNotContains(t, out, "playwright")
}

func TestCLINameFilterNarrowsActiveSet(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeRepo("names = [\"playwright\", \"browserless\"]\n")
	f.writeManifest(nameServerManifest)
	out, _, err := f.run(Filters{Command: CmdDryRun, Names: []string{"playwright"}, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, out, "playwright")
	assertNotContains(t, out, "browserless")
}

func TestLayeredNamesUnionMCP(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	repo := filepath.Join(parent, "repo")
	sub := filepath.Join(repo, "a")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	f.writeSecrets("")
	f.writeManifest(nameServerManifest)
	f.write(filepath.Join(parent, ".agent-env.toml"), "names = [\"browserless\"]\n")
	f.write(filepath.Join(repo, ".agent-env.toml"), "profiles = [\"bytedance\"]\n")

	opts := f.opts
	opts.ProjectRoot = repo
	opts.WorkDir = sub
	opts.RepoConfigPath = filepath.Join(repo, ".agent-env.toml")
	out, _, err := f.runWith(opts, Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	// parent name + child profile both take effect.
	assertContains(t, out, "browserless")
	assertContains(t, out, "bytedance-srv")
}

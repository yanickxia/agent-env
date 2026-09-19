package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mcpLayerManifest = `
[[servers]]
name = "srv-base"
agents = ["codex"]
type = "stdio"
command = "npx"
profiles = ["base"]

[[servers]]
name = "srv-ark"
agents = ["codex"]
type = "stdio"
command = "npx"
profiles = ["ark-mlops"]

[[servers]]
name = "srv-global"
agents = ["codex"]
type = "stdio"
command = "npx"
profiles = ["global"]
`

func TestLayeredRepoConfigMCP(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	repo := filepath.Join(parent, "repo")
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	f.writeSecrets("")
	f.writeManifest(mcpLayerManifest)
	f.write(filepath.Join(parent, ".agent-env.toml"), `profiles = ["ark-mlops"]`)
	f.write(filepath.Join(repo, ".agent-env.toml"), `profiles = ["base"]`)

	opts := f.opts
	opts.ProjectRoot = repo
	opts.WorkDir = sub
	opts.RepoConfigPath = filepath.Join(repo, ".agent-env.toml")

	out, errb, err := f.runWith(opts, Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out, "srv-base") || !strings.Contains(out, "srv-ark") {
		t.Fatalf("merged profiles must select base+ark servers:\n%s", out)
	}
	if !strings.Contains(out, "srv-global") {
		t.Fatalf("global server must install:\n%s", out)
	}
	// Project write target stays the repo root, not the cwd.
	if !strings.Contains(out, filepath.Join(repo, ".codex")) {
		t.Fatalf("project target must be the repo root:\n%s", out)
	}
	if strings.Contains(out, sub) {
		t.Fatalf("project target must not be the cwd:\n%s", out)
	}
	if strings.Contains(errb, "skipped") {
		t.Fatalf("no repo-level skips expected: %q", errb)
	}
}

func TestLayeredRepoConfigMCPNoSelection(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	f.writeSecrets("")
	f.writeManifest(mcpLayerManifest)

	opts := f.opts
	opts.ProjectRoot = repo
	opts.WorkDir = repo
	opts.RepoConfigPath = filepath.Join(repo, ".agent-env.toml")

	out, errb, err := f.runWith(opts, Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out, "srv-global") || strings.Contains(out, "srv-base") || strings.Contains(out, "srv-ark") {
		t.Fatalf("global only with no selection:\n%s", out)
	}
	if !strings.Contains(errb, "skipped 2 repo-level servers") || !strings.Contains(errb, "up to /") {
		t.Fatalf("want layered skip notice, got %q", errb)
	}
}

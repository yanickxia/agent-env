package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoRepoInstallsGlobalOnlyWithoutWarning(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(gatingManifest)

	out, errb, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true, NoRepo: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, out, "[mcp_servers.srv-global]")
	assertNotContains(t, out, "srv-tagged")
	assertNotContains(t, out, "srv-frontend")
	assertNotContains(t, errb, "skipped")
	assertNotContains(t, errb, "no .agent-env.toml")
}

func TestNoRepoApplyWritesGlobalOnly(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(gatingManifest)

	_, errb, err := f.run(Filters{Command: CmdApply, NonInteractive: true, NoRepo: true})
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	codex := readFile(t, filepath.Join(f.home, ".codex", "config.toml"))
	assertContains(t, codex, "[mcp_servers.srv-global]")
	assertNotContains(t, codex, "srv-tagged")
	assertNotContains(t, errb, "skipped")
	assertNotContains(t, errb, "no .agent-env.toml")
}

// Even when an ancestor directory carries .agent-env.toml, --no-repo must not
// read it: repo-level servers stay excluded and no skip warning is printed.
func TestNoRepoIgnoresAncestorRepoConfig(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	repo := filepath.Join(parent, "repo")
	sub := filepath.Join(repo, "a")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	f.writeSecrets("")
	f.writeManifest(mcpLayerManifest)
	f.write(filepath.Join(parent, ".agent-env.toml"), `profiles = ["base"]`)

	opts := f.opts
	opts.ProjectRoot = repo
	opts.WorkDir = sub
	opts.RepoConfigPath = filepath.Join(repo, ".agent-env.toml")

	out, errb, err := f.runWith(opts, Filters{Command: CmdDryRun, NonInteractive: true, NoRepo: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, out, "srv-global")
	assertNotContains(t, out, "srv-base")
	assertNotContains(t, out, "srv-ark")
	assertNotContains(t, errb, "skipped")
	assertNotContains(t, errb, "no .agent-env.toml")

	// Sanity: without --no-repo the ancestor config selects the base server,
	// proving the flag (not an absent config) is what excluded it.
	sel, _, err := f.runWith(opts, Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("sanity dry-run failed: %v", err)
	}
	assertContains(t, sel, "srv-base")
}

func TestNoRepoRejectsProfileCombination(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(gatingManifest)

	_, _, err := f.run(Filters{
		Command:        CmdDryRun,
		NonInteractive: true,
		NoRepo:         true,
		Profiles:       []string{"base"},
		ProfileSeen:    true,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined with --profile") {
		t.Fatalf("want no-repo/profile combination error, got %v", err)
	}
}

func TestNoRepoRejectedByReadOnlyCommands(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(gatingManifest)

	if _, _, err := f.run(Filters{Command: CmdList, NoRepo: true}); err == nil || !strings.Contains(err.Error(), "not list") {
		t.Fatalf("list must reject --no-repo, got %v", err)
	}
	if _, _, err := f.run(Filters{Command: CmdProfiles, NoRepo: true}); err == nil || !strings.Contains(err.Error(), "not profiles") {
		t.Fatalf("profiles must reject --no-repo, got %v", err)
	}
	if _, _, err := f.run(Filters{Command: CmdUpsertStdin, NoRepo: true, Agents: []string{"codex"}}); err == nil || !strings.Contains(err.Error(), "does not accept filters") {
		t.Fatalf("upsert-stdin must reject --no-repo, got %v", err)
	}
}

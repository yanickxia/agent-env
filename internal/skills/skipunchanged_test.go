package skills

import (
	"path/filepath"
	"strings"
	"testing"
)

const skipManifest = `
[[installs]]
source = "example/ark"
agents = ["codex", "opencode"]
skills = ["mlops-lane"]
scope = "project"
profiles = ["ark-mlops"]
`

func TestSkipUnchangedFullFlow(t *testing.T) {
	f := newFixture(t)
	log := filepath.Join(f.dir, "npx.log")
	t.Setenv("NPX_LOG", log)

	f.writeRepo(`profiles = ["ark-mlops"]`)
	f.writeManifest(skipManifest)

	fl := Filters{
		Command:        CmdApply,
		Scopes:         []string{"project"},
		ScopeSeen:      true,
		NonInteractive: true,
		SkipUnchanged:  true,
	}

	if _, _, err := f.run(fl); err != nil {
		t.Fatalf("first apply failed: %v", err)
	}
	if got := lineCount(t, log); got != 1 {
		t.Fatalf("npx calls after first apply = %d, want 1", got)
	}
	state := readFileOrEmpty(t, f.state)
	if !strings.Contains(state, "example/ark") {
		t.Fatalf("state file missing entry:\n%s", state)
	}
	if !strings.Contains(state, "project:"+f.dir) {
		t.Fatalf("project stamp must be keyed by repo root:\n%s", state)
	}

	out, _, err := f.run(fl)
	if err != nil {
		t.Fatalf("second apply failed: %v", err)
	}
	if !strings.Contains(out, "skip (unchanged)") {
		t.Fatalf("second run must skip:\n%s", out)
	}
	if got := lineCount(t, log); got != 1 {
		t.Fatalf("npx calls after second apply = %d, want 1", got)
	}
}

func TestDryRunNeverWritesStamp(t *testing.T) {
	f := newFixture(t)
	log := filepath.Join(f.dir, "npx.log")
	t.Setenv("NPX_LOG", log)

	f.writeRepo(`profiles = ["ark-mlops"]`)
	f.writeManifest(skipManifest)

	_, _, err := f.run(Filters{
		Command:        CmdDryRun,
		Scopes:         []string{"project"},
		ScopeSeen:      true,
		NonInteractive: true,
		SkipUnchanged:  true,
	})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if got := lineCount(t, log); got != 0 {
		t.Fatalf("dry-run must not invoke npx, got %d calls", got)
	}
	if state := readFileOrEmpty(t, f.state); state != "" {
		t.Fatalf("dry-run must not write a stamp:\n%s", state)
	}
}

func TestStampPerRepoRoot(t *testing.T) {
	f := newFixture(t)
	log := filepath.Join(f.dir, "npx.log")
	t.Setenv("NPX_LOG", log)

	f.writeManifest(skipManifest)
	fl := Filters{
		Command:        CmdApply,
		Scopes:         []string{"project"},
		ScopeSeen:      true,
		NonInteractive: true,
		SkipUnchanged:  true,
	}

	// Repo A.
	repoA := filepath.Join(f.dir, "repoA")
	f.write(filepath.Join(repoA, ".agent-env.toml"), `profiles = ["ark-mlops"]`)
	optsA := f.opts
	optsA.ProjectRoot = repoA
	optsA.RepoConfigPath = filepath.Join(repoA, ".agent-env.toml")
	if _, _, err := f.runWith(optsA, fl); err != nil {
		t.Fatalf("repoA apply failed: %v", err)
	}

	// Repo B must not match repoA's stamp.
	repoB := filepath.Join(f.dir, "repoB")
	f.write(filepath.Join(repoB, ".agent-env.toml"), `profiles = ["ark-mlops"]`)
	optsB := f.opts
	optsB.ProjectRoot = repoB
	optsB.RepoConfigPath = filepath.Join(repoB, ".agent-env.toml")
	out, _, err := f.runWith(optsB, fl)
	if err != nil {
		t.Fatalf("repoB apply failed: %v", err)
	}
	if strings.Contains(out, "skip (unchanged)") {
		t.Fatalf("a different repo root must not match:\n%s", out)
	}
	if got := lineCount(t, log); got != 2 {
		t.Fatalf("npx calls = %d, want 2", got)
	}

	state := readFileOrEmpty(t, f.state)
	if !strings.Contains(state, "project:"+repoA) || !strings.Contains(state, "project:"+repoB) {
		t.Fatalf("expected per-repo stamps:\n%s", state)
	}

	// Returning to repoA skips again.
	out, _, err = f.runWith(optsA, fl)
	if err != nil {
		t.Fatalf("repoA re-apply failed: %v", err)
	}
	if !strings.Contains(out, "skip (unchanged)") {
		t.Fatalf("repoA should skip:\n%s", out)
	}
	if got := lineCount(t, log); got != 2 {
		t.Fatalf("npx calls = %d, want 2", got)
	}
}

package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLockFile mirrors the skills CLI lock shape for a source -> skills map.
func writeLockFile(t *testing.T, path string, bySource map[string][]string) {
	t.Helper()
	skills := map[string]map[string]string{}
	for src, names := range bySource {
		for _, n := range names {
			skills[n] = map[string]string{"source": src, "sourceType": "github"}
		}
	}
	body := map[string]any{"version": 3, "skills": skills, "dismissed": map[string]any{}}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdirs(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(root, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be gone, err=%v", path, err)
	}
}

const twoGlobals = `
[[installs]]
source = "example/a"
agents = ["codex"]
skills = ["sa"]
profiles = ["global"]

[[installs]]
source = "example/b"
agents = ["codex"]
skills = ["sb"]
profiles = ["global"]
`

const oneGlobalA = `
[[installs]]
source = "example/a"
agents = ["codex"]
skills = ["sa"]
profiles = ["global"]
`

func TestPruneNoRepoRemovesRetiredSource(t *testing.T) {
	f := newFixture(t)
	storeDir := filepath.Join(f.home, ".agents", "skills")
	mkdirs(t, storeDir, "sa", "sb")
	writeLockFile(t, filepath.Join(f.home, ".agents", ".skill-lock.json"), map[string][]string{
		"example/a": {"sa"},
		"example/b": {"sb"},
	})
	f.write(f.state, "example/a\tuser\tSIGA\nexample/b\tuser\tSIGB\n")
	f.writeManifest(twoGlobals)

	// Retire example/b by removing it from the manifest.
	f.writeManifest(oneGlobalA)

	opts := f.opts
	opts.Prune = true
	out, _, err := f.runWith(opts, Filters{Command: CmdApply, NonInteractive: true, NoRepo: true})
	if err != nil {
		t.Fatalf("prune apply failed: %v", err)
	}
	if !strings.Contains(out, "pruned: sb (source example/b) -> "+filepath.Join(storeDir, "sb")) {
		t.Fatalf("missing prune line:\n%s", out)
	}
	mustNotExist(t, filepath.Join(storeDir, "sb"))
	mustExist(t, filepath.Join(storeDir, "sa"))

	state := readFileOrEmpty(t, f.state)
	if strings.Contains(state, "example/b") {
		t.Fatalf("retired stamp must be removed:\n%s", state)
	}
	if !strings.Contains(state, "example/a\tuser\t") {
		t.Fatalf("active stamp must survive:\n%s", state)
	}
}

func TestPruneRepoRemovesRepoDirAndClaudeLink(t *testing.T) {
	f := newFixture(t)
	f.writeRepo(`profiles = ["base"]`)
	f.writeManifest(`
[[installs]]
source = "example/r"
agents = ["codex"]
skills = ["sr"]
profiles = ["base"]
`)
	repoStore := filepath.Join(f.dir, ".agents", "skills")
	claudeDir := filepath.Join(f.dir, ".claude", "skills")
	mkdirs(t, repoStore, "sr")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../.agents/skills/sr", filepath.Join(claudeDir, "sr")); err != nil {
		t.Fatal(err)
	}
	// The global store holds a same-named skill that must never be touched by a
	// repo-level prune.
	globalStore := filepath.Join(f.home, ".agents", "skills")
	mkdirs(t, globalStore, "sr")

	writeLockFile(t, filepath.Join(f.dir, "skills-lock.json"), map[string][]string{
		"example/r": {"sr"},
	})
	f.write(f.state, "example/r\tproject:"+f.dir+"\tSIGR\n")

	// Change the repo selection so the entry is no longer active.
	f.writeRepo(`profiles = ["other"]`)

	opts := f.opts
	opts.Prune = true
	out, _, err := f.runWith(opts, Filters{Command: CmdApply, NonInteractive: true})
	if err != nil {
		t.Fatalf("repo prune failed: %v", err)
	}
	if !strings.Contains(out, "pruned: sr (source example/r) -> "+filepath.Join(repoStore, "sr")) {
		t.Fatalf("missing prune line:\n%s", out)
	}
	mustNotExist(t, filepath.Join(repoStore, "sr"))
	mustNotExist(t, filepath.Join(claudeDir, "sr"))
	mustExist(t, filepath.Join(globalStore, "sr"))

	if state := readFileOrEmpty(t, f.state); strings.Contains(state, "example/r") {
		t.Fatalf("project stamp must be removed:\n%s", state)
	}
}

// TestPruneNeverTouchesManualInstalls locks in the safety invariant: a lock
// entry without a matching stamp is a manual `npx skills add` and must survive.
func TestPruneNeverTouchesManualInstalls(t *testing.T) {
	f := newFixture(t)
	storeDir := filepath.Join(f.home, ".agents", "skills")
	mkdirs(t, storeDir, "manual")
	writeLockFile(t, filepath.Join(f.home, ".agents", ".skill-lock.json"), map[string][]string{
		"example/manual": {"manual"},
	})
	f.write(f.state, "")
	f.writeManifest(oneGlobalA)

	opts := f.opts
	opts.Prune = true
	out, _, err := f.runWith(opts, Filters{Command: CmdApply, NonInteractive: true, NoRepo: true})
	if err != nil {
		t.Fatalf("prune apply failed: %v", err)
	}
	if strings.Contains(out, "manual") {
		t.Fatalf("manual source must not appear in prune output:\n%s", out)
	}
	mustExist(t, filepath.Join(storeDir, "manual"))
}

func TestPruneKeepsSkillSharedWithActiveSource(t *testing.T) {
	f := newFixture(t)
	storeDir := filepath.Join(f.home, ".agents", "skills")
	mkdirs(t, storeDir, "s")
	// The lock is keyed by skill name, so it records the stale source's copy.
	writeLockFile(t, filepath.Join(f.home, ".agents", ".skill-lock.json"), map[string][]string{
		"example/b": {"s"},
	})
	f.write(f.state, "example/b\tuser\tSIGB\n")
	// example/a is active and declares the same skill name.
	f.writeManifest(`
[[installs]]
source = "example/a"
agents = ["codex"]
skills = ["s"]
profiles = ["global"]
`)

	opts := f.opts
	opts.Prune = true
	out, _, err := f.runWith(opts, Filters{Command: CmdApply, NonInteractive: true, NoRepo: true})
	if err != nil {
		t.Fatalf("prune apply failed: %v", err)
	}
	if !strings.Contains(out, "keep (shared with active source): s") {
		t.Fatalf("expected same-name protection notice:\n%s", out)
	}
	mustExist(t, filepath.Join(storeDir, "s"))
	if state := readFileOrEmpty(t, f.state); strings.Contains(state, "example/b") {
		t.Fatalf("retired stamp must still be removed:\n%s", state)
	}
}

func TestPruneDryRunListsOnly(t *testing.T) {
	f := newFixture(t)
	storeDir := filepath.Join(f.home, ".agents", "skills")
	mkdirs(t, storeDir, "sb")
	writeLockFile(t, filepath.Join(f.home, ".agents", ".skill-lock.json"), map[string][]string{
		"example/b": {"sb"},
	})
	stateBefore := "example/a\tuser\tSIGA\nexample/b\tuser\tSIGB\n"
	f.write(f.state, stateBefore)
	f.writeManifest(oneGlobalA)

	opts := f.opts
	opts.Prune = true
	out, _, err := f.runWith(opts, Filters{Command: CmdDryRun, NonInteractive: true, NoRepo: true})
	if err != nil {
		t.Fatalf("dry-run prune failed: %v", err)
	}
	if !strings.Contains(out, "would prune: sb (source example/b) -> "+filepath.Join(storeDir, "sb")) {
		t.Fatalf("missing would-prune line:\n%s", out)
	}
	mustExist(t, filepath.Join(storeDir, "sb"))
	if got := readFileOrEmpty(t, f.state); got != stateBefore {
		t.Fatalf("dry-run must not touch stamps:\nbefore=%q\nafter =%q", stateBefore, got)
	}
}

func TestPruneNothingToPrune(t *testing.T) {
	f := newFixture(t)
	f.write(f.state, "example/a\tuser\tSIGA\n")
	f.writeManifest(oneGlobalA)

	opts := f.opts
	opts.Prune = true
	out, _, err := f.runWith(opts, Filters{Command: CmdApply, NonInteractive: true, NoRepo: true})
	if err != nil {
		t.Fatalf("prune apply failed: %v", err)
	}
	if !strings.Contains(out, "nothing to prune") {
		t.Fatalf("expected nothing-to-prune notice:\n%s", out)
	}
}

func TestPruneLockMissingOnlyClearsStamp(t *testing.T) {
	for _, tc := range []struct {
		name string
		lock bool
	}{
		{"no lock file", false},
		{"lock without record", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			storeDir := filepath.Join(f.home, ".agents", "skills")
			mkdirs(t, storeDir, "sb")
			if tc.lock {
				writeLockFile(t, filepath.Join(f.home, ".agents", ".skill-lock.json"), map[string][]string{
					"example/a": {"sa"},
				})
			}
			f.write(f.state, "example/b\tuser\tSIGB\n")
			f.writeManifest(oneGlobalA)

			opts := f.opts
			opts.Prune = true
			out, _, err := f.runWith(opts, Filters{Command: CmdApply, NonInteractive: true, NoRepo: true})
			if err != nil {
				t.Fatalf("prune apply failed: %v", err)
			}
			if !strings.Contains(out, "pruned: example/b (stamp only; no lock record)") {
				t.Fatalf("expected stamp-only notice:\n%s", out)
			}
			// Without a lock record we cannot know the skill name, so the
			// directory is left behind on purpose.
			mustExist(t, filepath.Join(storeDir, "sb"))
			if state := readFileOrEmpty(t, f.state); strings.Contains(state, "example/b") {
				t.Fatalf("stamp must be cleared:\n%s", state)
			}
		})
	}
}

func TestPruneWithForceCombination(t *testing.T) {
	f := newFixture(t)
	log := filepath.Join(f.dir, "npx.log")
	t.Setenv("NPX_LOG", log)
	storeDir := filepath.Join(f.home, ".agents", "skills")
	mkdirs(t, storeDir, "sb")
	writeLockFile(t, filepath.Join(f.home, ".agents", ".skill-lock.json"), map[string][]string{
		"example/a": {"sa"},
		"example/b": {"sb"},
	})
	f.write(f.state, "example/b\tuser\tSIGB\n")
	f.writeManifest(oneGlobalA)

	opts := f.opts
	opts.Prune = true
	opts.Force = true
	out, _, err := f.runWith(opts, Filters{Command: CmdApply, NonInteractive: true, NoRepo: true})
	if err != nil {
		t.Fatalf("force+prune failed: %v", err)
	}
	if strings.Contains(out, "skip (unchanged)") {
		t.Fatalf("force must reinstall the active entry:\n%s", out)
	}
	if got := lineCount(t, log); got != 1 {
		t.Fatalf("npx calls = %d, want 1 (forced active entry)", got)
	}
	mustNotExist(t, filepath.Join(storeDir, "sb"))
	if !strings.Contains(out, "pruned 1 path(s), 1 stamp(s)") {
		t.Fatalf("missing summary:\n%s", out)
	}
}

func TestPruneRejectedByReadOnlyCommands(t *testing.T) {
	f := newFixture(t)
	f.writeManifest(oneGlobalA)
	opts := f.opts
	opts.Prune = true
	for _, command := range []string{CmdList, CmdProfiles} {
		if err := func() error { _, _, e := f.runWith(opts, Filters{Command: command}); return e }(); err == nil {
			t.Fatalf("%s must reject --prune", command)
		}
	}
	for _, command := range []string{CmdResolve, CmdStatus} {
		f.writeRepo(`profiles = ["base"]`)
		if err := func() error { _, _, e := f.runWith(opts, Filters{Command: command}); return e }(); err == nil {
			t.Fatalf("%s must reject --prune", command)
		}
	}
}

// TestPruneMatchesURLSourceAgainstNormalizedLock guards the source matching:
// the manifest may declare a URL while the lock stores the normalized owner/repo.
func TestPruneMatchesURLSourceAgainstNormalizedLock(t *testing.T) {
	f := newFixture(t)
	storeDir := filepath.Join(f.home, ".agents", "skills")
	mkdirs(t, storeDir, "ctx")
	writeLockFile(t, filepath.Join(f.home, ".agents", ".skill-lock.json"), map[string][]string{
		"owner/repo": {"ctx"},
	})
	f.write(f.state, "https://github.com/owner/repo\tuser\tSIG\n")
	f.writeManifest(oneGlobalA)

	opts := f.opts
	opts.Prune = true
	out, _, err := f.runWith(opts, Filters{Command: CmdApply, NonInteractive: true, NoRepo: true})
	if err != nil {
		t.Fatalf("prune apply failed: %v", err)
	}
	if !strings.Contains(out, "pruned: ctx (source https://github.com/owner/repo) -> "+filepath.Join(storeDir, "ctx")) {
		t.Fatalf("URL source should match normalized lock source:\n%s", out)
	}
	mustNotExist(t, filepath.Join(storeDir, "ctx"))
}

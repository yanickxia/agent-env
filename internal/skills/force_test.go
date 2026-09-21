package skills

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestForceReinstallsAndRewritesStamp(t *testing.T) {
	f := newFixture(t)
	log := filepath.Join(f.dir, "npx.log")
	t.Setenv("NPX_LOG", log)
	f.writeRepo(`profiles = ["ark-mlops"]`)
	f.writeManifest(skipManifest)

	fl := Filters{Command: CmdApply, NonInteractive: true, SkipUnchanged: true}
	if _, _, err := f.run(fl); err != nil {
		t.Fatalf("first apply failed: %v", err)
	}
	if got := lineCount(t, log); got != 1 {
		t.Fatalf("npx calls after first apply = %d, want 1", got)
	}

	// Second run skips.
	if _, _, err := f.run(fl); err != nil {
		t.Fatalf("second apply failed: %v", err)
	}
	if got := lineCount(t, log); got != 1 {
		t.Fatalf("npx calls after skip = %d, want 1", got)
	}

	// --force reinstalls even though the stamp matches, and rewrites the stamp.
	forceOpts := f.opts
	forceOpts.Force = true
	out, _, err := f.runWith(forceOpts, fl)
	if err != nil {
		t.Fatalf("force apply failed: %v", err)
	}
	if strings.Contains(out, "skip (unchanged)") {
		t.Fatalf("force must not skip:\n%s", out)
	}
	if got := lineCount(t, log); got != 2 {
		t.Fatalf("npx calls after force = %d, want 2", got)
	}

	// A normal apply afterwards skips again (stamp was rewritten).
	if _, _, err := f.run(fl); err != nil {
		t.Fatalf("post-force apply failed: %v", err)
	}
	if got := lineCount(t, log); got != 2 {
		t.Fatalf("npx calls after post-force skip = %d, want 2", got)
	}
}

func TestForceOverridesSkipUnchangedFilterError(t *testing.T) {
	f := newFixture(t)
	f.writeManifest(skipManifest)
	f.writeRepo(`profiles = ["ark-mlops"]`)
	opts := f.opts
	opts.Force = true
	_, _, err := f.runWith(opts, Filters{
		Command:        CmdApply,
		Agents:         []string{"codex"},
		AgentSeen:      true,
		SkipUnchanged:  true,
		NonInteractive: true,
	})
	if err != nil {
		t.Fatalf("--force must override the --skip-unchanged filter restriction, got %v", err)
	}
}

func TestForceWithSkillFilterReinstallsOnlyThatEntry(t *testing.T) {
	f := newFixture(t)
	log := filepath.Join(f.dir, "npx.log")
	t.Setenv("NPX_LOG", log)
	f.writeManifest(`
[[installs]]
source = "example/a"
agents = ["codex"]
skills = ["s1"]
profiles = ["global"]

[[installs]]
source = "example/b"
agents = ["codex"]
skills = ["s2"]
profiles = ["global"]
`)
	opts := f.opts
	opts.Force = true
	if _, _, err := f.runWith(opts, Filters{
		Command:        CmdApply,
		Skills:         []string{"s1"},
		SkillSeen:      true,
		NonInteractive: true,
	}); err != nil {
		t.Fatalf("force+skill apply failed: %v", err)
	}
	logText := readFileOrEmpty(t, log)
	if !strings.Contains(logText, "example/a") {
		t.Fatalf("example/a must be installed:\n%s", logText)
	}
	if strings.Contains(logText, "example/b") {
		t.Fatalf("example/b must be filtered out:\n%s", logText)
	}
	if got := lineCount(t, log); got != 1 {
		t.Fatalf("npx calls = %d, want 1", got)
	}
}

func TestForceRejectedByReadOnlyCommands(t *testing.T) {
	f := newFixture(t)
	f.writeManifest(skipManifest)
	opts := f.opts
	opts.Force = true
	if err := func() error { _, _, e := f.runWith(opts, Filters{Command: CmdList}); return e }(); err == nil {
		t.Fatal("list must reject --force")
	}
	if err := func() error { _, _, e := f.runWith(opts, Filters{Command: CmdStatus}); return e }(); err == nil {
		t.Fatal("status must reject --force")
	}
}

func TestForceInRepoKeepsGlobalStamps(t *testing.T) {
	f := newFixture(t)
	log := filepath.Join(f.dir, "npx.log")
	t.Setenv("NPX_LOG", log)
	f.writeRepo(`profiles = ["ark-mlops"]`)
	f.writeManifest(`
[[installs]]
source = "example/global"
agents = ["codex"]
skills = ["g1"]
profiles = ["global"]

[[installs]]
source = "example/repo"
agents = ["codex"]
skills = ["r1"]
profiles = ["ark-mlops"]
`)

	// First apply installs both and writes stamps.
	fl := Filters{Command: CmdApply, NonInteractive: true, SkipUnchanged: true}
	if _, _, err := f.run(fl); err != nil {
		t.Fatalf("first apply failed: %v", err)
	}
	if got := lineCount(t, log); got != 2 {
		t.Fatalf("npx calls after first apply = %d, want 2", got)
	}

	// --force in a repo run reinstalls only repo-level entries; the global
	// entry keeps its stamp and skips.
	forceOpts := f.opts
	forceOpts.Force = true
	out, _, err := f.runWith(forceOpts, fl)
	if err != nil {
		t.Fatalf("force apply failed: %v", err)
	}
	if !strings.Contains(out, "skip (unchanged): example/global [user]") {
		t.Fatalf("global entry must keep its stamp in a repo force run:\n%s", out)
	}
	if strings.Contains(out, "skip (unchanged): example/repo") {
		t.Fatalf("repo-level entry must be forced:\n%s", out)
	}
	if got := lineCount(t, log); got != 3 {
		t.Fatalf("npx calls after force = %d, want 3 (repo-level only)", got)
	}
}

func TestForceNoRepoReinstallsGlobals(t *testing.T) {
	f := newFixture(t)
	log := filepath.Join(f.dir, "npx.log")
	t.Setenv("NPX_LOG", log)
	f.writeManifest(`
[[installs]]
source = "example/global"
agents = ["codex"]
skills = ["g1"]
profiles = ["global"]
`)

	// First --no-repo apply installs the global entry and writes its stamp.
	fl := Filters{Command: CmdApply, NonInteractive: true, NoRepo: true, SkipUnchanged: true}
	if _, _, err := f.run(fl); err != nil {
		t.Fatalf("first apply failed: %v", err)
	}
	if got := lineCount(t, log); got != 1 {
		t.Fatalf("npx calls after first apply = %d, want 1", got)
	}

	// --no-repo --force still reinstalls globals (interrupted-install repair).
	forceOpts := f.opts
	forceOpts.Force = true
	out, _, err := f.runWith(forceOpts, fl)
	if err != nil {
		t.Fatalf("force apply failed: %v", err)
	}
	if strings.Contains(out, "skip (unchanged)") {
		t.Fatalf("no-repo force must reinstall globals:\n%s", out)
	}
	if got := lineCount(t, log); got != 2 {
		t.Fatalf("npx calls after force = %d, want 2", got)
	}
}

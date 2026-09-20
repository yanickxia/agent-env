package skills

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yanickxia/agent-env/internal/config"
	"github.com/yanickxia/agent-env/internal/stamp"
)

// globalUserSig recomputes the [user] signature processManifest will look up,
// using the parsed manifest entry so the test tracks any default changes.
func globalUserSig(t *testing.T, manifestPath, source string) string {
	t.Helper()
	entries, err := config.ParseManifest(manifestPath)
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	for _, e := range entries {
		if e.Source != source {
			continue
		}
		if !e.Global {
			t.Fatalf("%s is not a global entry", source)
		}
		return stamp.Signature(
			strings.TrimSpace(e.Source),
			strings.TrimSpace(e.AgentsRaw),
			strings.TrimSpace(e.SkillsRaw),
			e.Mode, "user", e.Installer, e.EnvRaw, "", "")
	}
	t.Fatalf("source %s not found in manifest", source)
	return ""
}

// A matching [user] stamp must skip the global entry on a plain apply in a
// repo context, while repo-level entries still install normally.
func TestGlobalStampSkipsWithoutSkipUnchanged(t *testing.T) {
	f := newFixture(t)
	log := filepath.Join(f.dir, "npx.log")
	t.Setenv("NPX_LOG", log)

	f.writeRepo(`profiles = ["base"]`)
	f.writeManifest(globalEntry + repoEntry)
	f.write(f.state, "example/global\tuser\t"+globalUserSig(t, f.manifest, "example/global")+"\n")

	out, _, err := f.run(Filters{Command: CmdApply, NonInteractive: true})
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if !strings.Contains(out, "skip (unchanged): example/global [user]") {
		t.Fatalf("global entry must be skipped by its user stamp:\n%s", out)
	}
	// Only the repo-level entry should have hit npx.
	logText := readFileOrEmpty(t, log)
	if !strings.Contains(logText, "example/ark") || strings.Contains(logText, "example/global") {
		t.Fatalf("only the repo-level entry must install:\n%s", logText)
	}
	if got := lineCount(t, log); got != 1 {
		t.Fatalf("npx calls = %d, want 1 (repo-level only)", got)
	}
}

// A plain apply with no stamp installs the global entry and writes its [user]
// stamp, so the next run skips it. Repo-level project stamps stay opt-in.
func TestGlobalStampWrittenAfterPlainApply(t *testing.T) {
	f := newFixture(t)
	log := filepath.Join(f.dir, "npx.log")
	t.Setenv("NPX_LOG", log)

	f.writeRepo(`profiles = ["base"]`)
	f.writeManifest(globalEntry + repoEntry)

	if _, _, err := f.run(Filters{Command: CmdApply, NonInteractive: true}); err != nil {
		t.Fatalf("first apply failed: %v", err)
	}
	if got := lineCount(t, log); got != 2 {
		t.Fatalf("first apply npx calls = %d, want 2", got)
	}
	state := readFileOrEmpty(t, f.state)
	if !strings.Contains(state, "example/global\tuser\t") {
		t.Fatalf("plain apply must write the global user stamp:\n%s", state)
	}
	if strings.Contains(state, "project:") {
		t.Fatalf("repo-level project stamp must stay opt-in:\n%s", state)
	}

	out, _, err := f.run(Filters{Command: CmdApply, NonInteractive: true})
	if err != nil {
		t.Fatalf("second apply failed: %v", err)
	}
	if !strings.Contains(out, "skip (unchanged): example/global [user]") {
		t.Fatalf("second plain apply must skip the global entry:\n%s", out)
	}
	if got := lineCount(t, log); got != 3 {
		t.Fatalf("npx calls = %d, want 3 (repo-level re-installed)", got)
	}
}

// A stale/mismatched user stamp must not skip: the entry reinstalls and the
// stamp is rewritten with the current signature.
func TestGlobalStampMismatchReinstallsAndRewrites(t *testing.T) {
	f := newFixture(t)
	log := filepath.Join(f.dir, "npx.log")
	t.Setenv("NPX_LOG", log)

	f.writeRepo(`profiles = ["base"]`)
	f.writeManifest(globalEntry)
	f.write(f.state, "example/global\tuser\tdeadbeef\n")

	out, _, err := f.run(Filters{Command: CmdApply, NonInteractive: true})
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if strings.Contains(out, "skip (unchanged)") {
		t.Fatalf("mismatched stamp must not skip:\n%s", out)
	}
	if got := lineCount(t, log); got != 1 {
		t.Fatalf("npx calls = %d, want 1", got)
	}
	state := readFileOrEmpty(t, f.state)
	if !strings.Contains(state, globalUserSig(t, f.manifest, "example/global")) {
		t.Fatalf("stamp must be rewritten with the current signature:\n%s", state)
	}
}

// A matching [user] stamp is honored by dry-run too: it prints the skip and
// never writes the state file.
func TestGlobalStampDryRunSkipsWithoutWriting(t *testing.T) {
	f := newFixture(t)
	log := filepath.Join(f.dir, "npx.log")
	t.Setenv("NPX_LOG", log)

	f.writeRepo(`profiles = ["base"]`)
	f.writeManifest(globalEntry)
	sig := globalUserSig(t, f.manifest, "example/global")
	f.write(f.state, "example/global\tuser\t"+sig+"\n")

	out, _, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out, "skip (unchanged): example/global [user]") {
		t.Fatalf("dry-run must honor the user stamp:\n%s", out)
	}
	if got := lineCount(t, log); got != 0 {
		t.Fatalf("dry-run must not invoke npx, got %d calls", got)
	}
	if state := readFileOrEmpty(t, f.state); state != "example/global\tuser\t"+sig+"\n" {
		t.Fatalf("dry-run must not rewrite the state file:\n%q", state)
	}
}

// A narrowed run (--agent/--skill) still consults the user stamp, but must not
// write it when installing: the signature covers the declared shape, not the
// narrowed install set.
func TestGlobalStampNarrowedRunDoesNotWrite(t *testing.T) {
	t.Run("matching stamp skips", func(t *testing.T) {
		f := newFixture(t)
		log := filepath.Join(f.dir, "npx.log")
		t.Setenv("NPX_LOG", log)

		f.writeRepo(`profiles = ["base"]`)
		f.writeManifest(globalEntry)
		f.write(f.state, "example/global\tuser\t"+globalUserSig(t, f.manifest, "example/global")+"\n")

		out, _, err := f.run(Filters{
			Command:        CmdApply,
			NonInteractive: true,
			Agents:         []string{"codex"},
			AgentSeen:      true,
		})
		if err != nil {
			t.Fatalf("apply failed: %v", err)
		}
		if !strings.Contains(out, "skip (unchanged): example/global [user]") {
			t.Fatalf("matching stamp must skip even when narrowed:\n%s", out)
		}
		if got := lineCount(t, log); got != 0 {
			t.Fatalf("npx calls = %d, want 0", got)
		}
	})

	t.Run("mismatch installs without writing", func(t *testing.T) {
		f := newFixture(t)
		log := filepath.Join(f.dir, "npx.log")
		t.Setenv("NPX_LOG", log)

		f.writeRepo(`profiles = ["base"]`)
		f.writeManifest(globalEntry)
		f.write(f.state, "example/global\tuser\tdeadbeef\n")

		out, _, err := f.run(Filters{
			Command:        CmdApply,
			NonInteractive: true,
			Agents:         []string{"codex"},
			AgentSeen:      true,
		})
		if err != nil {
			t.Fatalf("apply failed: %v", err)
		}
		if strings.Contains(out, "skip (unchanged)") {
			t.Fatalf("mismatched stamp must install:\n%s", out)
		}
		if got := lineCount(t, log); got != 1 {
			t.Fatalf("npx calls = %d, want 1", got)
		}
		if state := readFileOrEmpty(t, f.state); state != "example/global\tuser\tdeadbeef\n" {
			t.Fatalf("narrowed run must not rewrite the user stamp:\n%q", state)
		}
	})
}

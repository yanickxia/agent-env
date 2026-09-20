package skills

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNoRepoInstallsGlobalOnlyWithoutWarning(t *testing.T) {
	for _, command := range []string{CmdApply, CmdDryRun} {
		t.Run(command, func(t *testing.T) {
			f := newFixture(t)
			log := filepath.Join(f.dir, "npx.log")
			t.Setenv("NPX_LOG", log)
			f.writeManifest(globalEntry + repoEntry)

			out, errb, err := f.run(Filters{Command: command, NonInteractive: true, NoRepo: true})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(out, "example/global") {
				t.Fatalf("global entry must install with --no-repo:\n%s", out)
			}
			if strings.Contains(out, "example/ark") {
				t.Fatalf("repo-level entry must be excluded with --no-repo:\n%s", out)
			}
			if strings.Contains(errb, "skipped") || strings.Contains(errb, "no .agent-env.toml") {
				t.Fatalf("--no-repo must not warn about repo-level skips: %q", errb)
			}
			if command == CmdApply {
				if got := lineCount(t, log); got != 1 {
					t.Fatalf("npx calls with --no-repo = %d, want 1 (global only)", got)
				}
			}
		})
	}
}

// Even when an ancestor directory carries .agent-env.toml, --no-repo must not
// read it: repo-level entries stay excluded and no skip warning is printed.
func TestNoRepoIgnoresAncestorRepoConfig(t *testing.T) {
	f := newFixture(t)
	tr := newLayerTree(t)
	f.writeManifest(layerManifest)
	f.write(filepath.Join(tr.parent, ".agent-env.toml"), `profiles = ["base"]`)

	opts := layeredOpts(f, tr)
	out, errb, err := f.runWith(opts, Filters{Command: CmdDryRun, NonInteractive: true, NoRepo: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out, "example/global") {
		t.Fatalf("global entry must install:\n%s", out)
	}
	if strings.Contains(out, "example/base") || strings.Contains(out, "example/ark") {
		t.Fatalf("ancestor repo config must be ignored under --no-repo:\n%s", out)
	}
	if strings.Contains(errb, "skipped") || strings.Contains(errb, "no .agent-env.toml") {
		t.Fatalf("--no-repo must suppress the repo-level skip warning: %q", errb)
	}

	// Sanity: without --no-repo the ancestor config selects the base entry.
	sel, _, err := f.runWith(opts, Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("sanity dry-run failed: %v", err)
	}
	if !strings.Contains(sel, "example/base") {
		t.Fatalf("ancestor config should select example/base without --no-repo:\n%s", sel)
	}
}

func TestNoRepoRejectsProfileCombination(t *testing.T) {
	f := newFixture(t)
	f.writeManifest(repoEntry)

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
	f.writeManifest(repoEntry)

	if _, _, err := f.run(Filters{Command: CmdList, NoRepo: true}); err == nil || !strings.Contains(err.Error(), "not list") {
		t.Fatalf("list must reject --no-repo, got %v", err)
	}
	if _, _, err := f.run(Filters{Command: CmdProfiles, NoRepo: true}); err == nil || !strings.Contains(err.Error(), "not profiles") {
		t.Fatalf("profiles must reject --no-repo, got %v", err)
	}
	for _, command := range []string{CmdResolve, CmdStatus} {
		if _, _, err := f.run(Filters{Command: command, NoRepo: true}); err == nil || !strings.Contains(err.Error(), "not resolve/status") {
			t.Fatalf("%s must reject --no-repo, got %v", command, err)
		}
	}
}

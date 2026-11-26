package skills

import (
	"path/filepath"
	"strings"
	"testing"
)

const hookManifest = `
[[installs]]
source = "example/global"
agents = ["codex"]
skills = ["s"]
profiles = ["global"]
post_install = ["printf 'hooked\\n' >> \"$NPX_LOG\""]
`

func TestPostInstallShellSelection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		shells []string
		want   string
	}{
		{"zsh preferred", []string{"sh", "bash", "zsh"}, "zsh"},
		{"bash without zsh", []string{"sh", "bash"}, "bash"},
		{"sh only", []string{"sh"}, "sh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			for _, s := range tc.shells {
				installStub(t, f.stub, s)
			}
			// Only the stub directory is on PATH, so the real shells of the
			// test machine cannot leak into the lookup.
			t.Setenv("PATH", f.stub)
			log := filepath.Join(f.dir, "hooks.log")
			t.Setenv("NPX_LOG", log)
			f.writeManifest(hookManifest)

			out, _, err := f.run(Filters{Command: CmdDryRun})
			if err != nil || !strings.Contains(out, tc.want+" -c printf\\ \\'hooked") {
				t.Fatalf("dry-run must print the resolved shell: err=%v out=%s", err, out)
			}
			if strings.Contains(readFileOrEmpty(t, log), "hooked") {
				t.Fatal("dry-run executed a hook")
			}
			if _, _, err := f.run(Filters{Command: CmdApply, NonInteractive: true}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(readFileOrEmpty(t, log), "-c printf 'hooked") {
				t.Fatalf("hook was not executed via %s: %s", tc.want, readFileOrEmpty(t, log))
			}
		})
	}
}

func TestPostInstallWithoutAnyShell(t *testing.T) {
	f := newFixture(t)
	t.Setenv("PATH", f.stub) // npx/node stubs only: no zsh, bash or sh
	log := filepath.Join(f.dir, "hooks.log")
	t.Setenv("NPX_LOG", log)
	f.writeManifest(hookManifest)

	out, _, err := f.run(Filters{Command: CmdDryRun})
	if err != nil || !strings.Contains(out, "cannot run without a shell: printf 'hooked") {
		t.Fatalf("dry-run must report the missing shell without failing: err=%v out=%s", err, out)
	}
	_, errb, err := f.run(Filters{Command: CmdApply, NonInteractive: true})
	if err == nil || !strings.Contains(err.Error(), "1 post_install hook(s) failed") {
		t.Fatalf("apply must fail the hook: err=%v", err)
	}
	if !strings.Contains(errb, "none of zsh, bash, sh found on PATH") {
		t.Fatalf("stderr must name the shells tried: %s", errb)
	}
	if strings.Contains(readFileOrEmpty(t, log), "hooked") {
		t.Fatal("nothing may run without a shell")
	}
}

func TestPostInstallIfMissingGuard(t *testing.T) {
	for _, tc := range []struct {
		name   string
		guard  string
		wantGo bool
	}{
		{"present", "node", false},
		{"absent", "agent-env-missing-binary", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			log := filepath.Join(f.dir, "hooks.log")
			t.Setenv("NPX_LOG", log)
			f.writeManifest(`
[[installs]]
source = "example/global"
agents = ["codex"]
skills = ["s"]
profiles = ["global"]
post_install = [{ run = "printf 'hooked\\n' >> \"$NPX_LOG\"", if_missing = "` + tc.guard + `" }]
`)
			out, _, err := f.run(Filters{Command: CmdApply, NonInteractive: true})
			if err != nil {
				t.Fatal(err)
			}
			ran := strings.Contains(readFileOrEmpty(t, log), "hooked")
			if ran != tc.wantGo || strings.Contains(out, "already present, skipping") == tc.wantGo {
				t.Fatalf("guard %q: ran=%v out=%s", tc.guard, ran, out)
			}
		})
	}
}

func TestInlinePostInstallGatedByAllowHooks(t *testing.T) {
	f := newFixture(t)
	f.writeRepo(`
agents = ["codex"]
[[installs]]
source = "./local-skills"
skills = ["local"]
post_install = ["printf 'inline-hook\\n' >> \"$NPX_LOG\""]
`)
	f.writeManifest(`
[[installs]]
source = "example/global"
agents = ["codex"]
skills = ["s"]
profiles = ["global"]
post_install = ["printf 'global-hook\\n' >> \"$NPX_LOG\""]
`)
	log := filepath.Join(f.dir, "hooks.log")
	t.Setenv("NPX_LOG", log)

	out, errb, err := f.run(Filters{Command: CmdDryRun})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "# post_install: skipped (declared in "+f.repo+"; pass --allow-hooks to run): printf 'inline-hook") || strings.Contains(out, "-c printf\\ \\'inline-hook") {
		t.Fatalf("dry-run must list the inline hook as skipped and not as a command: %s", out)
	}
	if !strings.Contains(out, "-c printf\\ \\'global-hook") {
		t.Fatalf("hooks from the user's global manifest are trusted and must print as commands: %s", out)
	}
	if !strings.Contains(errb, "1 post_install hook(s) from .agent-env.toml skipped; re-run with --allow-hooks") {
		t.Fatalf("stderr must summarize skipped hooks: %s", errb)
	}
	if strings.Contains(readFileOrEmpty(t, log), "hook") {
		t.Fatal("dry-run executed a hook")
	}

	apply := Filters{Command: CmdApply, NonInteractive: true}
	if _, errb, err := f.run(apply); err != nil {
		t.Fatalf("skipped inline hooks must not fail apply: %v (stderr: %s)", err, errb)
	}
	calls := readFileOrEmpty(t, log)
	if !strings.Contains(calls, "global-hook") || strings.Contains(calls, "inline-hook") {
		t.Fatalf("without --allow-hooks only global hooks run: %s", calls)
	}

	apply.AllowHooks = true
	if out, _, err := f.run(apply); err != nil || !strings.Contains(out, "-c printf\\ \\'inline-hook") {
		t.Fatalf("--allow-hooks must print and run the inline hook: err=%v out=%s", err, out)
	}
	if calls = readFileOrEmpty(t, log); !strings.Contains(calls, "inline-hook") {
		t.Fatalf("--allow-hooks must execute the inline hook: %s", calls)
	}

	for _, cmd := range []string{CmdResolve, CmdStatus, CmdList, CmdProfiles} {
		if _, _, err := f.run(Filters{Command: cmd, AllowHooks: true}); err == nil || !(strings.Contains(err.Error(), "--allow-hooks") || strings.Contains(err.Error(), "filters are supported")) {
			t.Fatalf("%s must reject --allow-hooks, got %v", cmd, err)
		}
	}
}

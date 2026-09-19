package skills

import (
	"strings"
	"testing"
)

const projectEntry = `
[[installs]]
source = "example/ark"
agents = ["codex"]
skills = ["mlops-lane"]
scope = "project"
profiles = ["base"]
`

// --- gating ------------------------------------------------------------------

func TestGatingProjectApplyNeedsSelection(t *testing.T) {
	for _, command := range []string{CmdApply, CmdDryRun} {
		t.Run(command, func(t *testing.T) {
			f := newFixture(t)
			f.writeManifest(projectEntry)

			_, _, err := f.run(Filters{
				Command:        command,
				Scopes:         []string{"project"},
				ScopeSeen:      true,
				NonInteractive: true,
			})
			if err == nil || !strings.Contains(err.Error(), "no .agent-env.toml") {
				t.Fatalf("want project gating error, got %v", err)
			}
			if !strings.Contains(err.Error(), "agent-env init <profile>") {
				t.Fatalf("gating hint must point at agent-env init, got %v", err)
			}
		})
	}
}

func TestGatingUnfilteredRunSkipsUnprofiledProjectEntries(t *testing.T) {
	f := newFixture(t)
	f.writeManifest(`
[[installs]]
source = "example/user"
agents = ["codex"]
skills = ["lane"]
scope = "user"

[[installs]]
source = "example/noprofile"
agents = ["codex"]
skills = ["lane"]
scope = "project"
`)
	out, errb, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "example/user") {
		t.Fatalf("user entry missing from output: %s", out)
	}
	if strings.Contains(out, "example/noprofile") {
		t.Fatalf("unprofiled project entry must be skipped: %s", out)
	}
	if !strings.Contains(errb, "skipped 1 project entries") {
		t.Fatalf("want skip notice, got %q", errb)
	}
	if !strings.Contains(errb, "agent-env init or --profile") {
		t.Fatalf("skip notice must point at agent-env init, got %q", errb)
	}
}

func TestGatingResolveStatusConstraints(t *testing.T) {
	t.Run("no repo config or profile", func(t *testing.T) {
		f := newFixture(t)
		f.writeManifest(projectEntry)
		for _, command := range []string{CmdResolve, CmdStatus} {
			_, _, err := f.run(Filters{Command: command})
			if err == nil || !strings.Contains(err.Error(), "need a repo config or --profile") {
				t.Fatalf("%s: want selection error, got %v", command, err)
			}
		}
	})

	t.Run("non project scope rejected", func(t *testing.T) {
		f := newFixture(t)
		f.writeManifest(projectEntry)
		f.writeRepo(`profiles = ["base"]`)
		_, _, err := f.run(Filters{Command: CmdResolve, Scopes: []string{"user"}, ScopeSeen: true})
		if err == nil || !strings.Contains(err.Error(), "not supported here") {
			t.Fatalf("want scope rejection, got %v", err)
		}
	})

	t.Run("skill filter rejected", func(t *testing.T) {
		f := newFixture(t)
		f.writeManifest(projectEntry)
		f.writeRepo(`profiles = ["base"]`)
		_, _, err := f.run(Filters{Command: CmdStatus, Skills: []string{"lane"}, SkillSeen: true})
		if err == nil || !strings.Contains(err.Error(), "--skill/--skills filters") {
			t.Fatalf("want skill rejection, got %v", err)
		}
	})

	t.Run("skip-unchanged rejected", func(t *testing.T) {
		f := newFixture(t)
		f.writeManifest(projectEntry)
		f.writeRepo(`profiles = ["base"]`)
		_, _, err := f.run(Filters{Command: CmdStatus, SkipUnchanged: true})
		if err == nil || !strings.Contains(err.Error(), "--skip-unchanged is an apply flag") {
			t.Fatalf("want skip-unchanged rejection, got %v", err)
		}
	})
}

func TestGatingProfilesRejectsFilters(t *testing.T) {
	f := newFixture(t)
	f.writeManifest(projectEntry)
	_, _, err := f.run(Filters{Command: CmdProfiles, Scopes: []string{"user"}, ScopeSeen: true})
	if err == nil || !strings.Contains(err.Error(), "not profiles") {
		t.Fatalf("want profiles filter rejection, got %v", err)
	}
}

func TestGatingListRejectsFiltersAndPrintsVerbatim(t *testing.T) {
	f := newFixture(t)
	body := "# a comment\n[[installs]]\nsource = \"example/a\"\nagents = [\"codex\"]\nscope = \"user\"\n"
	f.writeManifest(body)

	out, _, err := f.run(Filters{Command: CmdList})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if out != body {
		t.Fatalf("list must print verbatim:\nwant %q\ngot  %q", body, out)
	}

	_, _, err = f.run(Filters{Command: CmdList, AgentSeen: true, Agents: []string{"codex"}})
	if err == nil || !strings.Contains(err.Error(), "not list") {
		t.Fatalf("want list filter rejection, got %v", err)
	}
}

func TestGatingSkipUnchangedCombination(t *testing.T) {
	f := newFixture(t)
	f.writeManifest(projectEntry)
	_, _, err := f.run(Filters{
		Command:       CmdApply,
		Agents:        []string{"codex"},
		AgentSeen:     true,
		SkipUnchanged: true,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined with --agent") {
		t.Fatalf("want combination error, got %v", err)
	}
}

func TestGatingApplyWithoutNonInteractive(t *testing.T) {
	f := newFixture(t)
	f.writeManifest(projectEntry)
	_, _, err := f.run(Filters{Command: CmdApply})
	if err == nil || !strings.Contains(err.Error(), "interactive selection is not supported") {
		t.Fatalf("want interactive-not-supported error, got %v", err)
	}
}

func TestScopeValidationError(t *testing.T) {
	f := newFixture(t)
	f.writeManifest(projectEntry)
	_, _, err := f.run(Filters{Command: CmdDryRun, Scopes: []string{"bogus"}, ScopeSeen: true})
	if err == nil || !strings.Contains(err.Error(), "unsupported scope 'bogus'") {
		t.Fatalf("want scope validation error, got %v", err)
	}
}

// --- profiles selection ------------------------------------------------------

func TestProfilesCommandSortedDeduped(t *testing.T) {
	f := newFixture(t)
	f.writeManifest(`
[[installs]]
source = "example/a"
scope = "user"
profiles = ["base", "ark-mlops"]

[[installs]]
source = "example/b"
scope = "user"
profiles = ["maas", "base"]
`)
	out, _, err := f.run(Filters{Command: CmdProfiles})
	if err != nil {
		t.Fatalf("profiles failed: %v", err)
	}
	if out != "ark-mlops\nbase\nmaas\n" {
		t.Fatalf("unexpected profiles output: %q", out)
	}
}

func TestProfilesCommandEmpty(t *testing.T) {
	f := newFixture(t)
	f.writeManifest("[[installs]]\nsource = \"example/a\"\nscope = \"user\"\n")
	_, _, err := f.run(Filters{Command: CmdProfiles})
	if err == nil || !strings.Contains(err.Error(), "no profiles declared") {
		t.Fatalf("want no-profiles error, got %v", err)
	}
}

func TestProfileIntersectionSelectsEntries(t *testing.T) {
	f := newFixture(t)
	f.writeRepo(`profiles = ["ark-mlops"]`)
	f.writeManifest(`
[[installs]]
source = "example/keep"
agents = ["codex"]
skills = ["lane"]
scope = "project"
profiles = ["ark-mlops", "maas"]

[[installs]]
source = "example/skip"
agents = ["codex"]
skills = ["web"]
scope = "project"
profiles = ["frontend"]
`)
	out, _, err := f.run(Filters{Command: CmdDryRun, Scopes: []string{"project"}, ScopeSeen: true, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out, "example/keep") || strings.Contains(out, "example/skip") {
		t.Fatalf("profile intersection wrong:\n%s", out)
	}
}

func TestCLIProfileOverridesRepoConfigWithoutWriting(t *testing.T) {
	f := newFixture(t)
	f.writeRepo(`profiles = ["ark-mlops"]`)
	before := readFileOrEmpty(t, f.repo)
	f.writeManifest(`
[[installs]]
source = "example/ark"
agents = ["codex"]
skills = ["lane"]
scope = "project"
profiles = ["ark-mlops"]

[[installs]]
source = "example/web"
agents = ["codex"]
skills = ["browser"]
scope = "project"
profiles = ["frontend"]
`)
	out, _, err := f.run(Filters{
		Command: CmdDryRun, Scopes: []string{"project"}, ScopeSeen: true,
		Profiles: []string{"frontend"}, ProfileSeen: true, NonInteractive: true,
	})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out, "example/web") || strings.Contains(out, "example/ark") {
		t.Fatalf("--profile override wrong:\n%s", out)
	}
	if after := readFileOrEmpty(t, f.repo); after != before {
		t.Fatalf("--profile must not rewrite the repo config:\nbefore %q\nafter  %q", before, after)
	}
}

func TestAgentNarrowing(t *testing.T) {
	f := newFixture(t)
	f.writeManifest(`
[[installs]]
source = "example/keep"
agents = ["codex", "opencode"]
skills = ["lane"]
scope = "project"
profiles = ["ark-mlops"]

[[installs]]
source = "example/skip"
agents = ["trae"]
skills = ["web"]
scope = "project"
profiles = ["ark-mlops"]
`)

	t.Run("repo agents narrow entries", func(t *testing.T) {
		f.writeRepo("profiles = [\"ark-mlops\"]\nagents = [\"codex\"]\n")
		out, _, err := f.run(Filters{Command: CmdDryRun, Scopes: []string{"project"}, ScopeSeen: true, NonInteractive: true})
		if err != nil {
			t.Fatalf("dry-run failed: %v", err)
		}
		if !strings.Contains(out, "example/keep") || strings.Contains(out, "example/skip") {
			t.Fatalf("narrowing wrong:\n%s", out)
		}
		if !strings.Contains(out, "-a codex") || strings.Contains(out, "-a opencode") {
			t.Fatalf("agents not narrowed:\n%s", out)
		}
	})

	t.Run("absent repo agents use entry agents", func(t *testing.T) {
		f.writeRepo(`profiles = ["ark-mlops"]`)
		out, _, err := f.run(Filters{Command: CmdDryRun, Scopes: []string{"project"}, ScopeSeen: true, NonInteractive: true})
		if err != nil {
			t.Fatalf("dry-run failed: %v", err)
		}
		if !strings.Contains(out, "-a codex -a opencode") || !strings.Contains(out, "-a trae") {
			t.Fatalf("entry agents not used:\n%s", out)
		}
	})
}

func TestSkillFilterExcludesWildcardAutoMatch(t *testing.T) {
	f := newFixture(t)
	f.writeManifest(`
[[installs]]
source = "example/wild"
agents = ["codex"]
skills = ["*"]
scope = "user"

[[installs]]
source = "example/named"
agents = ["codex"]
skills = ["lane"]
scope = "user"
`)
	// No --skill: wildcard passes through as a literal --skill \*.
	out, _, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out, "--skill \\*") {
		t.Fatalf("wildcard must be passed to the installer:\n%s", out)
	}
	if !strings.Contains(out, "example/wild") {
		t.Fatalf("wildcard entry missing:\n%s", out)
	}

	// With --skill lane, the wildcard entry must NOT auto-match.
	out, _, err = f.run(Filters{Command: CmdDryRun, Skills: []string{"lane"}, SkillSeen: true, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if strings.Contains(out, "example/wild") {
		t.Fatalf("wildcard must not auto-match --skill:\n%s", out)
	}
	if !strings.Contains(out, "example/named") {
		t.Fatalf("named skill entry missing:\n%s", out)
	}
}

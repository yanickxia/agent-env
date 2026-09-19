package skills

import (
	"strings"
	"testing"
)

// repoEntry is a repo-level entry (no global keyword): gated by the selection.
const repoEntry = `
[[installs]]
source = "example/ark"
agents = ["codex"]
skills = ["mlops-lane"]
profiles = ["base"]
`

// globalEntry installs unconditionally.
const globalEntry = `
[[installs]]
source = "example/global"
agents = ["codex"]
skills = ["lane"]
profiles = ["global"]
`

// --- gating ------------------------------------------------------------------

func TestNoSelectionInstallsGlobalAndSkipsRepoLevel(t *testing.T) {
	for _, command := range []string{CmdApply, CmdDryRun} {
		t.Run(command, func(t *testing.T) {
			f := newFixture(t)
			f.writeManifest(globalEntry + repoEntry)

			out, errb, err := f.run(Filters{Command: command, NonInteractive: true})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(out, "example/global") {
				t.Fatalf("global entry must install without selection:\n%s", out)
			}
			if strings.Contains(out, "example/ark") {
				t.Fatalf("repo-level entry must be skipped without selection:\n%s", out)
			}
			if !strings.Contains(errb, "skipped 1 repo-level entries") {
				t.Fatalf("want skip notice, got %q", errb)
			}
			if !strings.Contains(errb, "agent-env init or --profile") {
				t.Fatalf("skip notice must point at agent-env init, got %q", errb)
			}
		})
	}
}

func TestMixedGlobalAndTagIsGlobal(t *testing.T) {
	f := newFixture(t)
	f.writeManifest(`
[[installs]]
source = "example/mixed"
agents = ["codex"]
skills = ["lane"]
profiles = ["global", "base"]
`)
	// No repo config: still installs, and the "base" tag does not gate it.
	out, errb, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "example/mixed") {
		t.Fatalf("mixed entry must be global:\n%s", out)
	}
	if strings.Contains(errb, "skipped") {
		t.Fatalf("mixed entry must not be skipped: %q", errb)
	}
	// And it still installs when a selection excludes "base".
	f2 := newFixture(t)
	f2.writeRepo(`profiles = ["frontend"]`)
	f2.writeManifest(`
[[installs]]
source = "example/mixed"
agents = ["codex"]
skills = ["lane"]
profiles = ["global", "base"]
`)
	out, _, err = f2.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "example/mixed") {
		t.Fatalf("global entry must ignore non-matching tags:\n%s", out)
	}
}

func TestGatingResolveStatusConstraints(t *testing.T) {
	t.Run("no repo config or profile", func(t *testing.T) {
		f := newFixture(t)
		f.writeManifest(repoEntry)
		for _, command := range []string{CmdResolve, CmdStatus} {
			_, _, err := f.run(Filters{Command: command})
			if err == nil || !strings.Contains(err.Error(), "need a repo config or --profile") {
				t.Fatalf("%s: want selection error, got %v", command, err)
			}
		}
	})

	t.Run("skill filter rejected", func(t *testing.T) {
		f := newFixture(t)
		f.writeManifest(repoEntry)
		f.writeRepo(`profiles = ["base"]`)
		_, _, err := f.run(Filters{Command: CmdStatus, Skills: []string{"lane"}, SkillSeen: true})
		if err == nil || !strings.Contains(err.Error(), "--skill/--skills filters") {
			t.Fatalf("want skill rejection, got %v", err)
		}
	})

	t.Run("skip-unchanged rejected", func(t *testing.T) {
		f := newFixture(t)
		f.writeManifest(repoEntry)
		f.writeRepo(`profiles = ["base"]`)
		_, _, err := f.run(Filters{Command: CmdStatus, SkipUnchanged: true})
		if err == nil || !strings.Contains(err.Error(), "--skip-unchanged is an apply flag") {
			t.Fatalf("want skip-unchanged rejection, got %v", err)
		}
	})
}

func TestGatingProfilesRejectsFilters(t *testing.T) {
	f := newFixture(t)
	f.writeManifest(`[[installs]]
source = "example/a"
profiles = ["base"]
`)
	_, _, err := f.run(Filters{Command: CmdProfiles, Agents: []string{"codex"}, AgentSeen: true})
	if err == nil || !strings.Contains(err.Error(), "not profiles") {
		t.Fatalf("want profiles filter rejection, got %v", err)
	}
}

func TestGatingListRejectsFiltersAndPrintsVerbatim(t *testing.T) {
	f := newFixture(t)
	body := "# a comment\n[[installs]]\nsource = \"example/a\"\nagents = [\"codex\"]\nprofiles = [\"global\"]\n"
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
	f.writeManifest(repoEntry)
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
	f.writeManifest(globalEntry)
	_, _, err := f.run(Filters{Command: CmdApply})
	if err == nil || !strings.Contains(err.Error(), "interactive selection is not supported") {
		t.Fatalf("want interactive-not-supported error, got %v", err)
	}
}

func TestGlobalProfileCannotBeSelected(t *testing.T) {
	f := newFixture(t)
	f.writeManifest(globalEntry)
	_, _, err := f.run(Filters{Command: CmdDryRun, Profiles: []string{"global"}, ProfileSeen: true, NonInteractive: true})
	if err == nil || !strings.Contains(err.Error(), `"global" is a reserved profile keyword`) {
		t.Fatalf("want reserved-global error, got %v", err)
	}
}

// --- profiles selection ------------------------------------------------------

func TestProfilesCommandSortedDedupedAndExcludesGlobal(t *testing.T) {
	f := newFixture(t)
	f.writeManifest(`
[[installs]]
source = "example/a"
profiles = ["global", "base", "ark-mlops"]

[[installs]]
source = "example/b"
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
	f.writeManifest("[[installs]]\nsource = \"example/a\"\nprofiles = [\"global\"]\n")
	_, _, err := f.run(Filters{Command: CmdProfiles})
	if err == nil || !strings.Contains(err.Error(), "no selectable profiles declared") {
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
profiles = ["ark-mlops", "maas"]

[[installs]]
source = "example/skip"
agents = ["codex"]
skills = ["web"]
profiles = ["frontend"]
`)
	out, _, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
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
profiles = ["ark-mlops"]

[[installs]]
source = "example/web"
agents = ["codex"]
skills = ["browser"]
profiles = ["frontend"]
`)
	out, _, err := f.run(Filters{
		Command:  CmdDryRun,
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
profiles = ["ark-mlops"]

[[installs]]
source = "example/skip"
agents = ["trae"]
skills = ["web"]
profiles = ["ark-mlops"]
`)

	t.Run("repo agents narrow entries", func(t *testing.T) {
		f.writeRepo("profiles = [\"ark-mlops\"]\nagents = [\"codex\"]\n")
		out, _, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
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
		out, _, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
		if err != nil {
			t.Fatalf("dry-run failed: %v", err)
		}
		if !strings.Contains(out, "-a codex -a opencode") || !strings.Contains(out, "-a trae") {
			t.Fatalf("entry agents not used:\n%s", out)
		}
	})
}

func TestGlobalEntryIgnoresRepoAgentsNarrowing(t *testing.T) {
	f := newFixture(t)
	f.writeRepo("profiles = [\"base\"]\nagents = [\"codex\"]\n")
	f.writeManifest(`
[[installs]]
source = "example/global"
agents = ["codex", "opencode"]
skills = ["lane"]
profiles = ["global"]
`)
	out, _, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out, "-a opencode") {
		t.Fatalf("global entries must not be narrowed by repo agents:\n%s", out)
	}
}

func TestSkillFilterExcludesWildcardAutoMatch(t *testing.T) {
	f := newFixture(t)
	f.writeManifest(`
[[installs]]
source = "example/wild"
agents = ["codex"]
skills = ["*"]
profiles = ["global"]

[[installs]]
source = "example/named"
agents = ["codex"]
skills = ["lane"]
profiles = ["global"]
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

func TestResolveShowsRepoLevelEntries(t *testing.T) {
	f := newFixture(t)
	f.writeRepo(`profiles = ["base"]`)
	f.writeManifest(globalEntry + repoEntry)
	out, _, err := f.run(Filters{Command: CmdResolve, NonInteractive: true})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if !strings.Contains(out, "example/ark") {
		t.Fatalf("resolve must show repo-level entries:\n%s", out)
	}
	if strings.Contains(out, "example/global") {
		t.Fatalf("resolve is repo-level only, must not show global entries:\n%s", out)
	}
	if !strings.Contains(out, "profiles: base") {
		t.Fatalf("resolve must print the entry profiles:\n%s", out)
	}
}

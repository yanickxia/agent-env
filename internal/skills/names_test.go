package skills

import (
	"strings"
	"testing"
)

const nameSelectionManifest = `
[[installs]]
source = "example/global"
agents = ["codex"]
skills = ["g"]
profiles = ["global"]

[[installs]]
source = "example/bytedance"
agents = ["codex"]
skills = ["bd"]
profiles = ["bytedance"]

[[installs]]
source = "example/clickup"
name = "clickup"
agents = ["codex", "opencode"]
skills = ["cup"]

[[installs]]
source = "example/lone"
name = "lone"
agents = ["codex"]
skills = ["lone"]

[[installs]]
source = "example/named-group"
name = "named-group"
agents = ["codex"]
skills = ["ng"]
profiles = ["bytedance"]
`

func TestOrphanEntriesSkippedWithoutSelection(t *testing.T) {
	f := newFixture(t)
	f.writeManifest(nameSelectionManifest)
	out, errb, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out, "example/global") {
		t.Fatalf("global must install:\n%s", out)
	}
	for _, absent := range []string{"example/bytedance", "example/clickup", "example/lone", "example/named-group"} {
		if strings.Contains(out, absent) {
			t.Fatalf("%s must be skipped without selection:\n%s", absent, out)
		}
	}
	if !strings.Contains(errb, "skipped 4 repo-level entries") {
		t.Fatalf("want 4 skips, got %q", errb)
	}
}

func TestNameSelectionPicksOrphans(t *testing.T) {
	f := newFixture(t)
	f.writeRepo("names = [\"clickup\"]\n")
	f.writeManifest(nameSelectionManifest)
	out, errb, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out, "example/clickup") {
		t.Fatalf("named orphan must install:\n%s", out)
	}
	for _, absent := range []string{"example/bytedance", "example/lone", "example/named-group"} {
		if strings.Contains(out, absent) {
			t.Fatalf("%s must not be selected by names only:\n%s", absent, out)
		}
	}
	if strings.Contains(errb, "skipped") {
		t.Fatalf("a selection exists; no skip notice expected: %q", errb)
	}
}

func TestNameBypassesProfileGroup(t *testing.T) {
	f := newFixture(t)
	// named-group has profiles=["bytedance"] but no profiles selection: the name
	// must still select it.
	f.writeRepo("names = [\"named-group\"]\n")
	f.writeManifest(nameSelectionManifest)
	out, _, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out, "example/named-group") {
		t.Fatalf("name must bypass the profile gate:\n%s", out)
	}
	if strings.Contains(out, "example/bytedance") {
		t.Fatalf("group entry must not be selected without its profile:\n%s", out)
	}
}

func TestProfilesAndNamesCombined(t *testing.T) {
	f := newFixture(t)
	f.writeRepo("profiles = [\"bytedance\"]\nnames = [\"lone\"]\n")
	f.writeManifest(nameSelectionManifest)
	out, _, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	for _, want := range []string{"example/bytedance", "example/named-group", "example/lone"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%s must be selected by profiles+names:\n%s", want, out)
		}
	}
	if strings.Contains(out, "example/clickup") {
		t.Fatalf("clickup not selected here:\n%s", out)
	}
}

func TestNameSelectedEntryHonorsRepoAgentsNarrowing(t *testing.T) {
	f := newFixture(t)
	f.writeRepo("names = [\"clickup\"]\nagents = [\"codex\"]\n")
	f.writeManifest(nameSelectionManifest)
	out, _, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out, "example/clickup") {
		t.Fatalf("clickup must be selected:\n%s", out)
	}
	if !strings.Contains(out, "-a codex") || strings.Contains(out, "-a opencode") {
		t.Fatalf("repo agents must narrow the name-selected entry:\n%s", out)
	}
}

func TestResolveShowsNames(t *testing.T) {
	f := newFixture(t)
	f.writeRepo("profiles = [\"bytedance\"]\nnames = [\"clickup\"]\n")
	f.writeManifest(nameSelectionManifest)
	out, _, err := f.run(Filters{Command: CmdResolve, NonInteractive: true})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if !strings.Contains(out, "names: clickup") {
		t.Fatalf("resolve must print names line:\n%s", out)
	}
	if !strings.Contains(out, "name: clickup") {
		t.Fatalf("resolve must print each entry name:\n%s", out)
	}
	if !strings.Contains(out, "example/named-group") {
		t.Fatalf("resolve should include profile+name entries:\n%s", out)
	}
}

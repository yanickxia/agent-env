package config

import (
	"strings"
	"testing"
)

func TestParseManifestInstallName(t *testing.T) {
	entries, err := parseBody(t, `
[[installs]]
source = "example/clickup"
name = "clickup"
profiles = ["global"]
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries[0].Name != "clickup" {
		t.Fatalf("name = %q", entries[0].Name)
	}

	// optional: missing name is fine
	entries, err = parseBody(t, "[[installs]]\nsource = \"example/x\"\nprofiles = [\"base\"]\n")
	if err != nil || entries[0].Name != "" {
		t.Fatalf("name must be optional, got %q err=%v", entries[0].Name, err)
	}

	// non-kebab rejected
	_, err = parseBody(t, "[[installs]]\nsource = \"example/x\"\nname = \"Click_Up\"\n")
	if err == nil || !strings.Contains(err.Error(), "must be kebab-case") {
		t.Fatalf("want kebab error, got %v", err)
	}

	// duplicate across installs rejected
	_, err = parseBody(t, `
[[installs]]
source = "example/a"
name = "dup"

[[installs]]
source = "example/b"
name = "dup"
`)
	if err == nil || !strings.Contains(err.Error(), "duplicate install name \"dup\"") {
		t.Fatalf("want duplicate error, got %v", err)
	}
}

func TestParseManifestOrphanProfilesOptional(t *testing.T) {
	// No profiles field -> orphan (not global, no profiles), still valid.
	entries, err := parseBody(t, "[[installs]]\nsource = \"example/orphan\"\nname = \"orphan\"\n")
	if err != nil {
		t.Fatalf("orphan install must parse: %v", err)
	}
	if entries[0].Global || len(entries[0].Profiles) != 0 {
		t.Fatalf("expected orphan (non-global, no profiles): %+v", entries[0])
	}

	// Empty array is also orphan.
	entries, err = parseBody(t, "[[installs]]\nsource = \"example/orphan2\"\nprofiles = []\n")
	if err != nil || entries[0].Global || len(entries[0].Profiles) != 0 {
		t.Fatalf("empty profiles must be orphan: %+v err=%v", entries[0], err)
	}
}

func TestParseServersOrphanProfilesOptional(t *testing.T) {
	rows, err := parseServersBody(t, "[[servers]]\nname = \"playwright\"\ncommand = \"npx\"\n", nil, nil)
	if err != nil {
		t.Fatalf("orphan server must parse: %v", err)
	}
	if rows[0].Global || len(rows[0].Profiles) != 0 {
		t.Fatalf("expected orphan server: %+v", rows[0])
	}
}

func TestLoadRepoConfigNames(t *testing.T) {
	cfg, found, err := LoadRepoConfig(writeRepo(t, "names = [\"clickup\", \"playwright\"]\n"))
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if strings.Join(cfg.Names, ",") != "clickup,playwright" {
		t.Fatalf("names = %v", cfg.Names)
	}
	if len(cfg.Profiles) != 0 {
		t.Fatalf("profiles should be empty: %v", cfg.Profiles)
	}

	// profiles optional alongside names
	cfg, _, err = LoadRepoConfig(writeRepo(t, "profiles = [\"base\"]\nnames = [\"clickup\"]\n"))
	if err != nil || strings.Join(cfg.Profiles, ",") != "base" || strings.Join(cfg.Names, ",") != "clickup" {
		t.Fatalf("combined selectors wrong: profiles=%v names=%v err=%v", cfg.Profiles, cfg.Names, err)
	}

	// both omitted = no selection (valid)
	_, found, err = LoadRepoConfig(writeRepo(t, "agents = [\"codex\"]\n"))
	if err != nil || !found {
		t.Fatalf("profiles-less repo config must be valid: found=%v err=%v", found, err)
	}

	// global reserved in names
	_, _, err = LoadRepoConfig(writeRepo(t, "names = [\"global\"]\n"))
	if err == nil || !strings.Contains(err.Error(), "reserved keyword") {
		t.Fatalf("want reserved-name error, got %v", err)
	}
}

func TestMergeNamesNearestFirst(t *testing.T) {
	parent := &RepoConfig{Names: []string{"a", "b"}}
	child := &RepoConfig{Names: []string{"b", "c"}}
	merged := MergeRepoConfigs([]*RepoConfig{child, parent})
	if strings.Join(merged.Names, ",") != "b,c,a" {
		t.Fatalf("merged names = %v, want b,c,a", merged.Names)
	}
}

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLayer(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func noEnv(string) string { return "" }

func TestWalkRepoConfigsNearestFirstAndMerges(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	repo := filepath.Join(parent, "repo")
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLayer(t, filepath.Join(parent, ".agent-env.toml"), `profiles = ["ark-mlops"]`)
	writeLayer(t, filepath.Join(repo, ".agent-env.toml"), "profiles = [\"base\"]\nagents = [\"codex\"]\n")

	cfg, layers, found, err := LoadRepoSelection("", sub, noEnv, "hint")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if len(layers) != 2 || layers[0] != filepath.Join(repo, ".agent-env.toml") || layers[1] != filepath.Join(parent, ".agent-env.toml") {
		t.Fatalf("layers nearest-first wrong: %v", layers)
	}
	// profiles union, nearest priority: child "base" first, then parent "ark-mlops".
	if strings.Join(cfg.Profiles, ",") != "base,ark-mlops" {
		t.Fatalf("merged profiles = %v", cfg.Profiles)
	}
	if !cfg.AgentsDeclared || strings.Join(cfg.Agents, ",") != "codex" {
		t.Fatalf("agents = %v declared=%v", cfg.Agents, cfg.AgentsDeclared)
	}
}

func TestMergeAgentsNearestWins(t *testing.T) {
	parent := &RepoConfig{Profiles: []string{"p"}, Agents: []string{"trae"}, AgentsDeclared: true}
	child := &RepoConfig{Profiles: []string{"c"}, Agents: []string{"codex"}, AgentsDeclared: true}

	merged := MergeRepoConfigs([]*RepoConfig{child, parent})
	if strings.Join(merged.Agents, ",") != "codex" {
		t.Fatalf("nearest agents must win, got %v", merged.Agents)
	}

	// Only parent declares -> inherited.
	onlyParent := MergeRepoConfigs([]*RepoConfig{{Profiles: []string{"c"}}, parent})
	if !onlyParent.AgentsDeclared || strings.Join(onlyParent.Agents, ",") != "trae" {
		t.Fatalf("parent agents not inherited: %v declared=%v", onlyParent.Agents, onlyParent.AgentsDeclared)
	}

	// Nobody declares -> empty, not declared.
	none := MergeRepoConfigs([]*RepoConfig{{Profiles: []string{"c"}}, {Profiles: []string{"p"}}})
	if none.AgentsDeclared || len(none.Agents) != 0 {
		t.Fatalf("expected no agents, got %v declared=%v", none.Agents, none.AgentsDeclared)
	}
}

func TestMergeModeAndVarsNearestOverride(t *testing.T) {
	parent := &RepoConfig{Profiles: []string{"p"}, Mode: "copy", Vars: map[string]any{"shared": "parent", "onlyparent": "1"}}
	child := &RepoConfig{Profiles: []string{"c"}, Vars: map[string]any{"shared": "child"}}

	merged := MergeRepoConfigs([]*RepoConfig{child, parent})
	if merged.Mode != "copy" {
		t.Fatalf("mode should inherit parent when child omits it, got %q", merged.Mode)
	}
	if merged.Vars["shared"] != "child" || merged.Vars["onlyparent"] != "1" {
		t.Fatalf("vars merge wrong: %v", merged.Vars)
	}

	withChildMode := MergeRepoConfigs([]*RepoConfig{{Profiles: []string{"c"}, Mode: "symlink"}, parent})
	if withChildMode.Mode != "symlink" {
		t.Fatalf("child mode must win, got %q", withChildMode.Mode)
	}
}

func TestWalkInvalidLayerErrorsWithPath(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	repo := filepath.Join(parent, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLayer(t, filepath.Join(parent, ".agent-env.toml"), `profiles = ["base"]`)
	bad := filepath.Join(repo, ".agent-env.toml")
	writeLayer(t, bad, "profiles = [\"Bad_Name\"]\n") // non-kebab profile is invalid

	_, _, err := WalkRepoConfigs(repo, "hint")
	if err == nil || !strings.Contains(err.Error(), bad) {
		t.Fatalf("invalid layer must fail with its path, got %v", err)
	}
}

func TestLoadRepoSelectionExplicitForcesSingleFile(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	repo := filepath.Join(parent, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLayer(t, filepath.Join(parent, ".agent-env.toml"), `profiles = ["parent"]`)
	explicit := filepath.Join(root, "explicit.toml")
	writeLayer(t, explicit, `profiles = ["only"]`)

	cfg, layers, found, err := LoadRepoSelection(explicit, repo, noEnv, "hint")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if len(layers) != 1 || layers[0] != explicit {
		t.Fatalf("explicit mode must not walk: %v", layers)
	}
	if strings.Join(cfg.Profiles, ",") != "only" {
		t.Fatalf("explicit profiles = %v", cfg.Profiles)
	}
}

func TestLoadRepoSelectionEnvOverrideForcesSingleFile(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	envPath := filepath.Join(root, "env.toml")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLayer(t, filepath.Join(parent, ".agent-env.toml"), `profiles = ["parent"]`)
	writeLayer(t, envPath, `profiles = ["from-env"]`)

	getenv := func(k string) string {
		if k == "AGENT_ENV_REPO_CONFIG" {
			return envPath
		}
		return ""
	}
	cfg, layers, found, err := LoadRepoSelection("", parent, getenv, "hint")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if len(layers) != 1 || layers[0] != envPath || strings.Join(cfg.Profiles, ",") != "from-env" {
		t.Fatalf("env override must be single-file: layers=%v profiles=%v", layers, cfg.Profiles)
	}
}

func TestLoadRepoSelectionNoLayers(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	_, layers, found, err := LoadRepoSelection("", sub, noEnv, "hint")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if found || len(layers) != 0 {
		t.Fatalf("expected no layers, found=%v layers=%v", found, layers)
	}
}

// $HOME-level config applies to a cwd underneath HOME.
func TestHomeLevelConfigApplies(t *testing.T) {
	home := t.TempDir()
	sub := filepath.Join(home, "codes", "team", "repo", "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLayer(t, filepath.Join(home, ".agent-env.toml"), `profiles = ["base"]`)

	cfg, layers, found, err := LoadRepoSelection("", sub, noEnv, "hint")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if len(layers) != 1 || layers[0] != filepath.Join(home, ".agent-env.toml") {
		t.Fatalf("home layer not found: %v", layers)
	}
	if strings.Join(cfg.Profiles, ",") != "base" {
		t.Fatalf("profiles = %v", cfg.Profiles)
	}
}

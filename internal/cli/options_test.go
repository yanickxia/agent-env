package cli

import (
	"path/filepath"
	"testing"
)

func TestResolveOptionsEnvPriority(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "") // make git unavailable so the cwd is the project root
	t.Setenv("AGENT_ENV_CONFIG", "/canonical/config.toml")
	t.Setenv("AGENT_SKILLS_MANIFEST", "/legacy/manifest.toml")
	t.Setenv("AGENT_SKILLS_STATE", "/state/state.tsv")
	t.Setenv("AGENT_ENV_REPO_CONFIG", "/repo/.agent-env.toml")
	t.Setenv("AGENT_SKILLS_REPO_CONFIG", "/legacy-repo/.agent-env.toml")

	opts := resolveOptions()
	if opts.ManifestPath != "/canonical/config.toml" {
		t.Fatalf("manifest = %q", opts.ManifestPath)
	}
	if opts.StatePath != "/state/state.tsv" {
		t.Fatalf("state = %q", opts.StatePath)
	}
	if opts.RepoConfigPath != "/repo/.agent-env.toml" {
		t.Fatalf("repo config = %q", opts.RepoConfigPath)
	}
	if opts.Home != home {
		t.Fatalf("home = %q want %q", opts.Home, home)
	}
	if opts.ProjectRoot == "" {
		t.Fatal("project root must be resolved")
	}
}

func TestResolveOptionsDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	for _, k := range []string{
		"AGENT_ENV_CONFIG", "AGENT_SKILLS_MANIFEST", "AGENT_SKILLS_STATE",
		"AGENT_ENV_REPO_CONFIG", "AGENT_SKILLS_REPO_CONFIG",
	} {
		t.Setenv(k, "")
	}

	opts := resolveOptions()
	if opts.ManifestPath != filepath.Join(home, ".config", "agent-env", "config.toml") {
		t.Fatalf("manifest default = %q", opts.ManifestPath)
	}
	if opts.StatePath != filepath.Join(home, ".local", "state", "agent-skills-sync", "state.tsv") {
		t.Fatalf("state default = %q", opts.StatePath)
	}
	if filepath.Base(opts.RepoConfigPath) != ".agent-env.toml" {
		t.Fatalf("repo config default = %q", opts.RepoConfigPath)
	}
}

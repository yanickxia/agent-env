package config

import (
	"path/filepath"
	"testing"
)

func envFrom(m map[string]string) GetenvFunc {
	return func(k string) string { return m[k] }
}

func TestManifestPathPrecedence(t *testing.T) {
	home := "/home/u"
	def := filepath.Join(home, ".config", "agent-env", "config.toml")

	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "default",
			env:  map[string]string{},
			want: def,
		},
		{
			name: "legacy alias",
			env:  map[string]string{"AGENT_SKILLS_MANIFEST": "/legacy/m.toml"},
			want: "/legacy/m.toml",
		},
		{
			name: "canonical wins over legacy",
			env: map[string]string{
				"AGENT_ENV_CONFIG":      "/canonical/c.toml",
				"AGENT_SKILLS_MANIFEST": "/legacy/m.toml",
			},
			want: "/canonical/c.toml",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ManifestPath(envFrom(tc.env), home); got != tc.want {
				t.Fatalf("ManifestPath = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStatePath(t *testing.T) {
	home := "/home/u"
	def := filepath.Join(home, ".local", "state", "agent-skills-sync", "state.tsv")
	if got := StatePath(envFrom(nil), home); got != def {
		t.Fatalf("default = %q want %q", got, def)
	}
	if got := StatePath(envFrom(map[string]string{"AGENT_SKILLS_STATE": "/tmp/s.tsv"}), home); got != "/tmp/s.tsv" {
		t.Fatalf("override = %q", got)
	}
}

func TestRepoConfigPathPrecedence(t *testing.T) {
	root := "/repo/x"
	def := filepath.Join(root, ".agent-env.toml")

	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"default", map[string]string{}, def},
		{"legacy", map[string]string{"AGENT_SKILLS_REPO_CONFIG": "/l.toml"}, "/l.toml"},
		{
			"canonical wins",
			map[string]string{
				"AGENT_ENV_REPO_CONFIG":    "/c.toml",
				"AGENT_SKILLS_REPO_CONFIG": "/l.toml",
			},
			"/c.toml",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RepoConfigPath(envFrom(tc.env), root); got != tc.want {
				t.Fatalf("RepoConfigPath = %q want %q", got, tc.want)
			}
		})
	}
}

func TestExpandSource(t *testing.T) {
	if got := ExpandSource("/home/u", "~/skills"); got != filepath.Join("/home/u", "skills") {
		t.Fatalf("tilde expansion = %q", got)
	}
	if got := ExpandSource("/home/u", "example/repo"); got != "example/repo" {
		t.Fatalf("no expansion expected = %q", got)
	}
	if got := ExpandSource("/home/u", "~other/repo"); got != "~other/repo" {
		t.Fatalf("only ~/ is expanded = %q", got)
	}
}

func TestSecretsAndClaudeJSONPath(t *testing.T) {
	home := "/home/u"
	def := filepath.Join(home, ".config", "agent-env", "secrets.toml")
	if got := SecretsPath(envFrom(nil), home); got != def {
		t.Fatalf("secrets default = %q want %q", got, def)
	}
	if got := SecretsPath(envFrom(map[string]string{"AGENT_MCP_SECRETS": "/s.toml"}), home); got != "/s.toml" {
		t.Fatalf("secrets override = %q", got)
	}

	claudeDef := filepath.Join(home, ".claude.json")
	if got := ClaudeJSONPath(envFrom(nil), home); got != claudeDef {
		t.Fatalf("claude default = %q want %q", got, claudeDef)
	}
	if got := ClaudeJSONPath(envFrom(map[string]string{"CLAUDE_JSON": "/c.json"}), home); got != "/c.json" {
		t.Fatalf("claude override = %q", got)
	}
}

func TestProjectRootFallsBackToDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", "") // make git unavailable
	if got := ProjectRoot(dir); got != dir {
		t.Fatalf("ProjectRoot = %q want %q", got, dir)
	}
}

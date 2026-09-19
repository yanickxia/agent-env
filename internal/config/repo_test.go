package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRepo(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".agent-env.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadRepoConfigValid(t *testing.T) {
	path := writeRepo(t, `
profiles = ["base", "ark-mlops", "base"]
agents = ["codex", "opencode"]
mode = "copy"

[vars]
team = "ark"
`)
	cfg, found, err := LoadRepoConfig(path)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if strings.Join(cfg.Profiles, ",") != "base,ark-mlops" {
		t.Fatalf("profiles = %v", cfg.Profiles)
	}
	if strings.Join(cfg.Agents, ",") != "codex,opencode" || cfg.Mode != "copy" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.Vars["team"] != "ark" {
		t.Fatalf("vars = %v", cfg.Vars)
	}
}

func TestLoadRepoConfigMissingIsNotAnError(t *testing.T) {
	cfg, found, err := LoadRepoConfig(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if found || cfg != nil {
		t.Fatalf("found=%v cfg=%v, want missing", found, cfg)
	}
}

func TestLoadRepoConfigErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "unknown key",
			body: "profiles = [\"base\"]\nsource = \"evil\"\n",
			want: "only allows agents, mode, names, profiles, vars; found source",
		},
		{
			name: "non kebab",
			body: "profiles = [\"Base\"]\n",
			want: "profile names must be kebab-case",
		},
		{
			name: "bad mode",
			body: "profiles = [\"base\"]\nmode = \"hardlink\"\n",
			want: `"mode" must be "symlink" or "copy"`,
		},
		{
			name: "vars not table",
			body: "profiles = [\"base\"]\nvars = \"x\"\n",
			want: `"vars" must be a table`,
		},
		{
			name: "agents wrong element type",
			body: "profiles = [\"base\"]\nagents = [1]\n",
			want: `"agents" must be a string array`,
		},
		{
			name: "not toml",
			body: "profiles = [\n",
			want: "not valid TOML",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := LoadRepoConfig(writeRepo(t, tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
			if !strings.HasPrefix(err.Error(), Prog+": ") {
				t.Fatalf("error must be prefixed: %q", err.Error())
			}
		})
	}
}

// A bare string is accepted for profiles, matching the zsh normalizer.
func TestLoadRepoConfigScalarProfiles(t *testing.T) {
	cfg, found, err := LoadRepoConfig(writeRepo(t, `profiles = "base"`))
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if len(cfg.Profiles) != 1 || cfg.Profiles[0] != "base" {
		t.Fatalf("profiles = %v", cfg.Profiles)
	}
	if len(cfg.Agents) != 0 {
		t.Fatalf("agents should be empty, got %v", cfg.Agents)
	}
}

func TestParseSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.toml")
	if err := os.WriteFile(path, []byte("foo_token = \"abc\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := ParseSecrets(path)
	if err != nil {
		t.Fatal(err)
	}
	if m["foo_token"] != "abc" {
		t.Fatalf("secrets = %v", m)
	}
	if _, err := ParseSecrets(filepath.Join(t.TempDir(), "absent.toml")); err != nil {
		t.Fatalf("missing secrets must not error: %v", err)
	}
}

func TestLoadRepoConfigRejectsGlobalProfile(t *testing.T) {
	_, _, err := LoadRepoConfig(writeRepo(t, "profiles = [\"base\", \"global\"]\n"))
	if err == nil || !strings.Contains(err.Error(), `"global" is a reserved profile keyword`) {
		t.Fatalf("want reserved-global error, got %v", err)
	}
}

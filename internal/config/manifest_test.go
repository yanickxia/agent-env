package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parseBody(t *testing.T, body string) ([]Install, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return ParseManifest(path)
}

func TestParseManifestValid(t *testing.T) {
	body := `
[[installs]]
source = "example/full"
agents = ["claude-code", "codex"]
skills = ["a", "b"]
mode = "symlink"
profiles = ["global", "base", "ark-mlops", "base"]
post_install = [
  { run = "npm install -g x", if_missing = "x" },
  "echo bare",
]
env = { B_KEY = "2", A_KEY = "1" }
`
	entries, err := parseBody(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Source != "example/full" || e.Mode != "symlink" || e.Installer != "skills" {
		t.Fatalf("unexpected normalized entry: %+v", e)
	}
	if !e.Global {
		t.Fatalf("profiles containing global must set Global=true: %+v", e)
	}
	if e.AgentsRaw != "claude-code,codex" || e.SkillsRaw != "a,b" {
		t.Fatalf("raw fields = %q / %q", e.AgentsRaw, e.SkillsRaw)
	}
	if e.ProfilesRaw != "global,base,ark-mlops" {
		t.Fatalf("profiles dedupe/order = %q", e.ProfilesRaw)
	}
	if len(e.PostInstall) != 2 || e.PostInstall[0].IfMissing != "x" || e.PostInstall[1].IfMissing != "" {
		t.Fatalf("post_install = %+v", e.PostInstall)
	}
	if len(e.Env) != 2 || e.Env[0].Key != "A_KEY" || e.Env[1].Key != "B_KEY" {
		t.Fatalf("env must be key-sorted: %+v", e.Env)
	}
	// env payload: key <GS> value, records joined by <RS>, sorted by key.
	wantEnv := "A_KEY" + SepGS + "1" + SepRS + "B_KEY" + SepGS + "2"
	if e.EnvRaw != wantEnv {
		t.Fatalf("env raw = %q want %q", e.EnvRaw, wantEnv)
	}
}

func TestParseManifestScalarListAccepted(t *testing.T) {
	entries, err := parseBody(t, `
[[installs]]
source = "example/s"
agents = "codex"
skills = "lane"
profiles = "base"
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := entries[0]
	if got.AgentsRaw != "codex" || got.SkillsRaw != "lane" || got.ProfilesRaw != "base" {
		t.Fatalf("scalar normalization = %+v", got)
	}
	if got.Global {
		t.Fatalf("repo-level entry must not be global: %+v", got)
	}
}

func TestParseManifestErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "scope removed",
			body: "[[installs]]\nsource = \"example/a\"\nscope = \"user\"\nprofiles = [\"base\"]\n",
			want: `"scope" was removed`,
		},
		{
			name: "scope removed array",
			body: "[[installs]]\nsource = \"example/a\"\nscope = [\"project\"]\nprofiles = [\"base\"]\n",
			want: `"scope" was removed`,
		},
		{
			name: "agents wrong type",
			body: "[[installs]]\nsource = \"example/a\"\nagents = [1, 2]\nprofiles = [\"base\"]\n",
			want: `"agents" must be a string or array`,
		},
		{
			name: "skills wrong type",
			body: "[[installs]]\nsource = \"example/a\"\nskills = 5\nprofiles = [\"base\"]\n",
			want: `"skills" must be a string or array`,
		},
		{
			name: "mode wrong type",
			body: "[[installs]]\nsource = \"example/a\"\nmode = 3\nprofiles = [\"base\"]\n",
			want: `"mode" must be a string`,
		},
		{
			name: "profiles non kebab",
			body: "[[installs]]\nsource = \"example/a\"\nprofiles = [\"Ark_ML\"]\n",
			want: "profile names must be kebab-case",
		},
		{
			name: "post_install not array",
			body: "[[installs]]\nsource = \"example/a\"\nprofiles = [\"base\"]\npost_install = \"x\"\n",
			want: `"post_install" must be an array`,
		},
		{
			name: "post_install unknown key",
			body: "[[installs]]\nsource = \"example/a\"\nprofiles = [\"base\"]\npost_install = [{ run = \"x\", extra = \"y\" }]\n",
			want: "unknown post_install key(s) extra",
		},
		{
			name: "post_install missing run",
			body: "[[installs]]\nsource = \"example/a\"\nprofiles = [\"base\"]\npost_install = [{ if_missing = \"x\" }]\n",
			want: `post_install "run" must be a non-empty string`,
		},
		{
			name: "installer agentbuddy rejected",
			body: "[[installs]]\nsource = \"example/a\"\nprofiles = [\"base\"]\ninstaller = \"agentbuddy\"\n",
			want: `installer "agentbuddy" is not supported`,
		},
		{
			name: "installer unknown",
			body: "[[installs]]\nsource = \"example/a\"\nprofiles = [\"base\"]\ninstaller = \"nope\"\n",
			want: `unsupported installer "nope"`,
		},
		{
			name: "env not table",
			body: "[[installs]]\nsource = \"example/a\"\nprofiles = [\"base\"]\nenv = [1]\n",
			want: `"env" must be a table`,
		},
		{
			name: "env non-string value",
			body: "[[installs]]\nsource = \"example/a\"\nprofiles = [\"base\"]\nenv = { N = 1 }\n",
			want: `env value for "N" must be a string`,
		},
		{
			name: "missing source",
			body: "[[installs]]\nprofiles = [\"base\"]\n",
			want: `must include a non-empty "source"`,
		},
		{
			name: "installs missing",
			body: "[other]\nx = 1\n",
			want: `must contain an "installs" array`,
		},
		{
			name: "installs wrong type",
			body: "installs = \"nope\"\n",
			want: `must contain an "installs" array`,
		},
		{
			name: "entry not a table",
			body: "installs = [\"nope\"]\n",
			want: "each install entry must be a table",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseBody(t, tc.body)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
			if !strings.HasPrefix(err.Error(), Prog+": ") {
				t.Fatalf("error must be prefixed with %q: %q", Prog+": ", err.Error())
			}
		})
	}
}

func TestParseManifestMissingFile(t *testing.T) {
	_, err := ParseManifest(filepath.Join(t.TempDir(), "nope.toml"))
	if err == nil || !strings.Contains(err.Error(), "manifest not found") {
		t.Fatalf("want manifest not found error, got %v", err)
	}
}

func TestParseManifestIgnoresServers(t *testing.T) {
	entries, err := parseBody(t, `
[[installs]]
source = "example/a"
profiles = ["global"]

[[servers]]
name = "srv"
type = "stdio"
command = "npx"
profiles = ["base"]
`)
	if err != nil {
		t.Fatalf("servers table must be ignored: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 install, got %d", len(entries))
	}
}

func TestParseManifestEntryCount(t *testing.T) {
	entries, err := ParseManifest(filepath.Join("..", "stamp", "testdata", "config.toml"))
	if err != nil {
		t.Fatalf("parse real config snapshot: %v", err)
	}
	if len(entries) != 9 {
		t.Fatalf("want 9 installs in real snapshot, got %d", len(entries))
	}
	if entries[0].Source != "microsoft/playwright-cli" {
		t.Fatalf("first source = %q", entries[0].Source)
	}
	if !entries[0].Global {
		t.Fatalf("fixture installs should be global entries")
	}
}

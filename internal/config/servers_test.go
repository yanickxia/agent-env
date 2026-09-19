package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resolverFor(secrets, env map[string]string) SecretResolver {
	return func(key string) string {
		if key == "" {
			return ""
		}
		if v, ok := secrets[key]; ok {
			return v
		}
		if v, ok := secrets[strings.ToLower(key)]; ok {
			return v
		}
		return env[key]
	}
}

func parseServersBody(t *testing.T, body string, secrets, env map[string]string) ([]Server, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return ParseServers(path, resolverFor(secrets, env))
}

func TestParseServersValid(t *testing.T) {
	body := `
[[installs]]
source = "ignored/skills-entry"
scope = "user"

[[servers]]
name = "srv-full"
agents = ["codex", "claude"]
type = "stdio"
command = "npx"
args = ["-y", "fake@latest", "${TOKEN}"]
scope = ["project", "user", "project"]
profiles = ["base", "ark-mlops"]
env_vars = ["A", "B"]
startup_timeout_sec = 20
[servers.env]
ZED = "z"
ALPHA = "${SECRET_A}"
[servers.headers]
Auth = "Bearer ${TOKEN}"

[[servers]]
name = "srv-http"
agents = ["trae"]
type = "streamable-http"
url = "https://example.test/${PATH_TOKEN}"
scope = "project"
bearer_token_env_var = "BT"
`
	secrets := map[string]string{"secret_a": "sekret", "path_token": "ptok"}
	env := map[string]string{"TOKEN": "envtok"}
	rows, err := parseServersBody(t, body, secrets, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// installs ignored; 2 scopes for srv-full + 1 for srv-http = 3 rows.
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d: %+v", len(rows), rows)
	}
	full := rows[0]
	if full.Name != "srv-full" || full.Scope != "project" || rows[1].Scope != "user" {
		t.Fatalf("scope expansion/dedupe wrong: %+v / %+v", rows[0], rows[1])
	}
	if got := strings.Join(full.Args, ","); got != "-y,fake@latest,envtok" {
		t.Fatalf("args expansion = %q", got)
	}
	// Document order for env/headers must be preserved.
	if len(full.Env) != 2 || full.Env[0].Key != "ZED" || full.Env[1].Key != "ALPHA" {
		t.Fatalf("env order = %+v", full.Env)
	}
	if full.Env[1].Value != "sekret" {
		t.Fatalf("env secret expansion = %q", full.Env[1].Value)
	}
	if len(full.Headers) != 1 || full.Headers[0].Value != "Bearer envtok" {
		t.Fatalf("header expansion = %+v", full.Headers)
	}
	if full.StartupTimeoutSec == nil || *full.StartupTimeoutSec != 20 {
		t.Fatalf("timeout = %v", full.StartupTimeoutSec)
	}
	http := rows[2]
	if http.URL != "https://example.test/ptok" || http.BearerTokenEnvVar != "BT" {
		t.Fatalf("http row = %+v", http)
	}
	if http.StartupTimeoutSec != nil {
		t.Fatalf("http timeout should be nil")
	}
}

func TestParseServersErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"missing name", "[[servers]]\nscope = \"user\"\n", `must include a non-empty "name"`},
		{"missing scope", "[[servers]]\nname = \"s\"\n", `must declare "scope"`},
		{"empty scope array", "[[servers]]\nname = \"s\"\nscope = []\n", `"scope" array must not be empty`},
		{"scope global", "[[servers]]\nname = \"s\"\nscope = \"global\"\n", `got "global" (use "user")`},
		{"scope local", "[[servers]]\nname = \"s\"\nscope = \"local\"\n", `(use "project")`},
		{"scope wrong type", "[[servers]]\nname = \"s\"\nscope = [1]\n", `"scope" must be a string or an array of strings`},
		{"agents not array", "[[servers]]\nname = \"s\"\nscope = \"user\"\nagents = \"codex\"\n", `"agents" must be a string array`},
		{"profiles not array", "[[servers]]\nname = \"s\"\nscope = \"user\"\nprofiles = \"base\"\n", `"profiles" must be a string array`},
		{"type wrong type", "[[servers]]\nname = \"s\"\nscope = \"user\"\ntype = 3\n", `"type" must be a string`},
		{"command wrong type", "[[servers]]\nname = \"s\"\nscope = \"user\"\ncommand = 3\n", `"command" must be a string`},
		{"url wrong type", "[[servers]]\nname = \"s\"\nscope = \"user\"\nurl = 3\n", `"url" must be a string`},
		{"args wrong type", "[[servers]]\nname = \"s\"\nscope = \"user\"\nargs = 3\n", `"args" must be a string array`},
		{"env_vars wrong type", "[[servers]]\nname = \"s\"\nscope = \"user\"\nenv_vars = [1]\n", `"env_vars" must be a string array`},
		{"env wrong type", "[[servers]]\nname = \"s\"\nscope = \"user\"\nenv = [1]\n", `"env" must be a string map`},
		{"env value not string", "[[servers]]\nname = \"s\"\nscope = \"user\"\n[servers.env]\nN = 1\n", `"env" must be a string map`},
		{"headers wrong type", "[[servers]]\nname = \"s\"\nscope = \"user\"\nheaders = [1]\n", `"headers" must be a string map`},
		{"bearer wrong type", "[[servers]]\nname = \"s\"\nscope = \"user\"\nbearer_token_env_var = 1\n", `"bearer_token_env_var" must be a string`},
		{"timeout wrong type", "[[servers]]\nname = \"s\"\nscope = \"user\"\nstartup_timeout_sec = \"x\"\n", `"startup_timeout_sec" must be an int`},
		{"entry not table", "servers = [\"x\"]\n", `each server entry must be a table`},
		{"servers missing", "[other]\nx = 1\n", `must contain a "servers" array`},
		{"servers wrong type", "servers = \"x\"\n", `must contain a "servers" array`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseServersBody(t, tc.body, nil, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
			if !strings.HasPrefix(err.Error(), Prog+": ") {
				t.Fatalf("error must be prefixed: %q", err.Error())
			}
		})
	}
}

func TestParseServersMissingVarBecomesEmpty(t *testing.T) {
	rows, err := parseServersBody(t, `
[[servers]]
name = "s"
scope = "user"
args = ["${NOPE}"]
url = "x"
`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Args[0] != "" {
		t.Fatalf("missing var must expand to empty, got %q", rows[0].Args[0])
	}
}

// Manifest profiles are NOT kebab-validated (matching the zsh parser); only
// the CLI --profile filter enforces kebab-case.
func TestParseServersProfilesNotKebabValidated(t *testing.T) {
	rows, err := parseServersBody(t, `
[[servers]]
name = "s"
scope = "user"
profiles = ["Base_ML"]
`, nil, nil)
	if err != nil {
		t.Fatalf("manifest profiles must not be kebab-validated: %v", err)
	}
	if rows[0].Profiles[0] != "Base_ML" {
		t.Fatalf("profile = %v", rows[0].Profiles)
	}
}

func TestParseServersMissingFile(t *testing.T) {
	_, err := ParseServers(filepath.Join(t.TempDir(), "nope.toml"), resolverFor(nil, nil))
	if err == nil || !strings.Contains(err.Error(), "manifest not found") {
		t.Fatalf("want manifest not found, got %v", err)
	}
}

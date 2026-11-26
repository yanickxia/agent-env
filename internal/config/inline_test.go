package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInlineEntriesStandaloneAndDefaults(t *testing.T) {
	path := writeRepo(t, `
agents = ["codex", "claude"]
mode = "copy"
[[installs]]
source = "./skills"
skills = ["local"]
[[servers]]
name = "local"
command = "./bin/mcp"
args = ["${TOKEN}"]
env = { Z = "${TOKEN}", A = "a" }
headers = { "X-Token" = "${TOKEN}" }
`)
	cfg, found, err := LoadRepoConfig(path)
	if err != nil || !found || !cfg.HasInlineEntries() {
		t.Fatalf("cfg=%+v found=%v err=%v", cfg, found, err)
	}
	if len(cfg.Profiles) != 0 || len(cfg.Names) != 0 || cfg.Servers[0].Args[0] != "${TOKEN}" {
		t.Fatalf("inline entries must not require selectors or resolve secrets: %+v", cfg)
	}
	missing := filepath.Join(t.TempDir(), "absent.toml")
	installs, err := LoadProjectInstalls(missing, cfg)
	if err != nil || len(installs) != 1 {
		t.Fatalf("installs=%+v err=%v", installs, err)
	}
	i := installs[0]
	if i.Global || i.RepoPath != path || i.Source != filepath.Join(filepath.Dir(path), "skills") || i.Mode != "copy" || i.AgentsRaw != "codex,claude" {
		t.Fatalf("install defaults/path/scope: %+v", i)
	}
	servers, configured, err := LoadProjectServers(missing, cfg, func(string) string { return "secret" })
	if err != nil || configured || len(servers) != 1 {
		t.Fatalf("servers=%+v configured=%v err=%v", servers, configured, err)
	}
	s := servers[0]
	if s.Global || s.RepoPath != path || s.Command != filepath.Join(filepath.Dir(path), "bin/mcp") || !reflect.DeepEqual(s.Agents, []string{"codex", "claude-code"}) {
		t.Fatalf("server defaults/path/scope: %+v", s)
	}
	if s.Args[0] != "secret" || s.Env[0] != (KV{"Z", "secret"}) || s.Env[1].Key != "A" || s.Headers[0].Value != "secret" {
		t.Fatalf("placeholder/order handling: %+v", s)
	}
	if cfg.Servers[0].Args[0] != "${TOKEN}" || cfg.Servers[0].Env[0].Value != "${TOKEN}" || cfg.Installs[0].Mode != "" {
		t.Fatal("loading effective entries mutated repo config")
	}
}

func TestInlineEntriesValidation(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"installs type", `installs = "x"`, `"installs" array`},
		{"servers type", `servers = "x"`, `"servers" array`},
		{"install not table", `installs = [1]`, "must be a table"},
		{"server not table", `servers = [1]`, "must be a table"},
		{"source required", "[[installs]]\nskills = [\"local\"]", `non-empty "source"`},
		{"name required", "[[servers]]\ncommand = \"test\"", `non-empty "name"`},
		{"skills type", "[[installs]]\nsource = \"x/y\"\nskills = [1]", `"skills" must be`},
		{"env type", "[[servers]]\nname = \"local\"\nenv = { X = 1 }", `"env" must be`},
		{"install global", "[[installs]]\nsource = \"x/y\"\nprofiles = [\"base\", \"global\"]", "project-only"},
		{"server global", "[[servers]]\nname = \"local\"\nprofiles = [\"global\"]", "project-only"},
		{"server profile", "[[servers]]\nname = \"local\"\nprofiles = [\"Bad_Profile\"]", "kebab-case"},
		{"post_install unknown key", "[[installs]]\nsource = \"x/y\"\npost_install = [{ run = \"x\", extra = \"y\" }]", "unknown post_install key"},
		{"server hooks", "[[servers]]\nname = \"local\"\ncommand = \"mcp\"\npost_install = [\"echo nope\"]", "post_install"},
		{"scope", "[[servers]]\nname = \"local\"\nscope = \"user\"", "scope"},
		{"unknown", "[[servers]]\nname = \"local\"\ncommmand = \"typo\"", "commmand"},
		{"mode", "[[installs]]\nsource = \"x/y\"\nmode = \"bad\"", "mode must be"},
		{"duplicate installs", "[[installs]]\nsource = \"x/y\"\nname = \"same\"\n[[installs]]\nsource = \"x/z\"\nname = \"same\"", "duplicate install name"},
		{"duplicate servers", "[[servers]]\nname = \"same\"\n[[servers]]\nname = \"same\"", "duplicate server name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeRepo(t, tc.body)
			_, _, err := LoadRepoConfig(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), path) {
				t.Fatalf("want %q and path, got %v", tc.want, err)
			}
		})
	}
}

func TestInlinePostInstallAccepted(t *testing.T) {
	path := writeRepo(t, `
[[installs]]
source = "./local"
post_install = ["npm install -g x", { run = "npm install -g y", if_missing = "y" }]
`)
	cfg, _, err := LoadRepoConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	installs, err := LoadProjectInstalls(filepath.Join(t.TempDir(), "absent.toml"), cfg)
	if err != nil || len(installs) != 1 || installs[0].RepoPath != path {
		t.Fatalf("installs=%+v err=%v", installs, err)
	}
	hooks := installs[0].PostInstall
	if len(hooks) != 2 || hooks[0] != (PostInstall{Run: "npm install -g x"}) || hooks[1] != (PostInstall{Run: "npm install -g y", IfMissing: "y"}) {
		t.Fatalf("inline post_install must parse with the manifest syntax: %+v", hooks)
	}
	// Hooks are not part of the stamp signature: changing them must not
	// invalidate an unchanged inline install.
	if _, _, err := LoadRepoConfig(writeRepo(t, "[[installs]]\nsource = \"./local\"\npost_install = \"x\"")); err == nil || !strings.Contains(err.Error(), `"post_install" must be an array`) {
		t.Fatalf("shared parser must still validate inline hooks: %v", err)
	}
}

func TestInlineLayerPrecedenceAndIsolation(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	child := filepath.Join(parent, "child")
	parentPath := filepath.Join(parent, ".agent-env.toml")
	childPath := filepath.Join(child, ".agent-env.toml")
	writeLayer(t, parentPath, `
agents = ["opencode"]
[[installs]]
name = "shared"
source = "example/parent"
[[installs]]
source = "./parent-skills"
[[servers]]
name = "shared"
command = "parent"
[[servers]]
name = "inherited"
command = "./bin/inherited"
`)
	writeLayer(t, childPath, `
agents = ["codex"]
[[installs]]
name = "shared"
source = "example/child"
[[servers]]
name = "shared"
command = "child"
`)
	cfg, _, found, err := LoadRepoSelection("", child, noEnv, "hint")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if len(cfg.Installs) != 2 || cfg.Installs[0].Source != "example/child" || cfg.Installs[1].Source != filepath.Join(parent, "parent-skills") {
		t.Fatalf("install layers: %+v", cfg.Installs)
	}
	if len(cfg.Servers) != 2 || cfg.Servers[0].Command != "child" || cfg.Servers[1].Command != filepath.Join(parent, "bin/inherited") {
		t.Fatalf("server layers: %+v", cfg.Servers)
	}
	manifest := filepath.Join(root, "config.toml")
	writeLayer(t, manifest, `
[[installs]]
name = "shared"
source = "example/manifest"
[[installs]]
name = "global"
source = "example/global"
profiles = ["global"]
[[servers]]
name = "shared"
command = "manifest"
[[servers]]
name = "shared"
command = "global"
profiles = ["global"]
`)
	is, err := LoadProjectInstalls(manifest, cfg)
	if err != nil || len(is) != 3 || is[0].Source != "example/child" || !is[2].Global || is[1].AgentsRaw != "codex" {
		t.Fatalf("merged installs=%+v err=%v", is, err)
	}
	ss, configured, err := LoadProjectServers(manifest, cfg, nil)
	if err != nil || !configured || len(ss) != 3 || ss[0].Command != "child" || !ss[2].Global {
		t.Fatalf("merged servers=%+v configured=%v err=%v", ss, configured, err)
	}
	// An explicit file excludes inherited entries, and siblings never see
	// the child's inline definitions.
	explicit, _, _, err := LoadRepoSelection(childPath, child, noEnv, "hint")
	if err != nil || len(explicit.Servers) != 1 {
		t.Fatalf("single-file selection: %+v %v", explicit, err)
	}
	sibling, _, _, err := LoadRepoSelection("", filepath.Join(parent, "sibling"), noEnv, "hint")
	if err != nil || sibling.Servers[0].Command != "parent" {
		t.Fatalf("sibling selection: %+v %v", sibling, err)
	}
}

func TestInlineOptionalGlobalManifestStillValidates(t *testing.T) {
	repo := &RepoConfig{Installs: []Install{{Source: "example/local"}}}
	for _, body := range []string{"# no domain tables", "installs = []\nservers = []\n"} {
		path := filepath.Join(t.TempDir(), "config.toml")
		writeLayer(t, path, body)
		if _, err := LoadProjectInstalls(path, repo); err != nil {
			t.Fatal(err)
		}
		if _, _, err := LoadProjectServers(path, repo, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{"invalid = [", "installs = 1\nservers = 1"} {
		path := filepath.Join(t.TempDir(), "config.toml")
		writeLayer(t, path, body)
		if _, err := LoadProjectInstalls(path, repo); err == nil {
			t.Fatal("malformed manifest must not be ignored")
		}
		if _, _, err := LoadProjectServers(path, repo, nil); err == nil {
			t.Fatal("malformed manifest must not be ignored")
		}
	}
	// A path that exists but cannot be read as a file is not optional.
	dir := t.TempDir()
	if _, err := LoadProjectInstalls(dir, repo); err == nil {
		t.Fatal("directory manifest must fail")
	}
	if _, _, err := LoadProjectServers(dir, repo, nil); err == nil {
		t.Fatal("directory manifest must fail")
	}
}

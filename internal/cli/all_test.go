package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yanickxia/agent-env/internal/mcp"
	"github.com/yanickxia/agent-env/internal/skills"
)

type allFixture struct {
	dir   string
	home  string
	log   string
	out   bytes.Buffer
	errb  bytes.Buffer
	sOpts skills.Options
	mOpts mcp.Options
}

func newAllFixture(t *testing.T) *allFixture {
	t.Helper()
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	stub := filepath.Join(dir, "stub")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stub, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "npx.log")
	t.Setenv("PATH", stub+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NPX_LOG", log)
	writeStub(t, filepath.Join(stub, "npx"), "#!/bin/sh\nprintf '%s\\t%s\\n' \"$PWD\" \"$*\" >> \"$NPX_LOG\"\nexit 0\n")
	writeStub(t, filepath.Join(stub, "node"), "#!/bin/sh\nexit 0\n")

	f := &allFixture{
		dir:  dir,
		home: home,
		log:  log,
	}
	f.sOpts = skills.Options{
		ManifestPath:   filepath.Join(dir, "config.toml"),
		StatePath:      filepath.Join(dir, "state.tsv"),
		RepoConfigPath: filepath.Join(dir, ".agent-env.toml"),
		ProjectRoot:    dir,
		Home:           home,
		Stdout:         &f.out,
		Stderr:         &f.errb,
	}
	f.mOpts = mcp.Options{
		ManifestPath:   filepath.Join(dir, "config.toml"),
		SecretsPath:    filepath.Join(dir, "secrets.toml"),
		RepoConfigPath: filepath.Join(dir, ".agent-env.toml"),
		ProjectRoot:    dir,
		Home:           home,
		WorkDir:        dir,
		ClaudeJSON:     filepath.Join(home, ".claude.json"),
		Stdout:         &f.out,
		Stderr:         &f.errb,
		LookupEnv:      func(string) string { return "" },
	}
	return f
}

func writeStub(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func (f *allFixture) writeManifest(t *testing.T, body string) {
	t.Helper()
	if err := os.WriteFile(f.sOpts.ManifestPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *allFixture) resetBuffers() {
	f.out.Reset()
	f.errb.Reset()
}

func (f *allFixture) npxCalls(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(f.log)
	if err != nil {
		return 0
	}
	s := strings.TrimRight(string(data), "\n")
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func TestAllInOneStandaloneInlineConfig(t *testing.T) {
	for _, domain := range []string{"skills", "mcp", "both"} {
		t.Run(domain, func(t *testing.T) {
			f := newAllFixture(t)
			body := "agents = [\"codex\"]\n"
			if domain != "mcp" {
				body += "[[installs]]\nsource = \"example/local\"\nskills = [\"local-skill\"]\n"
			}
			if domain != "skills" {
				body += "[[servers]]\nname = \"local-mcp\"\ncommand = \"local-command\"\n"
			}
			if err := os.WriteFile(f.sOpts.RepoConfigPath, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			sErr, mErr := runAll(sharedFilters{
				command: skills.CmdApply, nonInteractive: true,
				agents: []string{"codex"}, agentSeen: true,
			}, f.sOpts, f.mOpts)
			if sErr != nil || mErr != nil {
				t.Fatalf("standalone inline config: skills=%v mcp=%v", sErr, mErr)
			}
			wantCalls := 1
			if domain == "mcp" {
				wantCalls = 0
			}
			if got := f.npxCalls(t); got != wantCalls {
				t.Fatalf("npx calls = %d, want %d", got, wantCalls)
			}
			if domain != "skills" {
				data, err := os.ReadFile(filepath.Join(f.dir, ".codex", "config.toml"))
				if err != nil || !strings.Contains(string(data), "local-mcp") {
					t.Fatalf("missing project MCP config: %s %v", data, err)
				}
			}
			if _, err := os.Stat(filepath.Join(f.home, ".codex", "config.toml")); !os.IsNotExist(err) {
				t.Fatalf("inline-only sync must not create a user config: %v", err)
			}
		})
	}
}

func TestAllInOneBothDomainsExecute(t *testing.T) {
	f := newAllFixture(t)
	f.writeManifest(t, `
[[installs]]
source = "example/skill"
agents = ["codex"]
skills = ["lane"]
profiles = ["global"]

[[servers]]
name = "srv"
agents = ["codex"]
type = "stdio"
command = "npx"
args = ["-y", "x"]
profiles = ["global"]
`)

	sf := sharedFilters{command: skills.CmdApply, nonInteractive: true, skipUnchanged: true}
	sErr, mErr := runAll(sf, f.sOpts, f.mOpts)
	if sErr != nil || mErr != nil {
		t.Fatalf("runAll errors: skills=%v mcp=%v", sErr, mErr)
	}
	out := f.out.String()
	if !strings.Contains(out, "=== skills ===") || !strings.Contains(out, "=== mcp ===") {
		t.Fatalf("missing domain separators:\n%s", out)
	}
	if got := f.npxCalls(t); got != 1 {
		t.Fatalf("npx calls after first apply = %d, want 1", got)
	}
	codex := filepath.Join(f.home, ".codex", "config.toml")
	content, err := os.ReadFile(codex)
	if err != nil {
		t.Fatalf("mcp did not write codex user target: %v", err)
	}
	if !strings.Contains(string(content), "[mcp_servers.srv]") {
		t.Fatalf("mcp target missing server:\n%s", content)
	}

	// Second run with --skip-unchanged: skills skips, MCP rewrites, npx not re-called.
	f.resetBuffers()
	sErr, mErr = runAll(sf, f.sOpts, f.mOpts)
	if sErr != nil || mErr != nil {
		t.Fatalf("second runAll errors: skills=%v mcp=%v", sErr, mErr)
	}
	if !strings.Contains(f.out.String(), "skip (unchanged)") {
		t.Fatalf("skills did not skip on second run:\n%s", f.out.String())
	}
	if got := f.npxCalls(t); got != 1 {
		t.Fatalf("npx calls after second apply = %d, want 1", got)
	}
}

func TestAllInOneAggregatesFailureAndStillRunsMCP(t *testing.T) {
	f := newAllFixture(t)
	f.writeManifest(t, `
[[installs]]
source = "example/bad"
agents = ["codex"]
skills = ["lane"]
profiles = ["global"]
post_install = ["false"]

[[servers]]
name = "srv"
agents = ["codex"]
type = "stdio"
command = "npx"
args = ["-y", "x"]
profiles = ["global"]
`)

	sf := sharedFilters{command: skills.CmdApply, nonInteractive: true}
	sErr, mErr := runAll(sf, f.sOpts, f.mOpts)
	if sErr == nil || !strings.Contains(sErr.Error(), "post_install hook(s) failed") {
		t.Fatalf("expected skills failure, got %v", sErr)
	}
	if mErr != nil {
		t.Fatalf("mcp should still succeed, got %v", mErr)
	}
	// MCP side effect must be present despite the skills failure.
	codex := filepath.Join(f.home, ".codex", "config.toml")
	if _, err := os.Stat(codex); err != nil {
		t.Fatalf("mcp did not run after skills failure: %v", err)
	}
	if !strings.Contains(f.out.String(), "=== skills ===") || !strings.Contains(f.out.String(), "=== mcp ===") {
		t.Fatalf("both domains must have started:\n%s", f.out.String())
	}
}

func TestAllInOneProfilePassedToBothDomains(t *testing.T) {
	f := newAllFixture(t)
	f.writeManifest(t, `
[[installs]]
source = "example/skills-global"
agents = ["codex"]
skills = ["a"]
profiles = ["global"]

[[installs]]
source = "example/skills-base"
agents = ["codex"]
skills = ["b"]
profiles = ["base"]

[[servers]]
name = "mcp-global"
agents = ["codex"]
type = "stdio"
command = "npx"
profiles = ["global"]

[[servers]]
name = "mcp-base"
agents = ["codex"]
type = "stdio"
command = "npx"
profiles = ["base"]
`)

	sf := sharedFilters{command: skills.CmdDryRun, profiles: []string{"base"}, profileSeen: true, nonInteractive: true}
	sErr, mErr := runAll(sf, f.sOpts, f.mOpts)
	if sErr != nil || mErr != nil {
		t.Fatalf("runAll errors: skills=%v mcp=%v", sErr, mErr)
	}
	out := f.out.String()
	if !strings.Contains(out, "example/skills-global") || !strings.Contains(out, "example/skills-base") {
		t.Fatalf("skills domain did not honor --profile base:\n%s", out)
	}
	if !strings.Contains(out, "mcp-global") || !strings.Contains(out, "mcp-base") {
		t.Fatalf("mcp domain did not honor --profile base:\n%s", out)
	}
}

func TestAllInOneRejectsDomainSpecificFlags(t *testing.T) {
	for _, flag := range []string{"--skill", "--name"} {
		cmd := newAllCmd("apply", "test")
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		cmd.SetArgs([]string{flag, "x"})
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "unknown flag: "+flag) {
			t.Fatalf("%s: want unknown flag error, got %v", flag, err)
		}
	}
}

func TestAllInOneApplyRequiresNonInteractive(t *testing.T) {
	cmd := newAllCmd("apply", "test")
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--profile", "base"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "requires --non-interactive") {
		t.Fatalf("want non-interactive requirement, got %v", err)
	}
}

func TestAllInOneZeroActiveSucceeds(t *testing.T) {
	f := newAllFixture(t)
	f.writeManifest(t, `
[[installs]]
source = "example/s"
agents = ["codex"]
skills = ["a"]
profiles = ["base"]

[[servers]]
name = "s"
agents = ["codex"]
type = "stdio"
command = "npx"
profiles = ["base"]
`)
	sf := sharedFilters{command: skills.CmdApply, nonInteractive: true}
	sErr, mErr := runAll(sf, f.sOpts, f.mOpts)
	if sErr != nil || mErr != nil {
		t.Fatalf("zero-active all-in-one must succeed: skills=%v mcp=%v", sErr, mErr)
	}
	out := f.out.String()
	if !strings.Contains(out, "=== skills ===") || !strings.Contains(out, "=== mcp ===") {
		t.Fatalf("both domains must run:\n%s", out)
	}
	if !strings.Contains(f.errb.String(), "no active entries") {
		t.Fatalf("want informational notice, got %q", f.errb.String())
	}
}

func TestAllInOneCrossDomainNameSelection(t *testing.T) {
	f := newAllFixture(t)
	if err := os.WriteFile(f.sOpts.RepoConfigPath, []byte("names = [\"clickup\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.writeManifest(t, `
[[installs]]
source = "example/clickup-skill"
name = "clickup"
agents = ["codex"]
skills = ["cup"]

[[servers]]
name = "clickup"
agents = ["codex"]
type = "stdio"
command = "npx"
`)
	sf := sharedFilters{command: skills.CmdDryRun, nonInteractive: true}
	sErr, mErr := runAll(sf, f.sOpts, f.mOpts)
	if sErr != nil || mErr != nil {
		t.Fatalf("runAll errors: skills=%v mcp=%v", sErr, mErr)
	}
	out := f.out.String()
	if !strings.Contains(out, "example/clickup-skill") {
		t.Fatalf("install named clickup must be selected:\n%s", out)
	}
	if !strings.Contains(out, "mcp_servers.clickup") {
		t.Fatalf("server named clickup must be selected:\n%s", out)
	}
}

func TestAllInOneNoRepoPassedToBothDomains(t *testing.T) {
	f := newAllFixture(t)
	// A repo config that would otherwise select the base entries.
	if err := os.WriteFile(f.sOpts.RepoConfigPath, []byte("profiles = [\"base\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.writeManifest(t, `
[[installs]]
source = "example/skills-global"
agents = ["codex"]
skills = ["a"]
profiles = ["global"]

[[installs]]
source = "example/skills-base"
agents = ["codex"]
skills = ["b"]
profiles = ["base"]

[[servers]]
name = "mcp-global"
agents = ["codex"]
type = "stdio"
command = "npx"
profiles = ["global"]

[[servers]]
name = "mcp-base"
agents = ["codex"]
type = "stdio"
command = "npx"
profiles = ["base"]
`)
	sf := sharedFilters{command: skills.CmdDryRun, nonInteractive: true, noRepo: true}
	sErr, mErr := runAll(sf, f.sOpts, f.mOpts)
	if sErr != nil || mErr != nil {
		t.Fatalf("runAll errors: skills=%v mcp=%v", sErr, mErr)
	}
	out := f.out.String()
	if !strings.Contains(out, "example/skills-global") || !strings.Contains(out, "mcp-global") {
		t.Fatalf("global entries must install under --no-repo:\n%s", out)
	}
	if strings.Contains(out, "example/skills-base") || strings.Contains(out, "mcp-base") {
		t.Fatalf("repo-level entries must be excluded under --no-repo:\n%s", out)
	}
	if strings.Contains(f.errb.String(), "no .agent-env.toml") {
		t.Fatalf("--no-repo must not warn about repo-level skips: %q", f.errb.String())
	}
}

func TestAllInOneRejectsNoRepoWithProfile(t *testing.T) {
	cmd := newAllCmd("dry-run", "test")
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--no-repo", "--profile", "base"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "cannot be combined with --profile") {
		t.Fatalf("want no-repo/profile combination error, got %v", err)
	}
}

func TestAllInOneHasForceFlag(t *testing.T) {
	cmd := newAllCmd("apply", "test")
	if cmd.Flags().Lookup("force") == nil {
		t.Fatal("all-in-one apply must expose --force")
	}
}

func TestAllowHooksFlagIsSharedByAllInOneAndSkills(t *testing.T) {
	all := newAllCmd("apply", "test")
	flag := all.Flags().Lookup("allow-hooks")
	if flag == nil || flag.Value.Type() != "bool" || flag.DefValue != "false" {
		t.Fatalf("all-in-one apply must expose a default-off --allow-hooks bool, got %+v", flag)
	}
	if !(&allFlags{allowHK: true}).toShared(all, skills.CmdApply).allowHooks || (&allFlags{}).toShared(all, skills.CmdApply).allowHooks {
		t.Fatal("--allow-hooks must reach the skills domain from the all-in-one command")
	}

	var sub *cobra.Command
	for _, c := range newSkillsCmd().Commands() {
		if c.Name() == "apply" || c.Name() == "dry-run" {
			if flag := c.Flags().Lookup("allow-hooks"); flag == nil || flag.DefValue != "false" {
				t.Fatalf("skills %s must expose a default-off --allow-hooks: %+v", c.Name(), flag)
			}
			sub = c
		}
	}
	if !(&filterFlags{allowHK: true}).toFilters(sub, skills.CmdDryRun).AllowHooks || (&filterFlags{}).toFilters(sub, skills.CmdDryRun).AllowHooks {
		t.Fatal("--allow-hooks must reach skills.Filters from the skills subcommand")
	}
}

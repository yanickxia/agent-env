package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestAllInOneBothDomainsExecute(t *testing.T) {
	f := newAllFixture(t)
	f.writeManifest(t, `
[[installs]]
source = "example/skill"
agents = ["codex"]
skills = ["lane"]
scope = "user"

[[servers]]
name = "srv"
agents = ["codex"]
type = "stdio"
command = "npx"
args = ["-y", "x"]
scope = ["user"]
`)

	sf := sharedFilters{command: skills.CmdApply, scopes: []string{"user"}, scopeSeen: true, nonInteractive: true, skipUnchanged: true}
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
scope = "user"
post_install = ["false"]

[[servers]]
name = "srv"
agents = ["codex"]
type = "stdio"
command = "npx"
args = ["-y", "x"]
scope = ["user"]
`)

	sf := sharedFilters{command: skills.CmdApply, scopes: []string{"user"}, scopeSeen: true, nonInteractive: true}
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

func TestAllInOneScopePassedToBothDomains(t *testing.T) {
	f := newAllFixture(t)
	f.writeManifest(t, `
[[installs]]
source = "example/skills-user"
agents = ["codex"]
skills = ["a"]
scope = "user"

[[installs]]
source = "example/skills-project"
agents = ["codex"]
skills = ["b"]
scope = "project"

[[servers]]
name = "mcp-user"
agents = ["codex"]
type = "stdio"
command = "npx"
scope = ["user"]

[[servers]]
name = "mcp-project"
agents = ["codex"]
type = "stdio"
command = "npx"
scope = ["project"]
profiles = ["base"]
`)

	sf := sharedFilters{command: skills.CmdDryRun, scopes: []string{"user"}, scopeSeen: true, nonInteractive: true}
	sErr, mErr := runAll(sf, f.sOpts, f.mOpts)
	if sErr != nil || mErr != nil {
		t.Fatalf("runAll errors: skills=%v mcp=%v", sErr, mErr)
	}
	out := f.out.String()
	if !strings.Contains(out, "example/skills-user") || strings.Contains(out, "example/skills-project") {
		t.Fatalf("skills domain did not honor --scope user:\n%s", out)
	}
	if !strings.Contains(out, "mcp-user") || strings.Contains(out, "mcp-project") {
		t.Fatalf("mcp domain did not honor --scope user:\n%s", out)
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
	cmd.SetArgs([]string{"--scope", "user"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "requires --non-interactive") {
		t.Fatalf("want non-interactive requirement, got %v", err)
	}
}

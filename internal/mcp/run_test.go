package mcp

import (
	"path/filepath"
	"strings"
	"testing"
)

const gatingManifest = `
[[servers]]
name = "srv-tagged"
agents = ["codex", "opencode"]
type = "stdio"
command = "npx"
args = ["-y", "fake@latest"]
scope = ["project"]
profiles = ["base", "ark-mlops"]

[[servers]]
name = "srv-untagged"
agents = ["codex", "opencode"]
type = "stdio"
command = "npx"
args = ["-y", "fake2@latest"]
scope = ["project"]

[[servers]]
name = "srv-user"
agents = ["codex"]
type = "stdio"
command = "npx"
args = ["-y", "fake3@latest"]
scope = ["user"]
profiles = ["frontend"]
`

func TestGatingNoSelectionKeepsAllServers(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(gatingManifest)
	out, errb, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, out, "[mcp_servers.srv-tagged]")
	assertContains(t, out, "[mcp_servers.srv-untagged]")
	assertContains(t, out, "[mcp_servers.srv-user]")
	assertNotContains(t, errb, "without matching profiles")
}

func TestGatingRepoProfilesGateProjectOnly(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(gatingManifest)
	f.writeRepo(`profiles = ["base"]`)

	out, errb, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, out, "[mcp_servers.srv-tagged]")
	assertNotContains(t, out, "srv-untagged")
	assertContains(t, errb, "skipped 1 project servers without matching profiles")
	// User rows ignore the profile gate entirely.
	assertContains(t, out, "[mcp_servers.srv-user]")
}

func TestGatingCLIProfileOverridesRepo(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(gatingManifest + `
[[servers]]
name = "srv-base-only"
agents = ["codex"]
type = "stdio"
command = "npx"
scope = ["project"]
profiles = ["base"]
`)
	f.writeRepo(`profiles = ["base"]`)

	out, _, err := f.run(Filters{Command: CmdDryRun, Profiles: []string{"ark-mlops"}, ProfileSeen: true, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, out, "[mcp_servers.srv-tagged]")
	assertNotContains(t, out, "srv-base-only")
}

func TestGatingRepoAgentsNarrowProject(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(gatingManifest)
	f.writeRepo("profiles = [\"base\"]\nagents = [\"codex\"]\n")

	out, _, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, out, "[mcp_servers.srv-tagged]")
	assertNotContains(t, out, "would write OpenCode project MCP config")
	assertNotContains(t, out, `"srv-tagged": {`)
}

func TestGatingUnknownAgentRejected(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(gatingManifest)
	_, _, err := f.run(Filters{Command: CmdDryRun, Agents: []string{"nope"}, AgentSeen: true, NonInteractive: true})
	if err == nil || !strings.Contains(err.Error(), "unknown agent(s): nope") {
		t.Fatalf("want unknown agent error, got %v", err)
	}
}

func TestProfilesCommand(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(gatingManifest)
	out, _, err := f.run(Filters{Command: CmdProfiles})
	if err != nil {
		t.Fatalf("profiles failed: %v", err)
	}
	if out != "ark-mlops\nbase\nfrontend\n" {
		t.Fatalf("profiles output = %q", out)
	}

	f.writeManifest("[[servers]]\nname = \"a\"\nscope = [\"project\"]\n")
	_, _, err = f.run(Filters{Command: CmdProfiles})
	if err == nil || !strings.Contains(err.Error(), "no profiles declared") {
		t.Fatalf("want no-profiles error, got %v", err)
	}

	_, _, err = f.run(Filters{Command: CmdProfiles, Agents: []string{"codex"}, AgentSeen: true})
	if err == nil || !strings.Contains(err.Error(), "not profiles") {
		t.Fatalf("want filter rejection, got %v", err)
	}
}

func TestListVerbatimAndFiltersRejected(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	body := "# comment\n[[servers]]\nname = \"a\"\nscope = [\"user\"]\n"
	f.writeManifest(body)
	out, _, err := f.run(Filters{Command: CmdList})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if out != body {
		t.Fatalf("list not verbatim:\nwant %q\ngot %q", body, out)
	}
	_, _, err = f.run(Filters{Command: CmdList, Scopes: []string{"user"}, ScopeSeen: true})
	if err == nil || !strings.Contains(err.Error(), "not list") {
		t.Fatalf("want list filter rejection, got %v", err)
	}
}

func TestScopeValidation(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(gatingManifest)
	_, _, err := f.run(Filters{Command: CmdDryRun, Scopes: []string{"bogus"}, ScopeSeen: true})
	if err == nil || !strings.Contains(err.Error(), "unsupported scope filter 'bogus'") {
		t.Fatalf("want scope error, got %v", err)
	}
}

func TestApplyWithoutNonInteractive(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(gatingManifest)
	_, _, err := f.run(Filters{Command: CmdApply})
	if err == nil || !strings.Contains(err.Error(), "interactive selection is not supported") {
		t.Fatalf("want interactive error, got %v", err)
	}
}

// --- redaction ---------------------------------------------------------------

func TestRedactionSecretsVsEnv(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("token = \"sekret-value\"\n")
	f.env["ENVONLY"] = "env-value"
	f.writeManifest(`
[[servers]]
name = "sec"
agents = ["codex"]
type = "stdio"
command = "npx"
args = ["-y", "${TOKEN}"]
scope = ["user"]
[servers.env]
A = "${TOKEN}"
B = "${ENVONLY}"
`)

	out, _, err := f.run(Filters{Command: CmdDryRun, Scopes: []string{"user"}, ScopeSeen: true, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, out, "***redacted***")
	assertNotContains(t, out, "sekret-value")
	// Env-only fallback values are the user's own shell values: not redacted.
	assertContains(t, out, "env-value")
}

func TestRedactionCLICommands(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("token = \"proj-secret\"\n")
	f.writeManifest(`
[[servers]]
name = "cproj"
agents = ["claude"]
type = "stdio"
command = "npx"
args = ["-y", "x"]
scope = ["project"]
[servers.env]
P = "${TOKEN}"
`)
	out, _, err := f.run(Filters{Command: CmdDryRun, Scopes: []string{"project"}, ScopeSeen: true, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, out, "claude mcp add")
	assertContains(t, out, `P=\*\*\*redacted\*\*\*`)
	assertNotContains(t, out, "proj-secret")
}

// --- aiden CLI ---------------------------------------------------------------

func TestAidenCommandConstructionAndRun(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	logPath := filepath.Join(f.dir, "aiden.log")
	installLogStub(t, filepath.Join(f.dir, "stub"), "aiden", logPath)
	f.writeManifest(`
[[servers]]
name = "aid"
agents = ["aiden"]
type = "stdio"
command = "npx"
args = ["-y", "a@latest"]
scope = ["project"]
profiles = ["base"]
`)
	// dry-run: the command is printed and nothing runs.
	out, _, err := f.run(Filters{Command: CmdDryRun, Scopes: []string{"project"}, ScopeSeen: true, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, out, "aiden mcp add -s project aid -- npx -y a@latest")
	if readFile(t, logPath) != "" {
		t.Fatalf("dry-run must not invoke aiden")
	}

	// apply: the stub records the invocation (aiden failures are swallowed).
	if _, _, err := f.run(Filters{Command: CmdApply, Scopes: []string{"project"}, ScopeSeen: true, NonInteractive: true}); err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	log := readFile(t, logPath)
	assertContains(t, log, "mcp add -s project aid -- npx -y a@latest")
	assertContains(t, log, f.dir)
}

func TestAidenHTTPCommand(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("bt = \"tok\"\n")
	f.writeManifest(`
[[servers]]
name = "aidh"
agents = ["aiden"]
type = "streamable-http"
url = "https://x/mcp"
scope = ["user"]
bearer_token_env_var = "BT"
`)
	out, _, err := f.run(Filters{Command: CmdDryRun, Scopes: []string{"user"}, ScopeSeen: true, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, out, `aiden mcp add --transport http -s global aidh https://x/mcp -H Authorization:\ Bearer\ \*\*\*redacted\*\*\*`)
}

func TestUnsupportedAgentWarns(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(`
[[servers]]
name = "piserver"
agents = ["pi"]
type = "stdio"
command = "npx"
scope = ["user"]
`)
	_, errb, err := f.run(Filters{Command: CmdDryRun, Scopes: []string{"user"}, ScopeSeen: true, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, errb, "unsupported agent 'pi' for server 'piserver'; skipped")
}

func TestNoActiveEntriesError(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest("[[servers]]\nname = \"a\"\nscope = [\"project\"]\n")
	_, _, err := f.run(Filters{Command: CmdDryRun, Names: []string{"missing"}, NonInteractive: true})
	if err == nil || !strings.Contains(err.Error(), "no active entries found") {
		t.Fatalf("want no-active-entries error, got %v", err)
	}
}

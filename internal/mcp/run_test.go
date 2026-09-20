package mcp

import (
	"encoding/json"
	"os"
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
profiles = ["base", "ark-mlops"]

[[servers]]
name = "srv-frontend"
agents = ["codex", "opencode"]
type = "stdio"
command = "npx"
args = ["-y", "fake2@latest"]
profiles = ["frontend"]

[[servers]]
name = "srv-global"
agents = ["codex"]
type = "stdio"
command = "npx"
args = ["-y", "fake3@latest"]
profiles = ["global"]
`

func TestGatingNoSelectionInstallsGlobalSkipsRepoLevel(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(gatingManifest)
	out, errb, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, out, "[mcp_servers.srv-global]")
	assertNotContains(t, out, "srv-tagged")
	assertNotContains(t, out, "srv-frontend")
	assertContains(t, errb, "skipped 2 repo-level servers")
	assertContains(t, errb, "agent-env init or --profile")
}

func TestGatingRepoProfilesGateRepoLevelOnly(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(gatingManifest)
	f.writeRepo(`profiles = ["base"]`)

	out, _, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, out, "[mcp_servers.srv-tagged]")
	assertNotContains(t, out, "srv-frontend")
	// Global servers ignore the profile gate entirely.
	assertContains(t, out, "[mcp_servers.srv-global]")
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

func TestGatingRepoAgentsNarrowRepoLevel(t *testing.T) {
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

func TestGlobalProfileCannotBeSelected(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(gatingManifest)
	_, _, err := f.run(Filters{Command: CmdDryRun, Profiles: []string{"global"}, ProfileSeen: true, NonInteractive: true})
	if err == nil || !strings.Contains(err.Error(), `"global" is a reserved profile keyword`) {
		t.Fatalf("want reserved-global error, got %v", err)
	}
}

func TestProfilesCommandExcludesGlobal(t *testing.T) {
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

	f.writeManifest("[[servers]]\nname = \"a\"\nprofiles = [\"global\"]\n")
	_, _, err = f.run(Filters{Command: CmdProfiles})
	if err == nil || !strings.Contains(err.Error(), "no selectable profiles declared") {
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
	body := "# comment\n[[servers]]\nname = \"a\"\nprofiles = [\"global\"]\n"
	f.writeManifest(body)
	out, _, err := f.run(Filters{Command: CmdList})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if out != body {
		t.Fatalf("list not verbatim:\nwant %q\ngot %q", body, out)
	}
	_, _, err = f.run(Filters{Command: CmdList, Agents: []string{"codex"}, AgentSeen: true})
	if err == nil || !strings.Contains(err.Error(), "not list") {
		t.Fatalf("want list filter rejection, got %v", err)
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
profiles = ["global"]
[servers.env]
A = "${TOKEN}"
B = "${ENVONLY}"
`)

	out, _, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
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
	f.writeRepo(`profiles = ["base"]`)
	f.writeManifest(`
[[servers]]
name = "cproj"
agents = ["claude-code"]
type = "stdio"
command = "npx"
args = ["-y", "x"]
profiles = ["base"]
[servers.env]
P = "${TOKEN}"
`)
	out, _, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
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
	f.writeRepo(`profiles = ["base"]`)
	logPath := filepath.Join(f.dir, "aiden.log")
	installLogStub(t, filepath.Join(f.dir, "stub"), "aiden", logPath)
	f.writeManifest(`
[[servers]]
name = "aid"
agents = ["aiden"]
type = "stdio"
command = "npx"
args = ["-y", "a@latest"]
profiles = ["base"]
`)
	// dry-run: the command is printed and nothing runs.
	out, _, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, out, "aiden mcp add -s project aid -- npx -y a@latest")
	if readFile(t, logPath) != "" {
		t.Fatalf("dry-run must not invoke aiden")
	}

	// apply: the stub records the invocation (aiden failures are swallowed).
	if _, _, err := f.run(Filters{Command: CmdApply, NonInteractive: true}); err != nil {
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
profiles = ["global"]
bearer_token_env_var = "BT"
`)
	out, _, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, out, `aiden mcp add --transport http -s global aidh https://x/mcp -H Authorization:\ Bearer\ \*\*\*redacted\*\*\*`)
}

func TestPiUserApplyWritesMCPJSON(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(`
[[servers]]
name = "piserver"
agents = ["pi"]
type = "stdio"
command = "npx"
args = ["-y", "pi@latest"]
profiles = ["global"]
`)
	_, errb, err := f.run(Filters{Command: CmdApply, NonInteractive: true})
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	assertNotContains(t, errb, "unsupported agent 'pi'")

	target := filepath.Join(f.home, ".pi", "agent", "mcp.json")
	var after map[string]any
	if err := json.Unmarshal([]byte(readFile(t, target)), &after); err != nil {
		t.Fatalf("pi mcp.json is not valid JSON: %v\n%s", err, readFile(t, target))
	}
	mcp, _ := after["mcpServers"].(map[string]any)
	srv, _ := mcp["piserver"].(map[string]any)
	if srv == nil {
		t.Fatalf("piserver not written: %#v", after)
	}
	if srv["type"] != "stdio" || srv["command"] != "npx" {
		t.Fatalf("unexpected piserver entry: %#v", srv)
	}
}

func TestUnknownAgentStillWarns(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(`
[[servers]]
name = "unknownserver"
agents = ["bogusagent"]
type = "stdio"
command = "npx"
profiles = ["global"]
`)
	_, errb, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, errb, "unsupported agent 'bogusagent' for server 'unknownserver'; skipped")
}

func TestNoActiveEntriesIsSuccess(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest("[[servers]]\nname = \"a\"\nprofiles = [\"base\"]\n")
	out, errb, err := f.run(Filters{Command: CmdDryRun, Names: []string{"missing"}, NonInteractive: true})
	if err != nil {
		t.Fatalf("zero active entries must succeed, got %v", err)
	}
	if !strings.Contains(errb, "no active entries") {
		t.Fatalf("want informational notice, got %q", errb)
	}
	// dry-run still emits the empty-set would-write pipeline.
	assertContains(t, out, "would write Claude user MCP config")
	assertContains(t, out, "would write Codex user MCP config")
}

// --- claude alias canonicalization ------------------------------------------

func TestClaudeAliasTriggersClaudeWriter(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(`
[[servers]]
name = "c-alias"
agents = ["claude"]
type = "stdio"
command = "npx"
args = ["-y", "x"]
profiles = ["global"]
`)
	out, _, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, out, "would write Claude user MCP config")
}

func TestAgentFilterClaudeAlias(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeRepo(`profiles = ["base"]`)
	f.writeManifest(`
[[servers]]
name = "c1"
agents = ["claude-code"]
type = "stdio"
command = "npx"
args = ["-y", "x"]
profiles = ["base"]
`)
	outCC, _, err := f.run(Filters{Command: CmdDryRun, Agents: []string{"claude-code"}, AgentSeen: true, NonInteractive: true})
	if err != nil {
		t.Fatalf("claude-code filter failed: %v", err)
	}
	outC, _, err := f.run(Filters{Command: CmdDryRun, Agents: []string{"claude"}, AgentSeen: true, NonInteractive: true})
	if err != nil {
		t.Fatalf("claude alias filter failed: %v", err)
	}
	if outCC != outC {
		t.Fatalf("--agent claude and --agent claude-code must match:\n--- claude-code ---\n%s\n--- claude ---\n%s", outCC, outC)
	}
	assertContains(t, outCC, "claude mcp add c1")
}

// A zero-active run must still execute the writer pipeline so stale managed
// blocks and claude mcpServers are cleaned up.
func TestZeroActiveCleansUpWriters(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(`
[[servers]]
name = "archived-srv"
agents = ["codex", "claude-code"]
type = "stdio"
command = "npx"
profiles = ["archived"]
`)
	codex := filepath.Join(f.home, ".codex", "config.toml")
	f.write(codex, "model = \"x\"\n\n# AGENT_MCP_CODEX_MANAGED_BEGIN\n[mcp_servers.context7]\ncommand = \"npx\"\n\n# AGENT_MCP_CODEX_MANAGED_END\n")
	f.write(f.claude, `{"projects":{"p":{"a":1}},"mcpServers":{"context7":{"type":"stdio"}},"other":42}`)

	out, errb, err := f.run(Filters{Command: CmdApply, NonInteractive: true})
	if err != nil {
		t.Fatalf("zero-active apply must succeed, got %v", err)
	}
	if !strings.Contains(errb, "no active entries") {
		t.Fatalf("want informational notice, got %q", errb)
	}
	if !strings.Contains(out, "patched mcpServers") {
		t.Fatalf("claude patch must run even with zero entries:\n%s", out)
	}

	// claude mcpServers cleared, runtime keys preserved.
	var claude map[string]any
	if err := json.Unmarshal([]byte(readFile(t, f.claude)), &claude); err != nil {
		t.Fatal(err)
	}
	mcp, _ := claude["mcpServers"].(map[string]any)
	if len(mcp) != 0 {
		t.Fatalf("claude mcpServers must be empty, got %#v", mcp)
	}
	if claude["other"] != float64(42) {
		t.Fatalf("claude runtime keys must be preserved: %#v", claude)
	}

	// codex managed block removed entirely, outside bytes preserved.
	content := readFile(t, codex)
	assertNotContains(t, content, "# AGENT_MCP_CODEX_MANAGED_BEGIN")
	assertNotContains(t, content, "# AGENT_MCP_CODEX_MANAGED_END")
	assertNotContains(t, content, "context7")
	assertContains(t, content, `model = "x"`)

	// Missing trae/opencode targets are not created.
	if _, err := os.Stat(filepath.Join(f.home, ".trae", "traecli.yaml")); !os.IsNotExist(err) {
		t.Fatalf("trae target must not be created for an empty set")
	}
	if _, err := os.Stat(filepath.Join(f.home, ".config", "opencode", "opencode.jsonc")); !os.IsNotExist(err) {
		t.Fatalf("opencode target must not be created for an empty set")
	}

	// dry-run emits the empty-set would-write pipeline.
	out2, _, err := f.run(Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	assertContains(t, out2, "would write Codex user MCP config")
	assertContains(t, out2, "would write Claude user MCP config")
	assertNotContains(t, out2, "context7")
}

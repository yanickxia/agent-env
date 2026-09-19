package mcp

import (
	"strings"
	"testing"
)

const upsertManifest = `
[[servers]]
name = "u-codex"
agents = ["codex"]
type = "stdio"
command = "npx"
args = ["-y", "uc@latest"]
scope = ["user"]

[[servers]]
name = "u-opencode"
agents = ["opencode"]
type = "stdio"
command = "npx"
args = ["-y", "uo@latest"]
scope = ["user"]

[[servers]]
name = "u-trae"
agents = ["trae"]
type = "stdio"
command = "npx"
args = ["-y", "ut@latest"]
scope = ["user"]
`

func runUpsert(t *testing.T, f *fixture, agent, input string) string {
	t.Helper()
	opts := f.opts
	opts.Stdin = strings.NewReader(input)
	out, _, err := f.runWith(opts, Filters{Command: CmdUpsertStdin, Agents: []string{agent}, AgentSeen: true})
	if err != nil {
		t.Fatalf("upsert-stdin %s failed: %v", agent, err)
	}
	return out
}

func TestMarkerUpsertReplacesBlockKeepingOutside(t *testing.T) {
	base := "model = \"x\"\n\n[features]\nhooks = true\n\n" +
		codexBegin + "\n[mcp_servers.old]\ncommand = \"old\"\n\n" + codexEnd + "\n" +
		codexAnchor + "\nreserved stuff\n"
	block := codexBegin + "\n[mcp_servers.new]\ncommand = \"new\"\n\n" + codexEnd + "\n"

	got := markerUpsert(base, codexBegin, codexEnd, block, codexAnchor)
	assertContains(t, got, "[mcp_servers.new]")
	assertNotContains(t, got, "[mcp_servers.old]")

	// Bytes outside [BEGIN..END] (+one separator newline) must be unchanged.
	bPre := base[:strings.Index(base, codexBegin)]
	oPre := got[:strings.Index(got, codexBegin)]
	bPost := base[strings.Index(base, codexEnd)+len(codexEnd):]
	oPost := got[strings.Index(got, codexEnd)+len(codexEnd):]
	if strings.HasPrefix(bPost, "\n") {
		bPost = bPost[1:]
	}
	if strings.HasPrefix(oPost, "\n") {
		oPost = oPost[1:]
	}
	if bPre != oPre || bPost != oPost {
		t.Fatalf("outside bytes changed:\npre  %q vs %q\npost %q vs %q", bPre, oPre, bPost, oPost)
	}
}

func TestMarkerUpsertInsertsBeforeAnchor(t *testing.T) {
	base := "model = \"x\"\n" + codexAnchor + "\nreserved\n"
	block := codexBegin + "\n[mcp_servers.new]\ncommand = \"new\"\n\n" + codexEnd + "\n"
	got := markerUpsert(base, codexBegin, codexEnd, block, codexAnchor)
	if strings.Index(got, codexBegin) > strings.Index(got, codexAnchor) {
		t.Fatalf("block must be inserted before the anchor:\n%s", got)
	}
	assertContains(t, got, "model = \"x\"")
	assertContains(t, got, "reserved")
}

func TestMarkerUpsertIdempotent(t *testing.T) {
	block := codexBegin + "\n[mcp_servers.new]\ncommand = \"new\"\n\n" + codexEnd + "\n"
	base := "model = \"x\"\n\n[features]\nhooks = true\n"
	once := markerUpsert(base, codexBegin, codexEnd, block, codexAnchor)
	twice := markerUpsert(once, codexBegin, codexEnd, block, codexAnchor)
	if once != twice {
		t.Fatalf("marker upsert not idempotent:\n%s\n---\n%s", once, twice)
	}
}

func TestRemoveTopMCP(t *testing.T) {
	in := "{\n  \"model\": \"m\",\n  \"mcp\": {\n    \"x\": {}\n  },\n  \"plugin\": []\n}\n"
	got := removeTopMCP(in)
	assertNotContains(t, got, "\"mcp\"")
	assertContains(t, got, "\"model\": \"m\"")
	assertContains(t, got, "\"plugin\": []")
}

func TestUpsertStdinI1ReplacesBlockPreservesOutside(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(upsertManifest)
	base := "model = \"x\"\n\n[features]\nhooks = true\n\n" +
		codexBegin + "\n[mcp_servers.old]\ncommand = \"old\"\n\n" + codexEnd + "\n" +
		codexAnchor + "\nreserved stuff\n"
	out := runUpsert(t, f, "codex", base)
	assertContains(t, out, "[mcp_servers.u-codex]")
	assertNotContains(t, out, "[mcp_servers.old]")
	assertContains(t, out, codexAnchor)
	assertContains(t, out, "reserved stuff")
}

func TestUpsertStdinI2EqualsApply(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(upsertManifest)

	bases := map[string]string{
		"codex":    "model = \"x\"\n\n[features]\nhooks = true\n",
		"trae":     "model:\n  name: GPT\n",
		"opencode": "{\n  \"model\": \"m\",\n  \"plugin\": [\"x\"]\n}\n",
	}
	targets := map[string]string{
		"codex":    f.home + "/.codex/config.toml",
		"trae":     f.home + "/.trae/traecli.yaml",
		"opencode": f.home + "/.config/opencode/opencode.jsonc",
	}
	for agent, base := range bases {
		f.write(targets[agent], base)
	}

	// apply --scope user writes the transformed files.
	if _, _, err := f.run(Filters{Command: CmdApply, Scopes: []string{"user"}, ScopeSeen: true, NonInteractive: true}); err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	for agent, base := range bases {
		viaApply := readFile(t, targets[agent])
		viaUpsert := runUpsert(t, f, agent, base)
		if viaApply != viaUpsert {
			t.Fatalf("%s: apply != upsert-stdin\n--- apply ---\n%s\n--- upsert ---\n%s", agent, viaApply, viaUpsert)
		}
	}
}

func TestUpsertStdinI3Idempotent(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(upsertManifest)
	base := "model = \"x\"\n\n[features]\nhooks = true\n"
	once := runUpsert(t, f, "codex", base)
	twice := runUpsert(t, f, "codex", once)
	if once != twice {
		t.Fatalf("upsert-stdin not idempotent")
	}
}

func TestUpsertStdinI4NoMarkerAppend(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(upsertManifest)

	// trae: append after existing content; opencode: create from empty stdin.
	traeBase := "model:\n  name: GPT\n"
	traeOut := runUpsert(t, f, "trae", traeBase)
	assertContains(t, traeOut, "name: GPT")
	assertContains(t, traeOut, "mcp_servers:")
	assertContains(t, traeOut, "u-trae")

	opencodeOut := runUpsert(t, f, "opencode", "")
	if !strings.HasPrefix(opencodeOut, "{\n  // AGENT_MCP_OPENCODE_MANAGED_BEGIN\n") {
		t.Fatalf("opencode first creation malformed:\n%s", opencodeOut)
	}
	assertContains(t, opencodeOut, "\"u-opencode\": {")
}

func TestUpsertStdinRequiresSingleAgent(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(upsertManifest)
	_, _, err := f.run(Filters{Command: CmdUpsertStdin})
	if err == nil || !strings.Contains(err.Error(), "requires exactly one --agent") {
		t.Fatalf("want single-agent error, got %v", err)
	}
	_, _, err = f.run(Filters{Command: CmdUpsertStdin, Agents: []string{"codex", "trae"}, AgentSeen: true})
	if err == nil || !strings.Contains(err.Error(), "requires exactly one --agent") {
		t.Fatalf("want single-agent error, got %v", err)
	}
}

func TestUpsertStdinRejectsFilters(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(upsertManifest)
	_, _, err := f.run(Filters{Command: CmdUpsertStdin, Agents: []string{"codex"}, AgentSeen: true, Scopes: []string{"user"}, ScopeSeen: true})
	if err == nil || !strings.Contains(err.Error(), "does not accept filters") {
		t.Fatalf("want filter rejection, got %v", err)
	}
}

func TestUpsertStdinUnsupportedAgent(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(upsertManifest)
	_, _, err := f.run(Filters{Command: CmdUpsertStdin, Agents: []string{"claude"}, AgentSeen: true})
	if err == nil || !strings.Contains(err.Error(), "no user-level writer for agent 'claude'") {
		t.Fatalf("want unsupported agent error, got %v", err)
	}
}

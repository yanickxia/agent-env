package mcp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/yanickxia/agent-env/internal/config"
)

func TestRenderClaudeUser(t *testing.T) {
	entries := []config.Server{{
		Name: "u-claude", Type: "stdio", Command: "npx", Args: []string{"-y", "ucd@latest"},
		Env: []config.KV{{Key: "FOO", Value: "bar"}},
	}}
	got, err := renderClaudeUser(entries, func(k string) string { return "" }, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "u-claude": {
    "type": "stdio",
    "command": "npx",
    "args": [
      "-y",
      "ucd@latest"
    ],
    "env": {
      "FOO": "bar"
    }
  }
}`
	if got != want {
		t.Fatalf("renderClaudeUser mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestRenderClaudeUserHTTPBearer(t *testing.T) {
	entries := []config.Server{{
		Name: "h", Type: "streamable-http", URL: "https://x/mcp", BearerTokenEnvVar: "BT",
	}}
	got, err := renderClaudeUser(entries, func(k string) string { return "tok-" + k }, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, got, `"type": "http"`)
	assertContains(t, got, `"Authorization": "Bearer tok-BT"`)
}

func TestPatchClaudePreservesOtherKeys(t *testing.T) {
	base := `{
  "numStartups": 12,
  "projects": {
    "/p": {
      "history": [
        "a",
        "b"
      ],
      "trust": true
    }
  },
  "mcpServers": {
    "old": {
      "type": "stdio"
    }
  },
  "other": 42
}`
	rendered, err := renderClaudeUser([]config.Server{{
		Name: "u-claude", Type: "stdio", Command: "npx", Args: []string{"-y", "x"},
	}}, func(string) string { return "" }, func(string) {})
	if err != nil {
		t.Fatal(err)
	}

	out := string(patchClaudeJSON([]byte(base), rendered))

	// Value-level: every top-level key except mcpServers is unchanged.
	var before, after map[string]any
	if err := json.Unmarshal([]byte(base), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(out), &after); err != nil {
		t.Fatalf("patched output is not valid JSON: %v\n%s", err, out)
	}
	for k, v := range before {
		if k == "mcpServers" {
			continue
		}
		if !reflect.DeepEqual(v, after[k]) {
			t.Fatalf("top-level key %q changed: %#v -> %#v", k, v, after[k])
		}
	}
	mcp, ok := after["mcpServers"].(map[string]any)
	if !ok || len(mcp) != 1 || mcp["u-claude"] == nil {
		t.Fatalf("mcpServers not replaced: %#v", after["mcpServers"])
	}

	// Byte-level: prefix up to the mcpServers value and the suffix after it
	// are untouched.
	origStart, origEnd, _, _ := findTopLevelValue(base, "mcpServers")
	outStart, outEnd, _, _ := findTopLevelValue(out, "mcpServers")
	if base[:origStart] != out[:outStart] {
		t.Fatalf("prefix before mcpServers changed:\n%q\n%q", base[:origStart], out[:outStart])
	}
	origSuffix, outSuffix := base[origEnd:], out[outEnd:]
	if outSuffix != origSuffix && outSuffix != origSuffix+"\n" {
		t.Fatalf("suffix after mcpServers changed:\n%q\n%q", origSuffix, outSuffix)
	}
}

func TestPatchClaudeAddsMemberWhenAbsent(t *testing.T) {
	base := `{"projects":{"p":1},"other":42}`
	rendered := `{
  "s": {
    "type": "stdio",
    "command": "npx",
    "args": [],
    "env": {}
  }
}`
	out := string(patchClaudeJSON([]byte(base), rendered))
	if !json.Valid([]byte(out)) {
		t.Fatalf("patched output invalid JSON:\n%s", out)
	}
	var after map[string]any
	if err := json.Unmarshal([]byte(out), &after); err != nil {
		t.Fatal(err)
	}
	if after["other"] != float64(42) {
		t.Fatalf("unrelated key lost: %#v", after)
	}
	if _, ok := after["mcpServers"].(map[string]any); !ok {
		t.Fatalf("mcpServers not added: %s", out)
	}
	if !strings.Contains(out, `"projects"`) {
		t.Fatalf("projects lost: %s", out)
	}
}

func TestClaudeUserApplyPatch(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(`
[[servers]]
name = "u-claude"
agents = ["claude"]
type = "stdio"
command = "npx"
args = ["-y", "ucd@latest"]
profiles = ["global"]
[servers.env]
FOO = "bar"
`)
	f.write(f.claude, `{"projects":{"p":{"a":1}},"mcpServers":{"old":{"type":"stdio"}},"other":42}`)

	out, _, err := f.run(Filters{Command: CmdApply, NonInteractive: true})
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	assertContains(t, out, "patched mcpServers in")

	var after map[string]any
	if err := json.Unmarshal([]byte(readFile(t, f.claude)), &after); err != nil {
		t.Fatal(err)
	}
	projects, _ := after["projects"].(map[string]any)
	if projects == nil || projects["p"] == nil {
		t.Fatalf("projects lost: %#v", after["projects"])
	}
	if after["other"] != float64(42) {
		t.Fatalf("other lost: %#v", after["other"])
	}
	mcp, _ := after["mcpServers"].(map[string]any)
	if _, ok := mcp["u-claude"]; !ok {
		t.Fatalf("mcpServers not patched: %#v", mcp)
	}
	if _, ok := mcp["old"]; ok {
		t.Fatalf("old mcp server retained: %#v", mcp)
	}
}

func TestPatchClaudeMissingFileSkips(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(`
[[servers]]
name = "u-claude"
agents = ["claude"]
type = "stdio"
command = "npx"
profiles = ["global"]
`)
	_, errb, err := f.run(Filters{Command: CmdApply, NonInteractive: true})
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	assertContains(t, errb, "not found, skip")
}

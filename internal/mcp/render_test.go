package mcp

import (
	"testing"

	"github.com/yanickxia/agent-env/internal/config"
)

func TestRenderCodex(t *testing.T) {
	timeout := 20
	entries := []config.Server{{
		Name: "ctx", Type: "stdio", Command: "npx", Args: []string{"-y", "x"},
		EnvVars: []string{"A"}, Env: []config.KV{{Key: "K", Value: "V"}},
		StartupTimeoutSec: &timeout,
	}}
	want := `# AGENT_MCP_CODEX_MANAGED_BEGIN
[mcp_servers.ctx]
command = "npx"
args = ["-y", "x"]
env_vars = ["A"]
startup_timeout_sec = 20
[mcp_servers.ctx.env]
K = "V"

# AGENT_MCP_CODEX_MANAGED_END
`
	if got := renderCodex(entries); got != want {
		t.Fatalf("renderCodex mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestRenderCodexRemote(t *testing.T) {
	entries := []config.Server{{
		Name: "remote", Type: "streamable-http", URL: "https://x/mcp",
		BearerTokenEnvVar: "BT",
	}}
	want := `# AGENT_MCP_CODEX_MANAGED_BEGIN
[mcp_servers.remote]
url = "https://x/mcp"
bearer_token_env_var = "BT"

# AGENT_MCP_CODEX_MANAGED_END
`
	if got := renderCodex(entries); got != want {
		t.Fatalf("renderCodex remote mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestRenderTrae(t *testing.T) {
	entries := []config.Server{{
		Name: "ctx", Type: "stdio", Command: "npx", Args: []string{"-y", "x"},
		BearerTokenEnvVar: "BT", Env: []config.KV{{Key: "K", Value: "V"}},
	}}
	want := `# AGENT_MCP_MANAGED_BEGIN
mcp_servers:
  - name: 'ctx'
    type: 'stdio'
    command: 'npx'
    args:
      - '-y'
      - 'x'
    headers:
      Authorization: 'Bearer ${BT}'
    env:
      - key: 'K'
        value: 'V'
# AGENT_MCP_MANAGED_END
`
	if got := renderTrae(entries); got != want {
		t.Fatalf("renderTrae mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestRenderOpencodeStdio(t *testing.T) {
	entries := []config.Server{{
		Name: "ctx", Type: "stdio", Command: "npx", Args: []string{"-y", "x"},
		EnvVars: []string{"A"}, Env: []config.KV{{Key: "K", Value: "V"}},
	}}
	want := `  // AGENT_MCP_OPENCODE_MANAGED_BEGIN
  "mcp": {
    "ctx": {
      "type": "local",
      "command": ["npx", "-y", "x"],
      "enabled": true,
      "environment": {
        "A": "{env:A}",
        "K": "V"
      }
    }
  }
  // AGENT_MCP_OPENCODE_MANAGED_END
`
	if got := renderOpencode(entries); got != want {
		t.Fatalf("renderOpencode mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestRenderOpencodeRemote(t *testing.T) {
	timeout := 20
	entries := []config.Server{{
		Name: "r", Type: "streamable-http", URL: "http://x",
		BearerTokenEnvVar: "BT", StartupTimeoutSec: &timeout,
	}}
	want := `  // AGENT_MCP_OPENCODE_MANAGED_BEGIN
  "mcp": {
    "r": {
      "type": "remote",
      "url": "http://x",
      "enabled": true,
      "headers": {
        "Authorization": "Bearer {env:BT}"
      },
      "oauth": false,
      "timeout": 20000
    }
  }
  // AGENT_MCP_OPENCODE_MANAGED_END
`
	if got := renderOpencode(entries); got != want {
		t.Fatalf("renderOpencode remote mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestRenderOpencodeMultipleEntriesCommas(t *testing.T) {
	entries := []config.Server{
		{Name: "b", Type: "stdio", Command: "npx"},
		{Name: "a", Type: "stdio", Command: "npx"},
	}
	got := renderOpencode(entries)
	// Sorted by name: a then b, with a trailing comma between them.
	assertContains(t, got, "    \"a\": {\n")
	assertContains(t, got, "    },\n    \"b\": {\n")
}

func TestRenderersSortByName(t *testing.T) {
	entries := []config.Server{
		{Name: "zeta", Type: "stdio", Command: "z"},
		{Name: "alpha", Type: "stdio", Command: "a"},
	}
	for name, out := range map[string]string{
		"codex":    renderCodex(entries),
		"trae":     renderTrae(entries),
		"opencode": renderOpencode(entries),
	} {
		if idxA, idxZ := indexOf(out, "alpha"), indexOf(out, "zeta"); idxA == -1 || idxZ == -1 || idxA > idxZ {
			t.Fatalf("%s not sorted by name (a=%d z=%d):\n%s", name, idxA, idxZ, out)
		}
	}
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

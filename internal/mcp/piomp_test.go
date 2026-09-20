package mcp

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yanickxia/agent-env/internal/config"
)

func TestRenderPiOMPStdioAndHTTP(t *testing.T) {
	timeout := 20
	entries := []config.Server{
		{
			Name: "local", Type: "stdio", Command: "npx", Args: []string{"-y", "x"},
			EnvVars: []string{"IGNORED"}, Env: []config.KV{{Key: "K", Value: "V"}},
			StartupTimeoutSec: &timeout,
		},
		{
			Name: "remote", Type: "streamable-http", URL: "https://x/mcp",
			BearerTokenEnvVar: "BT",
		},
	}
	got, err := renderPiOMP(entries, func(k string) string { return "tok-" + k }, func(string) {}, "pi")
	if err != nil {
		t.Fatal(err)
	}

	want := `{
  "local": {
    "type": "stdio",
    "command": "npx",
    "args": [
      "-y",
      "x"
    ],
    "env": {
      "K": "V"
    }
  },
  "remote": {
    "type": "http",
    "url": "https://x/mcp",
    "headers": {
      "Authorization": "Bearer tok-BT"
    }
  }
}`
	if got != want {
		t.Fatalf("renderPiOMP mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
	// env_vars and startup_timeout_sec are intentionally ignored.
	assertNotContains(t, got, "IGNORED")
	assertNotContains(t, got, "20")
}

func TestRenderPiOMPPlainHeaders(t *testing.T) {
	entries := []config.Server{{
		Name: "h", Type: "sse", URL: "https://x/sse",
		Headers: []config.KV{{Key: "X-Api-Key", Value: "abc"}},
	}}
	got, err := renderPiOMP(entries, func(string) string { return "" }, func(string) {}, "omp")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, got, `"type": "sse"`)
	assertContains(t, got, `"X-Api-Key": "abc"`)
}

func TestPatchMCPJSONCreatesCanonicalFile(t *testing.T) {
	rendered := `{
  "s": {
    "type": "stdio",
    "command": "npx",
    "args": [],
    "env": {}
  }
}`
	out := string(patchMCPJSON(nil, rendered))
	if !json.Valid([]byte(out)) {
		t.Fatalf("created file is not valid JSON:\n%s", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("file must end with a newline:\n%q", out)
	}
	want := "{\n  \"mcpServers\": {\n    \"s\": {\n      \"type\": \"stdio\",\n      \"command\": \"npx\",\n      \"args\": [],\n      \"env\": {}\n    }\n  }\n}\n"
	if out != want {
		t.Fatalf("canonical output mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, out)
	}
}

func TestPatchMCPJSONPreservesOtherTopLevelKeys(t *testing.T) {
	base := `{
  "settings": {
    "theme": "dark"
  },
  "mcpServers": {
    "old": {
      "type": "stdio"
    }
  },
  "other": 42
}`
	rendered := `{
  "new": {
    "type": "stdio",
    "command": "npx",
    "args": [],
    "env": {}
  }
}`
	out := patchMCPJSON([]byte(base), rendered)

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
	mcp, _ := after["mcpServers"].(map[string]any)
	if _, ok := mcp["new"]; !ok {
		t.Fatalf("new server missing: %#v", mcp)
	}
	if _, ok := mcp["old"]; ok {
		t.Fatalf("old server retained: %#v", mcp)
	}
}

func TestPatchMCPJSONIdempotent(t *testing.T) {
	rendered := `{
  "s": {
    "type": "stdio",
    "command": "npx",
    "args": [],
    "env": {}
  }
}`
	once := patchMCPJSON(nil, rendered)
	twice := patchMCPJSON(once, rendered)
	if string(once) != string(twice) {
		t.Fatalf("patch not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
}

func TestPiOMPProjectAndUserApply(t *testing.T) {
	f := newFixture(t)
	f.writeSecrets("")
	f.writeManifest(`
[[servers]]
name = "pi-proj"
agents = ["pi"]
type = "stdio"
command = "npx"
profiles = ["base"]

[[servers]]
name = "omp-proj"
agents = ["omp"]
type = "stdio"
command = "npx"
profiles = ["base"]

[[servers]]
name = "omp-user"
agents = ["omp"]
type = "stdio"
command = "npx"
profiles = ["global"]
`)
	f.writeRepo(`profiles = ["base"]`)
	if _, _, err := f.run(Filters{Command: CmdApply, NonInteractive: true}); err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	piProject := filepath.Join(f.dir, ".pi", "mcp.json")
	ompProject := filepath.Join(f.dir, ".omp", "mcp.json")
	ompUser := filepath.Join(f.home, ".omp", "agent", "mcp.json")

	for path, name := range map[string]string{
		piProject:  "pi-proj",
		ompProject: "omp-proj",
		ompUser:    "omp-user",
	} {
		var after map[string]any
		if err := json.Unmarshal([]byte(readFile(t, path)), &after); err != nil {
			t.Fatalf("%s is not valid JSON: %v\n%s", path, err, readFile(t, path))
		}
		mcp, _ := after["mcpServers"].(map[string]any)
		if _, ok := mcp[name]; !ok {
			t.Fatalf("%s missing %q: %#v", path, name, after)
		}
	}

	// Re-applying must be byte-identical (idempotent).
	beforePi := readFile(t, piProject)
	beforeOmp := readFile(t, ompProject)
	if _, _, err := f.run(Filters{Command: CmdApply, NonInteractive: true}); err != nil {
		t.Fatalf("second apply failed: %v", err)
	}
	if readFile(t, piProject) != beforePi || readFile(t, ompProject) != beforeOmp {
		t.Fatal("repeated apply changed pi/omp output")
	}
}

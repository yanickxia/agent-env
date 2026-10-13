package repoinit

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/yanickxia/agent-env/internal/config"
)

func TestInitPreservesInlineDefinitions(t *testing.T) {
	for _, body := range []string{
		`agents = ["codex"]
[vars]
team = "local"
[[installs]]
source = "./skills"
name = "local"
skills = ["one", "two"]
env = { API_KEY = "${TOKEN}" }
[[servers]]
name = "local"
command = "./bin/mcp"
args = ["${TOKEN}"]
headers = { "X-Token" = "${TOKEN}" }
env = { Z = "z", A = "a" }
startup_timeout_sec = 30
`,
		`agents = []
installs = []
servers = []
[vars]
team = "empty"
`,
		`installs = []
[[servers]]
name = "local"
command = "mcp"
`,
	} {
		t.Run(body, func(t *testing.T) {
			f := newFixture(t)
			if err := os.WriteFile(f.path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			var before map[string]any
			if _, err := toml.Decode(body, &before); err != nil {
				t.Fatal(err)
			}
			if err := f.run(Filters{Profiles: []string{"base"}, DryRun: true}); err != nil || f.content() != body {
				t.Fatalf("dry-run must validate without writing: %v", err)
			}
			if err := f.run(Filters{Profiles: []string{"base"}}); err != nil {
				t.Fatal(err)
			}
			var after map[string]any
			if _, err := toml.Decode(f.content(), &after); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"installs", "servers", "vars", "agents"} {
				if !reflect.DeepEqual(before[key], after[key]) {
					t.Fatalf("%s changed: before=%+v after=%+v\n%s", key, before[key], after[key], f.content())
				}
			}
			if _, _, err := config.LoadRepoConfig(f.path); err != nil {
				t.Fatalf("init generated an invalid repo config: %v", err)
			}
		})
	}
}

func TestInitRejectsUnsafeInlineDefinitionsWithoutWriting(t *testing.T) {
	for _, extra := range []string{`post_install = ["echo unsafe"]`, `profiles = ["global"]`} {
		f := newFixture(t)
		body := "[[installs]]\nsource = \"example/local\"\n" + extra + "\n"
		if err := os.WriteFile(f.path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := f.run(Filters{Profiles: []string{"base"}}); err == nil || !strings.Contains(err.Error(), f.path) {
			t.Fatalf("invalid inline definition must fail with path: %v", err)
		}
		if f.content() != body {
			t.Fatal("init changed invalid config")
		}
	}
}

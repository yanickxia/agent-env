package add

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestAddPreservesInlineConfigAndRelativePaths(t *testing.T) {
	for _, domain := range []Domain{DomainSkills, DomainMCP} {
		for _, dryRun := range []bool{true, false} {
			t.Run(string(domain)+map[bool]string{true: "-dry-run", false: "-apply"}[dryRun], func(t *testing.T) {
				f := newFixture(t)
				body := `agents = ["codex"]
[[installs]]
source = "./local-skills"
skills = ["local-skill"]
[[servers]]
name = "local-server"
command = "./bin/local-server"
env = { API_KEY = "${TOKEN}" }
`
				f.write(f.repo, body)
				f.write(f.manifest, skillsNamedEntry+mcpServerEntry)
				opts, name := f.skillsOpts(), "clickup"
				if domain == DomainMCP {
					opts, name = f.mcpOpts(), "playwright"
				}
				if err := Run(opts, Filters{Names: []string{name}, DryRun: dryRun}); err != nil {
					t.Fatal(err)
				}
				if dryRun {
					if f.read(f.repo) != body {
						t.Fatal("dry-run modified config")
					}
				} else {
					var before, after map[string]any
					if _, err := toml.Decode(body, &before); err != nil {
						t.Fatal(err)
					}
					if _, err := toml.Decode(f.read(f.repo), &after); err != nil {
						t.Fatal(err)
					}
					for _, key := range []string{"installs", "servers", "agents"} {
						if !reflect.DeepEqual(before[key], after[key]) {
							t.Fatalf("add changed inline %s: %s", key, f.read(f.repo))
						}
					}
				}
				output := f.stdout.String()
				path := filepath.Join(f.dir, "local-skills")
				if domain == DomainMCP {
					path = filepath.Join(f.dir, "bin", "local-server")
					if !dryRun {
						output = f.read(filepath.Join(f.dir, ".codex", "config.toml"))
					}
				}
				if !strings.Contains(output, path) {
					t.Fatalf("inline path must remain config-relative: want %s in %s", path, output)
				}
				staged, err := filepath.Glob(filepath.Join(f.dir, ".agent-env-add-dry-run-*"))
				if err != nil || len(staged) != 0 {
					t.Fatalf("staged files not cleaned up: %v %v", staged, err)
				}
			})
		}
	}
}

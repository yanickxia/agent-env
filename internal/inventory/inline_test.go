package inventory

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInventoryRecognizesStandaloneInlineEntries(t *testing.T) {
	f := newLsFixture(t)
	f.write(f.repo, `agents = ["codex"]
[[installs]]
source = "./local-skills"
skills = ["local-skill"]
[[servers]]
name = "local-server"
command = "mcp"
env = { API_KEY = "${TOKEN}" }
`)
	if err := f.run(Filters{Declared: true}); err != nil {
		t.Fatal(err)
	}
	out := f.stdout()
	if !strings.Contains(out, "local-skill") || !strings.Contains(out, "local-server") || !strings.Contains(out, "(not installed)") {
		t.Fatalf("inline declarations missing: %s", out)
	}
	if strings.Contains(out, "(global)") || strings.Contains(out, "${TOKEN}") {
		t.Fatalf("inline declarations leaked into global scope or exposed values: %s", out)
	}
	f.installSkill(filepath.Join(f.dir, ".agents", "skills"), "local-skill")
	if err := f.run(Filters{Declared: true, Providers: []string{"codex"}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.stdout(), "no active declaration") {
		t.Fatalf("installed inline skill must have an active declaration: %s", f.stdout())
	}
	f.write(f.manifest, "installs = []\nservers = []\n")
	if err := f.run(Filters{Declared: true, NoRepo: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.stdout(), "local-") {
		t.Fatalf("--no-repo must ignore inline definitions: %s", f.stdout())
	}
}

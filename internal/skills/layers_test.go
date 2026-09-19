package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const layerManifest = `
[[installs]]
source = "example/base"
agents = ["codex"]
skills = ["lane"]
profiles = ["base"]

[[installs]]
source = "example/ark"
agents = ["codex"]
skills = ["mlops-lane"]
profiles = ["ark-mlops"]

[[installs]]
source = "example/frontend"
agents = ["codex"]
skills = ["web"]
profiles = ["frontend"]

[[installs]]
source = "example/global"
agents = ["codex"]
skills = ["g"]
profiles = ["global"]
`

type layerTree struct {
	root   string
	parent string
	repo   string
	sub    string
}

func newLayerTree(t *testing.T) layerTree {
	t.Helper()
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	repo := filepath.Join(parent, "repo")
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	return layerTree{root: root, parent: parent, repo: repo, sub: sub}
}

// layeredOpts points the runner at a deep subdir, with the install target still
// anchored at the repo root.
func layeredOpts(f *fixture, tr layerTree) Options {
	opts := f.opts
	opts.ProjectRoot = tr.repo
	opts.RepoConfigPath = filepath.Join(tr.repo, ".agent-env.toml")
	opts.StartDir = tr.sub
	return opts
}

func TestLayeredProfilesUnionAndResolveLayers(t *testing.T) {
	f := newFixture(t)
	tr := newLayerTree(t)
	f.writeManifest(layerManifest)
	f.write(filepath.Join(tr.parent, ".agent-env.toml"), `profiles = ["ark-mlops"]`)
	f.write(filepath.Join(tr.repo, ".agent-env.toml"), `profiles = ["base"]`)

	out, _, err := f.runWith(layeredOpts(f, tr), Filters{Command: CmdResolve, NonInteractive: true})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if !strings.Contains(out, "profiles: ark-mlops,base") {
		t.Fatalf("merged profiles wrong:\n%s", out)
	}
	repoLayer := filepath.Join(tr.repo, ".agent-env.toml")
	parentLayer := filepath.Join(tr.parent, ".agent-env.toml")
	ri, pi := strings.Index(out, repoLayer), strings.Index(out, parentLayer)
	if ri == -1 || pi == -1 || ri > pi {
		t.Fatalf("resolve must list layers nearest-first (repo then parent):\n%s", out)
	}
}

func TestLayeredGatingDryRun(t *testing.T) {
	f := newFixture(t)
	tr := newLayerTree(t)
	f.writeManifest(layerManifest)
	f.write(filepath.Join(tr.parent, ".agent-env.toml"), `profiles = ["ark-mlops"]`)
	f.write(filepath.Join(tr.repo, ".agent-env.toml"), `profiles = ["base"]`)

	out, errb, err := f.runWith(layeredOpts(f, tr), Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out, "example/base") || !strings.Contains(out, "example/ark") {
		t.Fatalf("base/ark entries must be selected by merged profiles:\n%s", out)
	}
	if strings.Contains(out, "example/frontend") {
		t.Fatalf("frontend must be skipped:\n%s", out)
	}
	if !strings.Contains(out, "example/global") {
		t.Fatalf("global entry must install:\n%s", out)
	}
	if strings.Contains(errb, "skipped") {
		t.Fatalf("no repo-level skips expected with a selection: %q", errb)
	}
}

func TestLayeredOnlyParentInherited(t *testing.T) {
	f := newFixture(t)
	tr := newLayerTree(t)
	f.writeManifest(layerManifest)
	f.write(filepath.Join(tr.parent, ".agent-env.toml"), `profiles = ["ark-mlops"]`)

	out, _, err := f.runWith(layeredOpts(f, tr), Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out, "example/ark") || strings.Contains(out, "example/base") {
		t.Fatalf("child without config must inherit parent (ark only):\n%s", out)
	}
}

func TestLayeredOnlyChild(t *testing.T) {
	f := newFixture(t)
	tr := newLayerTree(t)
	f.writeManifest(layerManifest)
	f.write(filepath.Join(tr.repo, ".agent-env.toml"), `profiles = ["base"]`)

	out, _, err := f.runWith(layeredOpts(f, tr), Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out, "example/base") || strings.Contains(out, "example/ark") {
		t.Fatalf("child only: base selected:\n%s", out)
	}
}

func TestLayeredNoConfigInstallsGlobalOnly(t *testing.T) {
	f := newFixture(t)
	tr := newLayerTree(t)
	f.writeManifest(layerManifest)

	out, errb, err := f.runWith(layeredOpts(f, tr), Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out, "example/global") {
		t.Fatalf("global must install:\n%s", out)
	}
	if strings.Contains(out, "example/base") || strings.Contains(out, "example/ark") || strings.Contains(out, "example/frontend") {
		t.Fatalf("repo-level entries must be skipped:\n%s", out)
	}
	if !strings.Contains(errb, "skipped 3 repo-level entries") {
		t.Fatalf("want skip notice, got %q", errb)
	}
	if !strings.Contains(errb, "up to /") {
		t.Fatalf("skip notice should mention the cwd->/ search, got %q", errb)
	}

	_, _, err = f.runWith(layeredOpts(f, tr), Filters{Command: CmdResolve})
	if err == nil || !strings.Contains(err.Error(), "need a repo config or --profile") || !strings.Contains(err.Error(), "up to /") {
		t.Fatalf("resolve must report the search path, got %v", err)
	}
}

func TestLayeredAgentsNearestWinsNarrowing(t *testing.T) {
	f := newFixture(t)
	tr := newLayerTree(t)
	f.writeManifest(`
[[installs]]
source = "example/multi"
agents = ["codex", "opencode"]
skills = ["lane"]
profiles = ["base"]
`)
	f.write(filepath.Join(tr.parent, ".agent-env.toml"), "profiles = [\"base\"]\nagents = [\"trae\"]\n")
	f.write(filepath.Join(tr.repo, ".agent-env.toml"), "profiles = [\"base\"]\nagents = [\"codex\"]\n")

	out, _, err := f.runWith(layeredOpts(f, tr), Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out, "-a codex") || strings.Contains(out, "-a opencode") || strings.Contains(out, "-a trae") {
		t.Fatalf("nearest agents (codex) must win:\n%s", out)
	}
}

// Running from a repo subdirectory must still select the repo-root layer while
// the install target stays the git toplevel.
func TestLayeredSubdirUsesRepoRootAsTarget(t *testing.T) {
	f := newFixture(t)
	tr := newLayerTree(t)
	f.writeManifest(layerManifest)
	f.write(filepath.Join(tr.repo, ".agent-env.toml"), `profiles = ["base"]`)

	out, _, err := f.runWith(layeredOpts(f, tr), Filters{Command: CmdDryRun, NonInteractive: true})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out, "cd "+tr.repo+" &&") {
		t.Fatalf("install target must be the repo root, not the cwd:\n%s", out)
	}
	if strings.Contains(out, tr.sub) {
		t.Fatalf("install target must not be the subdirectory:\n%s", out)
	}
}

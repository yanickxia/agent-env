package add

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skillsNamedEntry is an orphan repo-level install: no profiles, so only the
// repo config's names selector can activate it — exactly what add records.
const skillsNamedEntry = `
[[installs]]
source = "example/clickup"
name = "clickup"
agents = ["codex"]
skills = ["clickup-cli"]
`

const skillsNamelessEntry = `
[[installs]]
source = "example/nameless"
agents = ["codex"]
skills = ["mlops-lane"]
profiles = ["base"]
`

const skillsGlobalNamedEntry = `
[[installs]]
source = "example/vision"
name = "vision"
agents = ["codex"]
skills = ["agent-vision"]
profiles = ["global"]
`

const mcpServerEntry = `
[[servers]]
name = "playwright"
command = "npx"
args = ["-y", "@playwright/mcp@latest"]
agents = ["codex"]
`

const mcpGlobalServerEntry = `
[[servers]]
name = "context7"
command = "npx"
args = ["-y", "ctx7"]
agents = ["codex"]
profiles = ["global"]
`

type fixture struct {
	t        *testing.T
	dir      string
	manifest string
	state    string
	repo     string
	home     string
	secrets  string
	log      string
	stdout   bytes.Buffer
	stderr   bytes.Buffer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	stub := filepath.Join(dir, "stub")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stub, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "npx.log")
	t.Setenv("PATH", stub+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NPX_LOG", log)
	for _, name := range []string{"npx", "node"} {
		script := "#!/bin/sh\nprintf '%s\\t%s\\n' \"$PWD\" \"$*\" >> \"${NPX_LOG:-/dev/null}\"\nexit 0\n"
		if err := os.WriteFile(filepath.Join(stub, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &fixture{
		t:        t,
		dir:      dir,
		manifest: filepath.Join(dir, "config.toml"),
		state:    filepath.Join(dir, "state.tsv"),
		repo:     filepath.Join(dir, ".agent-env.toml"),
		home:     home,
		secrets:  filepath.Join(dir, "secrets.toml"),
		log:      log,
	}
}

func (f *fixture) skillsOpts() Options {
	return Options{
		Domain:         DomainSkills,
		ManifestPath:   f.manifest,
		StatePath:      f.state,
		RepoConfigPath: f.repo,
		ProjectRoot:    f.dir,
		StartDir:       f.dir,
		Home:           f.home,
		Stdout:         &f.stdout,
		Stderr:         &f.stderr,
		LookupEnv:      os.Getenv,
		InRepo:         true,
	}
}

func (f *fixture) mcpOpts() Options {
	return Options{
		Domain:         DomainMCP,
		ManifestPath:   f.manifest,
		SecretsPath:    f.secrets,
		RepoConfigPath: f.repo,
		ProjectRoot:    f.dir,
		StartDir:       f.dir,
		Home:           f.home,
		ClaudeJSON:     filepath.Join(f.home, ".claude.json"),
		Stdout:         &f.stdout,
		Stderr:         &f.stderr,
		Stdin:          strings.NewReader(""),
		LookupEnv:      os.Getenv,
		InRepo:         true,
	}
}

func (f *fixture) write(path, body string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) read(path string) string {
	f.t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func (f *fixture) npxCalls() int {
	f.t.Helper()
	text := f.read(f.log)
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n")
}

// --- skills add ---------------------------------------------------------------

func TestSkillsAddCreatesConfigAndInstalls(t *testing.T) {
	f := newFixture(t)
	f.write(f.manifest, skillsNamedEntry)

	err := Run(f.skillsOpts(), Filters{Names: []string{"clickup"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg := f.read(f.repo)
	if !strings.Contains(cfg, `names = ["clickup"]`) {
		t.Fatalf("repo config must record the name:\n%s", cfg)
	}
	if !strings.Contains(f.stdout.String(), "added: clickup -> "+f.repo) {
		t.Fatalf("want summary line, got:\n%s", f.stdout.String())
	}
	// The recorded entry must actually install via npx in the repo.
	if got := f.npxCalls(); got != 1 {
		t.Fatalf("npx calls = %d, want 1\nstderr: %s", got, f.stderr.String())
	}
	if log := f.read(f.log); !strings.Contains(log, "example/clickup") {
		t.Fatalf("npx must be invoked for the added entry, log:\n%s", log)
	}
}

func TestSkillsAddMergesExistingConfigAndDedupes(t *testing.T) {
	f := newFixture(t)
	f.write(f.manifest, skillsNamedEntry)
	f.write(f.repo, "profiles = [\"base\"]\nnames = [\"keep\"]\nmode = \"copy\"\n\n[vars]\nteam = \"ark\"\n")

	err := Run(f.skillsOpts(), Filters{Names: []string{"clickup"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg := f.read(f.repo)
	for _, want := range []string{
		`profiles = ["base"]`,
		`names = ["keep", "clickup"]`,
		`mode = "copy"`,
		`team = "ark"`,
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("repo config must keep %s:\n%s", want, cfg)
		}
	}
}

func TestSkillsAddRepeatIsIdempotent(t *testing.T) {
	f := newFixture(t)
	f.write(f.manifest, skillsNamedEntry)

	if err := Run(f.skillsOpts(), Filters{Names: []string{"clickup"}}); err != nil {
		t.Fatalf("first add failed: %v", err)
	}
	if got := f.npxCalls(); got != 1 {
		t.Fatalf("first add npx calls = %d, want 1", got)
	}
	first := f.read(f.repo)
	f.stdout.Reset()

	if err := Run(f.skillsOpts(), Filters{Names: []string{"clickup"}}); err != nil {
		t.Fatalf("second add failed: %v", err)
	}
	if got := strings.Count(first, `"clickup"`); got != 1 {
		t.Fatalf("repo config has %d clickup entries, want 1:\n%s", got, first)
	}
	// Stamp skip: the second apply must not re-run npx.
	if got := f.npxCalls(); got != 1 {
		t.Fatalf("second add npx calls = %d, want 0 extra (stamp skip)\nstderr: %s", got, f.stderr.String())
	}
	if !strings.Contains(f.stdout.String(), "names already recorded") {
		t.Fatalf("repeat add must report an unchanged config, got:\n%s", f.stdout.String())
	}
}

func TestSkillsAddGlobalEntryNotesButDoesNotRecord(t *testing.T) {
	f := newFixture(t)
	f.write(f.manifest, skillsGlobalNamedEntry)

	err := Run(f.skillsOpts(), Filters{Names: []string{"vision"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(f.repo); !os.IsNotExist(statErr) {
		t.Fatalf("global entry must not be recorded in the repo config")
	}
	if out := f.stdout.String(); !strings.Contains(out, "note: vision is global, installs everywhere; nothing to record") {
		t.Fatalf("want global note, got:\n%s", out)
	}
	if got := f.npxCalls(); got != 1 {
		t.Fatalf("global entry should still install once, npx calls = %d", got)
	}
}

// --- mcp add ------------------------------------------------------------------

func TestMCPAddPersistsNamesAndWritesProjectTarget(t *testing.T) {
	f := newFixture(t)
	f.write(f.manifest, mcpServerEntry)

	err := Run(f.mcpOpts(), Filters{Names: []string{"playwright"}})
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, f.stderr.String())
	}
	if cfg := f.read(f.repo); !strings.Contains(cfg, `names = ["playwright"]`) {
		t.Fatalf("repo config must record the server name:\n%s", cfg)
	}
	codex := f.read(filepath.Join(f.dir, ".codex", "config.toml"))
	if !strings.Contains(codex, "playwright") {
		t.Fatalf("codex project config must contain the server:\n%s", codex)
	}
}

func TestMCPAddGlobalServerNotRecorded(t *testing.T) {
	f := newFixture(t)
	f.write(f.manifest, mcpGlobalServerEntry)

	err := Run(f.mcpOpts(), Filters{Names: []string{"context7"}})
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, f.stderr.String())
	}
	if _, statErr := os.Stat(f.repo); !os.IsNotExist(statErr) {
		t.Fatalf("global server must not be recorded in the repo config")
	}
	if out := f.stdout.String(); !strings.Contains(out, "note: context7 is global") {
		t.Fatalf("want global note, got:\n%s", out)
	}
}

// --- failure modes -------------------------------------------------------------

func TestUnknownNameFailsWithoutSideEffects(t *testing.T) {
	f := newFixture(t)
	f.write(f.manifest, skillsNamedEntry+skillsNamelessEntry)

	err := Run(f.skillsOpts(), Filters{Names: []string{"lark"}})
	if err == nil {
		t.Fatal("unknown name must fail")
	}
	msg := err.Error()
	for _, want := range []string{
		"unknown skill name(s): lark",
		"available for skills add",
		"clickup",
		"entries without a name field cannot be added yet",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error must mention %q, got:\n%s", want, msg)
		}
	}
	// The nameless entry must not appear as addable.
	if strings.Contains(msg, "example/nameless") {
		t.Fatalf("nameless entries have no addable name, got:\n%s", msg)
	}
	// Zero side effects: no repo config, no stamps, no installs.
	if _, statErr := os.Stat(f.repo); !os.IsNotExist(statErr) {
		t.Fatalf("failed add must not write the repo config")
	}
	if _, statErr := os.Stat(f.state); !os.IsNotExist(statErr) {
		t.Fatalf("failed add must not write stamps")
	}
	if got := f.npxCalls(); got != 0 {
		t.Fatalf("failed add must not install, npx calls = %d", got)
	}
}

func TestPartialUnknownFailFast(t *testing.T) {
	f := newFixture(t)
	f.write(f.manifest, skillsNamedEntry)

	err := Run(f.skillsOpts(), Filters{Names: []string{"clickup", "lark"}})
	if err == nil {
		t.Fatal("any unknown name must fail the whole command")
	}
	if !strings.Contains(err.Error(), "lark") {
		t.Fatalf("error must name the unknown entry:\n%s", err)
	}
	if _, statErr := os.Stat(f.repo); !os.IsNotExist(statErr) {
		t.Fatalf("fail-fast must leave the repo config untouched")
	}
	if got := f.npxCalls(); got != 0 {
		t.Fatalf("fail-fast must not install, npx calls = %d", got)
	}
}

func TestMCPUnknownNameListsServers(t *testing.T) {
	f := newFixture(t)
	f.write(f.manifest, mcpServerEntry)

	err := Run(f.mcpOpts(), Filters{Names: []string{"nope"}})
	if err == nil {
		t.Fatal("unknown server name must fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unknown server name(s): nope") || !strings.Contains(msg, "playwright") {
		t.Fatalf("error must list available servers, got:\n%s", msg)
	}
}

func TestOutOfRepoContextRejected(t *testing.T) {
	f := newFixture(t)
	f.write(f.manifest, skillsNamedEntry)

	opts := f.skillsOpts()
	opts.InRepo = false
	err := Run(opts, Filters{Names: []string{"clickup"}})
	if err == nil || !strings.Contains(err.Error(), "only works inside a repository") {
		t.Fatalf("non-repo cwd must be rejected, got: %v", err)
	}

	err = Run(f.skillsOpts(), Filters{Names: []string{"clickup"}, NoRepo: true})
	if err == nil || !strings.Contains(err.Error(), "--no-repo") || !strings.Contains(err.Error(), "apply --no-repo") {
		t.Fatalf("--no-repo must be rejected with an apply --no-repo hint, got: %v", err)
	}
	if _, statErr := os.Stat(f.repo); !os.IsNotExist(statErr) {
		t.Fatalf("rejected add must not write the repo config")
	}
}

// --- dry-run --------------------------------------------------------------------

func TestDryRunHasNoSideEffects(t *testing.T) {
	f := newFixture(t)
	f.write(f.manifest, skillsNamedEntry)

	err := Run(f.skillsOpts(), Filters{Names: []string{"clickup"}, DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := f.stdout.String()
	if !strings.Contains(out, "would write "+f.repo) || !strings.Contains(out, `names = ["clickup"]`) {
		t.Fatalf("dry-run must show the planned config, got:\n%s", out)
	}
	if !strings.Contains(out, "example/clickup") {
		t.Fatalf("dry-run must show the planned install command, got:\n%s", out)
	}
	if _, statErr := os.Stat(f.repo); !os.IsNotExist(statErr) {
		t.Fatalf("dry-run must not write the repo config")
	}
	if _, statErr := os.Stat(f.state); !os.IsNotExist(statErr) {
		t.Fatalf("dry-run must not write stamps")
	}
	if got := f.npxCalls(); got != 0 {
		t.Fatalf("dry-run must not install, npx calls = %d", got)
	}
}

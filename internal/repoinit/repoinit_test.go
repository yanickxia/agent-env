package repoinit

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixture struct {
	t    *testing.T
	dir  string
	path string
	env  map[string]string
	opts Options
	out  bytes.Buffer
	errb bytes.Buffer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	f := &fixture{
		t:    t,
		dir:  dir,
		path: filepath.Join(dir, ".agent-env.toml"),
		env:  map[string]string{},
	}
	f.opts = Options{
		RepoConfigPath: f.path,
		ProjectRoot:    dir,
		Stdout:         &f.out,
		Stderr:         &f.errb,
		LookupEnv:      func(k string) string { return f.env[k] },
		Executable:     func() (string, error) { return "/exe/agent-env", nil },
	}
	return f
}

func (f *fixture) run(fl Filters) error {
	f.out.Reset()
	f.errb.Reset()
	return Run(f.opts, fl)
}

func (f *fixture) content() string {
	data, _ := os.ReadFile(f.path)
	return string(data)
}

func TestInitCreatesNewConfig(t *testing.T) {
	f := newFixture(t)
	err := f.run(Filters{Profiles: []string{"base", "ark-mlops"}, Agents: []string{"codex", "opencode"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "# Managed by agent-env init. profiles/names select which skill groups\n" +
		"# and individual entries agent-env installs into this repo. Safe to edit by hand.\n" +
		"\n" +
		"profiles = [\"base\", \"ark-mlops\"]\n" +
		"agents = [\"codex\", \"opencode\"]\n"
	if f.content() != want {
		t.Fatalf("content mismatch:\n--- want ---\n%s--- got ---\n%s", want, f.content())
	}
	assertContains(t, f.out.String(), "wrote "+f.path)
	assertContains(t, f.out.String(), "profiles: base, ark-mlops")
	assertContains(t, f.out.String(), "agents: codex, opencode")
	assertContains(t, f.out.String(), "next: agent-env skills apply --non-interactive")
}

func TestInitMergesExisting(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(f.path, []byte(`# a user comment
profiles = ["base"]
agents = ["codex"]
mode = "copy"

[vars]
team = "ark"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := f.run(Filters{Profiles: []string{"ark-mlops"}, Agents: []string{"opencode"}})
	if err != nil {
		t.Fatal(err)
	}
	got := f.content()
	for _, want := range []string{
		`profiles = ["base", "ark-mlops"]`,
		`agents = ["codex", "opencode"]`,
		`mode = "copy"`,
		"[vars]",
		`team = "ark"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("merged content missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "# a user comment") {
		t.Fatalf("comments must not be preserved:\n%s", got)
	}
}

func TestInitDryRunDoesNotWrite(t *testing.T) {
	f := newFixture(t)
	err := f.run(Filters{Profiles: []string{"base"}, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.path); !os.IsNotExist(err) {
		t.Fatalf("dry-run must not create the file")
	}
	assertContains(t, f.out.String(), "would write "+f.path)
	assertContains(t, f.out.String(), `profiles = ["base"]`)
}

func TestInitRejectsInvalidProfile(t *testing.T) {
	f := newFixture(t)
	err := f.run(Filters{Profiles: []string{"Base"}})
	if err == nil || !strings.Contains(err.Error(), "invalid profile name 'Base'") {
		t.Fatalf("want invalid profile error, got %v", err)
	}
	err = f.run(Filters{Profiles: []string{"ark_mlops"}})
	if err == nil || !strings.Contains(err.Error(), "invalid profile name 'ark_mlops'") {
		t.Fatalf("want invalid profile error, got %v", err)
	}
}

func TestInitRequiresProfile(t *testing.T) {
	f := newFixture(t)
	err := f.run(Filters{})
	if err == nil || !strings.Contains(err.Error(), "at least one PROFILE or --name is required") {
		t.Fatalf("want required-profile error, got %v", err)
	}
}

func TestInitDedupes(t *testing.T) {
	f := newFixture(t)
	if err := f.run(Filters{Profiles: []string{"base", "base"}, Agents: []string{"codex", "codex"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.content(), `profiles = ["base"]`) {
		t.Fatalf("profiles not deduped:\n%s", f.content())
	}
	if !strings.Contains(f.content(), `agents = ["codex"]`) {
		t.Fatalf("agents not deduped:\n%s", f.content())
	}
}

func TestInitRejectsUnknownKeys(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(f.path, []byte("profiles = [\"base\"]\nsource = \"evil\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := f.run(Filters{Profiles: []string{"ark-mlops"}})
	if err == nil || !strings.Contains(err.Error(), "key(s) not allowed: source") {
		t.Fatalf("want unknown-key error, got %v", err)
	}
}

func TestInitRejectsBadVars(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(f.path, []byte("profiles = [\"base\"]\n\n[vars]\nx = { nested = 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := f.run(Filters{Profiles: []string{"ark-mlops"}})
	if err == nil || !strings.Contains(err.Error(), `"vars.x" must be a string, number, boolean, or array of strings`) {
		t.Fatalf("want vars error, got %v", err)
	}
}

func TestInitApplyUsesOverrideAndPropagatesRC(t *testing.T) {
	f := newFixture(t)
	f.env["AGENT_SKILLS_SYNC_BIN"] = "/stub/agent-env"

	var gotBin, gotDir string
	f.opts.RunApply = func(bin, dir string, stdout, stderr io.Writer) error {
		gotBin, gotDir = bin, dir
		return &ApplyFailedError{Code: 7}
	}

	err := f.run(Filters{Profiles: []string{"base"}, Apply: true})
	if err == nil {
		t.Fatal("want ApplyFailedError")
	}
	var afe *ApplyFailedError
	if !errors.As(err, &afe) || afe.Code != 7 {
		t.Fatalf("want code 7, got %v", err)
	}
	if gotBin != "/stub/agent-env" || gotDir != f.dir {
		t.Fatalf("apply called with bin=%q dir=%q", gotBin, gotDir)
	}
	assertContains(t, f.out.String(), "/stub/agent-env skills apply --non-interactive --skip-unchanged")
}

func TestInitApplyDryRunPrintsButDoesNotRun(t *testing.T) {
	f := newFixture(t)
	f.env["AGENT_SKILLS_SYNC_BIN"] = "/stub/agent-env"
	called := false
	f.opts.RunApply = func(bin, dir string, stdout, stderr io.Writer) error {
		called = true
		return nil
	}
	if err := f.run(Filters{Profiles: []string{"base"}, Apply: true, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("--apply --dry-run must not run the sync")
	}
	assertContains(t, f.out.String(), "/stub/agent-env skills apply")
	if _, err := os.Stat(f.path); !os.IsNotExist(err) {
		t.Fatal("dry-run must not write")
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected to contain %q, got:\n%s", needle, haystack)
	}
}

func TestInitWritesAndMergesNames(t *testing.T) {
	f := newFixture(t)
	if err := f.run(Filters{Profiles: []string{"base"}, Names: []string{"clickup"}}); err != nil {
		t.Fatal(err)
	}
	got := f.content()
	if !strings.Contains(got, `profiles = ["base"]`) || !strings.Contains(got, `names = ["clickup"]`) {
		t.Fatalf("content missing selectors:\n%s", got)
	}
	if !strings.Contains(f.out.String(), "names: clickup") {
		t.Fatalf("output missing names: %q", f.out.String())
	}

	// merge: existing names kept, new appended
	if err := f.run(Filters{Names: []string{"playwright"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.content(), `names = ["clickup", "playwright"]`) {
		t.Fatalf("names not merged:\n%s", f.content())
	}
}

func TestInitNameOnlyIsValid(t *testing.T) {
	f := newFixture(t)
	if err := f.run(Filters{Names: []string{"clickup"}}); err != nil {
		t.Fatalf("name-only init must be valid: %v", err)
	}
	if !strings.Contains(f.content(), `names = ["clickup"]`) {
		t.Fatalf("content:\n%s", f.content())
	}
}

func TestInitRejectsGlobalName(t *testing.T) {
	f := newFixture(t)
	err := f.run(Filters{Names: []string{"global"}})
	if err == nil || !strings.Contains(err.Error(), "reserved keyword") {
		t.Fatalf("want reserved-name error, got %v", err)
	}
}

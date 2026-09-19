package skills

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

type fixture struct {
	t        *testing.T
	dir      string
	manifest string
	state    string
	repo     string
	home     string
	stub     string
	opts     Options
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
	installStub(t, stub, "npx")
	installStub(t, stub, "node")
	t.Setenv("PATH", stub+string(os.PathListSeparator)+os.Getenv("PATH"))

	f := &fixture{
		t:        t,
		dir:      dir,
		manifest: filepath.Join(dir, "config.toml"),
		state:    filepath.Join(dir, "state.tsv"),
		repo:     filepath.Join(dir, ".agent-env.toml"),
		home:     home,
		stub:     stub,
	}
	f.opts = Options{
		ManifestPath:   f.manifest,
		StatePath:      f.state,
		RepoConfigPath: f.repo,
		ProjectRoot:    dir,
		Home:           home,
	}
	return f
}

func installStub(t *testing.T, dir, name string) {
	t.Helper()
	script := "#!/bin/sh\nprintf '%s\\t%s\\n' \"$PWD\" \"$*\" >> \"${NPX_LOG:-/dev/null}\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
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

func (f *fixture) writeManifest(body string) { f.write(f.manifest, body) }
func (f *fixture) writeRepo(body string)     { f.write(f.repo, body) }

func (f *fixture) run(fl Filters) (string, string, error) {
	f.t.Helper()
	return f.runWith(f.opts, fl)
}

func (f *fixture) runWith(opts Options, fl Filters) (string, string, error) {
	f.t.Helper()
	var out, errb bytes.Buffer
	opts.Stdout = &out
	opts.Stderr = &errb
	err := Run(opts, fl)
	return out.String(), errb.String(), err
}

func readFileOrEmpty(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func lineCount(t *testing.T, path string) int {
	t.Helper()
	text := readFileOrEmpty(t, path)
	if text == "" {
		return 0
	}
	n := 1
	for _, r := range text {
		if r == '\n' {
			n++
		}
	}
	if len(text) > 0 && text[len(text)-1] == '\n' {
		n--
	}
	return n
}

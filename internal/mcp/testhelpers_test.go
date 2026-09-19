package mcp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixture struct {
	t        *testing.T
	dir      string
	home     string
	manifest string
	secrets  string
	repo     string
	claude   string
	env      map[string]string
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
	t.Setenv("PATH", stub+string(os.PathListSeparator)+os.Getenv("PATH"))

	f := &fixture{
		t:        t,
		dir:      dir,
		home:     home,
		manifest: filepath.Join(dir, "config.toml"),
		secrets:  filepath.Join(dir, "secrets.toml"),
		repo:     filepath.Join(dir, ".agent-env.toml"),
		claude:   filepath.Join(home, ".claude.json"),
		env:      map[string]string{},
	}
	f.opts = Options{
		ManifestPath:   f.manifest,
		SecretsPath:    f.secrets,
		RepoConfigPath: f.repo,
		ProjectRoot:    dir,
		Home:           home,
		WorkDir:        dir,
		ClaudeJSON:     f.claude,
		LookupEnv:      func(k string) string { return f.env[k] },
	}
	return f
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
func (f *fixture) writeSecrets(body string)  { f.write(f.secrets, body) }
func (f *fixture) writeRepo(body string)     { f.write(f.repo, body) }

func (f *fixture) run(filters Filters) (string, string, error) {
	return f.runWith(f.opts, filters)
}

func (f *fixture) runWith(opts Options, filters Filters) (string, string, error) {
	f.t.Helper()
	var out, errb bytes.Buffer
	opts.Stdout = &out
	opts.Stderr = &errb
	if opts.Stdin == nil {
		opts.Stdin = strings.NewReader("")
	}
	if opts.LookupEnv == nil {
		opts.LookupEnv = func(k string) string { return f.env[k] }
	}
	err := Run(opts, filters)
	return out.String(), errb.String(), err
}

func installLogStub(t *testing.T, dir, name string, logPath string) {
	t.Helper()
	script := "#!/bin/sh\nprintf '%s\\t%s\\n' \"$PWD\" \"$*\" >> \"" + logPath + "\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected to contain %q, got:\n%s", needle, haystack)
	}
}

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Fatalf("expected NOT to contain %q, got:\n%s", needle, haystack)
	}
}

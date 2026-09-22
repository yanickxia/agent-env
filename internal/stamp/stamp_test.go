package stamp

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yanickxia/agent-env/internal/config"
)

func hexSha(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// TestSignatureMatchesRealStateFile is the most important regression guard:
// the Go signature must stay byte-identical to the zsh implementation, so the
// real state.tsv rows remain valid. Both fixtures are snapshots of the user's
// live files.
func TestSignatureMatchesRealStateFile(t *testing.T) {
	entries, err := config.ParseManifest("testdata/config.toml")
	if err != nil {
		t.Fatalf("parse fixture config: %v", err)
	}
	store := Store{Path: "testdata/state.tsv"}

	checked := 0
	for _, e := range entries {
		if !e.Global {
			continue
		}
		// Global entries derive the historical "user" scope in the signature.
		sig := Signature(e.Source, e.AgentsRaw, e.SkillsRaw, e.Mode, "user", e.Installer, e.EnvRaw, "", "")
		got, ok := store.Lookup(e.Source, "user")
		if !ok {
			t.Errorf("no stamp row for %s", e.Source)
			continue
		}
		if got != sig {
			t.Errorf("signature mismatch for %s:\n  want %s\n  got  %s", e.Source, got, sig)
		}
		checked++
	}
	if checked != 9 {
		t.Fatalf("expected 9 user entries in fixture, checked %d", checked)
	}
}

func TestSignatureKnownVector(t *testing.T) {
	got := Signature("microsoft/playwright-cli", "claude-code,codex,opencode", "playwright-cli", "symlink", "user", "skills", "", "", "")
	want := "1827bc82dcdd0f79c3e0e6d1857158ea686368cdf08457265b888ed95b5c59e6"
	if got != want {
		t.Fatalf("known vector mismatch: want %s got %s", want, got)
	}
}

func TestProjectSignaturePinsProfilesAndRoot(t *testing.T) {
	src, agents, skills, mode, installer := "example/ark", "codex", "lane", "symlink", "skills"
	got := Signature(src, agents, skills, mode, "project", installer, "", "base,ark-mlops", "/repo/a")
	payload := strings.Join([]string{src, agents, skills, mode, "project", installer, ""}, "|") + "|base,ark-mlops|/repo/a"
	want := hexSha(payload)
	if got != want {
		t.Fatalf("project signature mismatch:\n  want %s\n  got  %s", want, got)
	}

	// A different root or profile set must change the signature.
	if other := Signature(src, agents, skills, mode, "project", installer, "", "base", "/repo/b"); other == got {
		t.Fatal("expected signature to differ across repo roots")
	}
	if other := Signature(src, agents, skills, mode, "project", installer, "", "frontend", "/repo/a"); other == got {
		t.Fatal("expected signature to differ across profile sets")
	}

	// User scope deliberately carries no trailing fields.
	userPayload := strings.Join([]string{src, agents, skills, mode, "user", installer, ""}, "|") + "|base|/repo/a"
	if Signature(src, agents, skills, mode, "user", installer, "", "base", "/repo/a") != hexSha(userPayload) {
		t.Fatal("user signature must ignore profiles/repo_root")
	}
}

func TestLookupAndWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.tsv")
	store := Store{Path: path}

	if _, ok := store.Lookup("x", "user"); ok {
		t.Fatal("lookup on missing file should return ok=false")
	}

	pre := "other/one\tuser\tAAA\nkeep/me\tproject:/r\tBBB\n"
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Write("x", "user", "SIG1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Write("x", "user", "SIG2"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	text := string(data)
	if strings.Count(text, "x\tuser") != 1 {
		t.Fatalf("upsert must keep exactly one row, got:\n%s", text)
	}
	if !strings.Contains(text, "SIG2") || strings.Contains(text, "SIG1") {
		t.Fatalf("upsert must replace the signature, got:\n%s", text)
	}
	if !strings.Contains(text, "other/one\tuser\tAAA\n") || !strings.Contains(text, "keep/me\tproject:/r\tBBB\n") {
		t.Fatalf("unrelated rows must survive byte-for-byte, got:\n%s", text)
	}

	got, ok := store.Lookup("x", "user")
	if !ok || got != "SIG2" {
		t.Fatalf("lookup after write = %q,%v", got, ok)
	}
}

func TestListUsesDelimiterAwarePrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.tsv")
	store := Store{Path: path}
	pre := "a\tuser\tA\n" +
		"b\tproject:/repo\tB\n" +
		"c\tproject:/repo2\tC\n" +
		"d\tproject:/x/y\tD\n"
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}

	user := store.List("user")
	if len(user) != 1 || user[0].Source != "a" {
		t.Fatalf("user rows = %+v", user)
	}
	repo := store.List("project:/repo")
	if len(repo) != 1 || repo[0].Source != "b" {
		t.Fatalf("project:/repo must not match project:/repo2, got %+v", repo)
	}
	all := store.List("project:")
	if len(all) != 3 {
		t.Fatalf("project family = %+v", all)
	}
}

func TestDeletePreservesOtherRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.tsv")
	store := Store{Path: path}
	pre := "a\tuser\tA\nb\tuser\tB\nc\tproject:/r\tC\n"
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := store.Delete([]Row{{Source: "b", Scope: "user"}}); err != nil {
		t.Fatal(err)
	}
	want := "a\tuser\tA\nc\tproject:/r\tC\n"
	if got := string(readAll(t, path)); got != want {
		t.Fatalf("delete preserved bytes mismatch:\nwant %q\ngot  %q", want, got)
	}

	// A no-op delete (no rows, or a miss) leaves the file untouched.
	if err := store.Delete(nil); err != nil {
		t.Fatal(err)
	}
	if got := string(readAll(t, path)); got != want {
		t.Fatalf("empty delete must be a no-op, got %q", got)
	}
	if err := store.Delete([]Row{{Source: "missing", Scope: "user"}}); err != nil {
		t.Fatal(err)
	}
	if got := string(readAll(t, path)); got != want {
		t.Fatalf("miss delete must be a no-op, got %q", got)
	}

	// Deleting from a missing file is fine.
	missing := Store{Path: filepath.Join(dir, "nope", "state.tsv")}
	if err := missing.Delete([]Row{{Source: "a", Scope: "user"}}); err != nil {
		t.Fatalf("delete on missing file: %v", err)
	}
}

func readAll(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

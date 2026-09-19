package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func makeTarGz(t *testing.T, goos, goarch string, bin []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	dir := "agent-env_" + goos + "_" + goarch
	for _, f := range []struct {
		name string
		data []byte
	}{
		{dir + "/agent-env", bin},
		{dir + "/README.md", []byte("readme")},
	} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.data); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func shaHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// updateServer serves a HEAD "latest" redirect plus GET tar.gz/.sha256 assets.
// sumData is the content served for the .sha256 file (to allow tamper tests).
func updateServer(t *testing.T, tag, goos, goarch string, tarData []byte, sumData string) (*httptest.Server, *int32) {
	t.Helper()
	asset := "agent-env_" + goos + "_" + goarch + ".tar.gz"
	var count int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		if strings.Contains(r.URL.Path, "/releases/latest/download/") {
			w.Header().Set("Location", srv.URL+"/releases/download/"+tag+"/"+asset)
			w.WriteHeader(http.StatusFound)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			_, _ = w.Write([]byte(sumData))
			return
		}
		if strings.HasSuffix(r.URL.Path, asset) {
			_, _ = w.Write(tarData)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &count
}

func newExe(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent-env")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunUpdatesBinary(t *testing.T) {
	bin := []byte("#!/bin/sh\necho new-binary\n")
	tarData := makeTarGz(t, "darwin", "arm64", bin)
	srv, _ := updateServer(t, "v9.9.9", "darwin", "arm64", tarData, shaHex(tarData)+"  agent-env_darwin_arm64.tar.gz\n")

	exe := newExe(t)
	var out bytes.Buffer
	err := Run(Options{
		Current: "v0.0.1",
		BaseURL: srv.URL,
		GOOS:    "darwin",
		GOARCH:  "arm64",
		ExePath: exe,
		Stdout:  &out,
		Getenv:  func(string) string { return "" },
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	got, _ := os.ReadFile(exe)
	if !bytes.Equal(got, bin) {
		t.Fatalf("exe content = %q, want %q", got, bin)
	}
	info, _ := os.Stat(exe)
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("exe mode = %v, want 0755", info.Mode().Perm())
	}
	if !strings.Contains(out.String(), "updated v0.0.1 → v9.9.9") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestRunAlreadyLatest(t *testing.T) {
	srv, count := updateServer(t, "v9.9.9", "darwin", "arm64", nil, "")
	exe := newExe(t)
	before, _ := os.ReadFile(exe)
	var out bytes.Buffer
	err := Run(Options{
		Current: "v9.9.9", Version: "v9.9.9",
		BaseURL: srv.URL, GOOS: "darwin", GOARCH: "arm64",
		ExePath: exe, Stdout: &out, Getenv: func(string) string { return "" },
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.Contains(out.String(), "already up to date (v9.9.9)") {
		t.Fatalf("stdout = %q", out.String())
	}
	if after, _ := os.ReadFile(exe); !bytes.Equal(before, after) {
		t.Fatal("binary must be untouched")
	}
	if got := atomic.LoadInt32(count); got != 0 {
		t.Fatalf("no request expected, got %d", got)
	}
}

func TestRunRejectsTamperedChecksum(t *testing.T) {
	bin := []byte("#!/bin/sh\necho new\n")
	tarData := makeTarGz(t, "darwin", "arm64", bin)
	// Checksum of DIFFERENT content -> tamper.
	srv, _ := updateServer(t, "v9.9.9", "darwin", "arm64", tarData, shaHex([]byte("tampered"))+"  agent-env_darwin_arm64.tar.gz\n")

	exe := newExe(t)
	before, _ := os.ReadFile(exe)
	var out bytes.Buffer
	err := Run(Options{
		Current: "v0.0.1", BaseURL: srv.URL, GOOS: "darwin", GOARCH: "arm64",
		ExePath: exe, Stdout: &out, Getenv: func(string) string { return "" },
	})
	if err == nil || !strings.Contains(err.Error(), "sha256 verification failed") {
		t.Fatalf("want checksum failure, got %v", err)
	}
	if !strings.Contains(err.Error(), "expected:") || !strings.Contains(err.Error(), "actual:") {
		t.Fatalf("error should show expected/actual: %v", err)
	}
	if after, _ := os.ReadFile(exe); !bytes.Equal(before, after) {
		t.Fatal("binary must not be replaced on checksum failure")
	}
}

func TestRunExplicitVersion(t *testing.T) {
	bin := []byte("#!/bin/sh\necho v030\n")
	tarData := makeTarGz(t, "darwin", "arm64", bin)
	srv, count := updateServer(t, "unused", "darwin", "arm64", tarData, shaHex(tarData)+"  agent-env_darwin_arm64.tar.gz\n")

	exe := newExe(t)
	var out bytes.Buffer
	err := Run(Options{
		Current: "v0.0.1", Version: "v0.3.0",
		BaseURL: srv.URL, GOOS: "darwin", GOARCH: "arm64",
		ExePath: exe, Stdout: &out, Getenv: func(string) string { return "" },
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if got := atomic.LoadInt32(count); got == 0 {
		t.Fatal("expected asset requests")
	}
	if !strings.Contains(out.String(), "updated v0.0.1 → v0.3.0") {
		t.Fatalf("stdout = %q", out.String())
	}
	_ = count
}

func TestRunDevBuild(t *testing.T) {
	bin := []byte("#!/bin/sh\necho new\n")
	tarData := makeTarGz(t, "darwin", "arm64", bin)
	srv, _ := updateServer(t, "v9.9.9", "darwin", "arm64", tarData, shaHex(tarData)+"  agent-env_darwin_arm64.tar.gz\n")
	exe := newExe(t)
	var out bytes.Buffer
	err := Run(Options{
		Current: "dev", BaseURL: srv.URL, GOOS: "darwin", GOARCH: "arm64",
		ExePath: exe, Stdout: &out, Getenv: func(string) string { return "" },
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.Contains(out.String(), "dev build") || !strings.Contains(out.String(), "v9.9.9") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestRunInvalidVersionFlag(t *testing.T) {
	exe := newExe(t)
	err := Run(Options{
		Current: "v0.0.1", Version: "not-a-version",
		GOOS: "darwin", GOARCH: "arm64", ExePath: exe, Stdout: &bytes.Buffer{},
		Getenv: func(string) string { return "" },
	})
	if err == nil || !strings.Contains(err.Error(), "--version expects a tag") {
		t.Fatalf("want version validation error, got %v", err)
	}
}

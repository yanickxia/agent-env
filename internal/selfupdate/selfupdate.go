// Package selfupdate implements `agent-env update`: download the release
// tarball for this platform, verify its sha256, and atomically replace the
// running binary.
package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/yanickxia/agent-env/internal/updatecheck"
	"github.com/yanickxia/agent-env/internal/version"
)

// Options configures Run.
type Options struct {
	Current string
	BaseURL string
	Version string // explicit target tag, or "" for latest
	GOOS    string
	GOARCH  string
	ExePath string // defaults to os.Executable()
	Client  *http.Client
	Stdout  io.Writer
	Getenv  func(string) string
}

// Run performs the update. A nil error means success (including "already up to
// date").
func Run(opts Options) error {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: 60 * time.Second}
	}
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if opts.GOARCH == "" {
		opts.GOARCH = runtime.GOARCH
	}
	if opts.BaseURL == "" {
		opts.BaseURL = updatecheck.BaseURL(opts.Getenv)
	}
	if opts.ExePath == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("cannot resolve the running executable: %v", err)
		}
		opts.ExePath = exe
	}

	// Platform support mirrors install.zsh.
	if opts.GOOS != "darwin" && opts.GOOS != "linux" {
		return fmt.Errorf("unsupported operating system: %s (supported: darwin, linux)", opts.GOOS)
	}
	if opts.GOARCH != "amd64" && opts.GOARCH != "arm64" {
		return fmt.Errorf("unsupported architecture: %s (supported: amd64, arm64)", opts.GOARCH)
	}

	target := strings.TrimSpace(opts.Version)
	if target != "" {
		if !version.IsValid(target) {
			return fmt.Errorf("--version expects a tag like v0.3.0, got %q", target)
		}
		target = version.Canonical(target)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tag, err := updatecheck.Latest(ctx, opts.BaseURL, opts.GOOS, opts.GOARCH, updatecheck.NoRedirectClient(5*time.Second))
		if err != nil {
			return fmt.Errorf("cannot determine the latest version: %v", err)
		}
		target = tag
	}

	current := version.Canonical(opts.Current)
	if version.IsValid(current) {
		if c, ok := version.Compare(target, current); ok && c == 0 {
			fmt.Fprintf(opts.Stdout, "already up to date (%s)\n", target)
			return nil
		}
	}

	asset := updatecheck.AssetName(opts.GOOS, opts.GOARCH)
	base := strings.TrimRight(opts.BaseURL, "/")
	tarURL := fmt.Sprintf("%s/releases/download/%s/%s", base, target, asset)
	sumURL := tarURL + ".sha256"

	tmpDir, err := os.MkdirTemp("", "agent-env-update.")
	if err != nil {
		return fmt.Errorf("cannot create a temporary directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tarPath := filepath.Join(tmpDir, asset)
	if err := download(opts.Client, tarURL, tarPath); err != nil {
		return fmt.Errorf("download failed: %v", err)
	}
	sumPath := filepath.Join(tmpDir, asset+".sha256")
	if err := download(opts.Client, sumURL, sumPath); err != nil {
		return fmt.Errorf("checksum download failed: %v", err)
	}

	if err := verifySHA256(tarPath, sumPath, asset); err != nil {
		return err
	}

	bin, err := extractBinary(tarPath)
	if err != nil {
		return err
	}

	if err := replaceSelf(opts.ExePath, bin); err != nil {
		return err
	}

	if version.IsValid(opts.Current) {
		fmt.Fprintf(opts.Stdout, "updated %s → %s\n", current, target)
	} else {
		fmt.Fprintf(opts.Stdout, "updated %s (dev build) → %s\n", opts.Current, target)
	}
	return nil
}

func download(client *http.Client, url, dest string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return f.Sync()
}

func verifySHA256(file, sumFile, asset string) error {
	sumData, err := os.ReadFile(sumFile)
	if err != nil {
		return fmt.Errorf("cannot read checksum file: %v", err)
	}
	fields := strings.Fields(string(sumData))
	if len(fields) == 0 {
		return fmt.Errorf("checksum file for %s is empty", asset)
	}
	expected := strings.ToLower(fields[0])

	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("cannot read %s: %v", asset, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("cannot hash %s: %v", asset, err)
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if expected != actual {
		return fmt.Errorf("sha256 verification failed for %s\n  expected: %s\n  actual:   %s\nRefusing to install a corrupted or tampered download.", asset, expected, actual)
	}
	return nil
}

func extractBinary(tarPath string) ([]byte, error) {
	f, err := os.Open(tarPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open archive: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("not a gzip archive: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read archive: %v", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(hdr.Name) == "agent-env" {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("agent-env binary not found in the archive")
}

// replaceSelf atomically replaces the running binary. On Unix, rename over a
// running executable is allowed.
func replaceSelf(exe string, bin []byte) error {
	target := exe
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		target = resolved
	}
	dir := filepath.Dir(target)

	tmp, err := os.CreateTemp(dir, ".agent-env.update.*")
	if err != nil {
		return replaceError(target, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return replaceError(target, err)
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return replaceError(target, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return replaceError(target, err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return replaceError(target, err)
	}
	return nil
}

func replaceError(target string, err error) error {
	return fmt.Errorf("cannot replace %s: %v\nthe install directory may not be writable; re-run the installer instead: zsh install.zsh install", target, err)
}

package updatecheck

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func latestServer(t *testing.T, tag string, status int, delay time.Duration) (*httptest.Server, *int32) {
	t.Helper()
	var count int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		if delay > 0 {
			time.Sleep(delay)
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Location", srv.URL+"/releases/download/"+tag+"/"+AssetName("darwin", "arm64"))
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	return srv, &count
}

func baseOptions(t *testing.T, srv *httptest.Server) Options {
	t.Helper()
	return Options{
		Current:   "v0.3.0",
		BaseURL:   srv.URL,
		CachePath: filepath.Join(t.TempDir(), "update-check.json"),
		GOOS:      "darwin",
		GOARCH:    "arm64",
		Getenv:    func(string) string { return "" },
	}
}

func TestCheckNewerNotifies(t *testing.T) {
	srv, count := latestServer(t, "v9.9.9", http.StatusOK, 0)
	msg := Check(baseOptions(t, srv))
	if msg == "" || !strings.Contains(msg, "v9.9.9 is available") || !strings.Contains(msg, "current: v0.3.0") || !strings.Contains(msg, "agent-env update") {
		t.Fatalf("unexpected message: %q", msg)
	}
	if got := atomic.LoadInt32(count); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func TestCheckSameVersionSilentAndCached(t *testing.T) {
	srv, count := latestServer(t, "v0.3.0", http.StatusOK, 0)
	opts := baseOptions(t, srv)
	if msg := Check(opts); msg != "" {
		t.Fatalf("same version must be silent, got %q", msg)
	}
	if _, err := os.Stat(opts.CachePath); err != nil {
		t.Fatalf("cache should be written: %v", err)
	}
	if got := atomic.LoadInt32(count); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func TestCheckServerErrorSilent(t *testing.T) {
	srv, _ := latestServer(t, "", http.StatusInternalServerError, 0)
	if msg := Check(baseOptions(t, srv)); msg != "" {
		t.Fatalf("500 must be silent, got %q", msg)
	}
}

func TestCheckTimeoutSilent(t *testing.T) {
	srv, _ := latestServer(t, "v9.9.9", http.StatusOK, 400*time.Millisecond)
	opts := baseOptions(t, srv)
	opts.Client = NoRedirectClient(80 * time.Millisecond)
	if msg := Check(opts); msg != "" {
		t.Fatalf("timeout must be silent, got %q", msg)
	}
}

func TestCheckCacheTTLAndNotifiedSuppression(t *testing.T) {
	srv, count := latestServer(t, "v9.9.9", http.StatusOK, 0)
	opts := baseOptions(t, srv)
	now := time.Now()
	opts.Now = func() time.Time { return now }

	if msg := Check(opts); msg == "" {
		t.Fatal("first check should notify")
	}
	// Within TTL: no new request, and notification suppressed (notified_at fresh).
	if msg := Check(opts); msg != "" {
		t.Fatalf("second check should be suppressed, got %q", msg)
	}
	if got := atomic.LoadInt32(count); got != 1 {
		t.Fatalf("cached TTL must avoid a second request, requests=%d", got)
	}

	// After TTL, notified_at is also stale -> probe again and notify again.
	now = now.Add(25 * time.Hour)
	if msg := Check(opts); msg == "" {
		t.Fatal("after TTL the notice should reappear")
	}
	if got := atomic.LoadInt32(count); got != 2 {
		t.Fatalf("after TTL a new request is expected, requests=%d", got)
	}
}

func TestCheckNotifiedSuppressedEvenAfterTTLProbe(t *testing.T) {
	// Simulate: checked_at stale (forces a probe) but notified_at fresh and the
	// latest did not change -> suppress.
	srv, _ := latestServer(t, "v9.9.9", http.StatusOK, 0)
	opts := baseOptions(t, srv)
	now := time.Now()
	opts.Now = func() time.Time { return now }
	saveCache(opts.CachePath, Cache{
		Latest:     "v9.9.9",
		CheckedAt:  now.Add(-30 * time.Hour),
		NotifiedAt: now.Add(-1 * time.Hour),
	})
	if msg := Check(opts); msg != "" {
		t.Fatalf("fresh notified_at with same latest must suppress, got %q", msg)
	}
}

func TestCheckSkipsDevAndEnv(t *testing.T) {
	srv, count := latestServer(t, "v9.9.9", http.StatusOK, 0)

	devOpts := baseOptions(t, srv)
	devOpts.Current = "dev"
	if msg := Check(devOpts); msg != "" {
		t.Fatalf("dev must skip, got %q", msg)
	}

	envOpts := baseOptions(t, srv)
	envOpts.Getenv = func(k string) string {
		if k == "AGENT_ENV_NO_UPDATE_CHECK" {
			return "1"
		}
		return ""
	}
	if msg := Check(envOpts); msg != "" {
		t.Fatalf("NO_UPDATE_CHECK must skip, got %q", msg)
	}
	if got := atomic.LoadInt32(count); got != 0 {
		t.Fatalf("no requests expected for skipped checks, got %d", got)
	}

	skipOpts := baseOptions(t, srv)
	skipOpts.Skip = true
	if msg := Check(skipOpts); msg != "" {
		t.Fatalf("Skip must skip, got %q", msg)
	}
}

func TestTagFromLocation(t *testing.T) {
	cases := map[string]string{
		"https://github.com/yanickxia/agent-env/releases/download/v0.3.0/agent-env_darwin_arm64.tar.gz": "v0.3.0",
		"/yanickxia/agent-env/releases/download/v1.2.3/agent-env_linux_amd64.tar.gz":                    "v1.2.3",
		"https://example.test/releases/download/v9.9.9/asset.tar.gz?x=1":                                "v9.9.9",
		"https://example.test/releases/latest/download/agent-env.tar.gz":                                "",
		"https://example.test/other/v0.1.0/x":                                                           "",
	}
	for loc, want := range cases {
		if got := TagFromLocation(loc); got != want {
			t.Fatalf("TagFromLocation(%q) = %q want %q", loc, got, want)
		}
	}
}

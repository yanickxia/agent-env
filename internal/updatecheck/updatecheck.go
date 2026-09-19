// Package updatecheck performs a best-effort, asynchronous check for a newer
// agent-env release. It never calls the GitHub API: it issues a HEAD request to
// the "latest/download" asset and reads the version out of the 302 Location.
package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/yanickxia/agent-env/internal/version"
)

// DefaultBase is the canonical release base URL.
const DefaultBase = "https://github.com/yanickxia/agent-env"

// CacheTTL is how long a checked version and a notification stay valid.
const CacheTTL = 24 * time.Hour

// AssetName returns the release asset file name for a platform.
func AssetName(goos, goarch string) string {
	return fmt.Sprintf("agent-env_%s_%s.tar.gz", goos, goarch)
}

// BaseURL resolves the release base URL (AGENT_ENV_RELEASE_BASE overrides).
func BaseURL(getenv func(string) string) string {
	if v := strings.TrimSpace(getenv("AGENT_ENV_RELEASE_BASE")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return DefaultBase
}

// DefaultCachePath is $XDG_CACHE_HOME/agent-env/update-check.json, falling back
// to $HOME/.cache/agent-env/update-check.json.
func DefaultCachePath(getenv func(string) string) string {
	base := getenv("XDG_CACHE_HOME")
	if base == "" {
		home := getenv("HOME")
		if home == "" {
			if h, err := os.UserHomeDir(); err == nil {
				home = h
			}
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "agent-env", "update-check.json")
}

// NoRedirectClient returns an HTTP client that does not follow redirects, so
// the "latest" redirect can be inspected directly.
func NoRedirectClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Latest resolves the newest release tag via the stable "latest" redirect.
func Latest(ctx context.Context, base, goos, goarch string, client *http.Client) (string, error) {
	if client == nil {
		client = NoRedirectClient(2 * time.Second)
	}
	url := strings.TrimRight(base, "/") + "/releases/latest/download/" + AssetName(goos, goarch)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("no redirect location for %s", url)
	}
	tag := TagFromLocation(loc)
	if tag == "" {
		return "", fmt.Errorf("could not parse a version out of %q", loc)
	}
	return tag, nil
}

// TagFromLocation extracts vX.Y.Z from a release asset URL of the form
// .../releases/download/vX.Y.Z/agent-env_....tar.gz (absolute or relative).
func TagFromLocation(loc string) string {
	const marker = "/releases/download/"
	i := strings.Index(loc, marker)
	if i < 0 {
		return ""
	}
	rest := loc[i+len(marker):]
	if j := strings.IndexByte(rest, '/'); j >= 0 {
		rest = rest[:j]
	}
	if k := strings.IndexAny(rest, "?#"); k >= 0 {
		rest = rest[:k]
	}
	if version.IsValid(rest) {
		return rest
	}
	return ""
}

// Cache is the on-disk update-check state.
type Cache struct {
	Latest     string    `json:"latest"`
	CheckedAt  time.Time `json:"checked_at"`
	NotifiedAt time.Time `json:"notified_at"`
}

// Options configures Check.
type Options struct {
	Current   string
	BaseURL   string
	CachePath string
	GOOS      string
	GOARCH    string
	Now       func() time.Time
	Client    *http.Client
	Getenv    func(string) string
	// Skip disables the check entirely (e.g. for the update command).
	Skip bool
}

// Check returns the one-line notice when a newer release is available and it
// has not been announced recently; otherwise it returns "". All failures are
// silent (best effort).
func Check(opts Options) string {
	if opts.Skip {
		return ""
	}
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if opts.GOARCH == "" {
		opts.GOARCH = runtime.GOARCH
	}
	if opts.BaseURL == "" {
		opts.BaseURL = BaseURL(opts.Getenv)
	}
	if opts.CachePath == "" {
		opts.CachePath = DefaultCachePath(opts.Getenv)
	}
	if opts.Client == nil {
		opts.Client = NoRedirectClient(2 * time.Second)
	}

	// dev / non-semver builds never participate.
	if !version.IsValid(opts.Current) {
		return ""
	}
	if truthy(opts.Getenv("AGENT_ENV_NO_UPDATE_CHECK")) {
		return ""
	}

	now := opts.Now()
	cache := loadCache(opts.CachePath)

	latest := ""
	if version.IsValid(cache.Latest) && !cache.CheckedAt.IsZero() && now.Sub(cache.CheckedAt) < CacheTTL {
		latest = cache.Latest
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		tag, err := Latest(ctx, opts.BaseURL, opts.GOOS, opts.GOARCH, opts.Client)
		if err != nil {
			return ""
		}
		latest = tag
		cache.Latest = tag
		cache.CheckedAt = now
	}

	if !version.IsValid(latest) {
		return ""
	}
	if c, ok := version.Compare(latest, opts.Current); !ok || c <= 0 {
		saveCache(opts.CachePath, cache)
		return ""
	}

	suppressed := !cache.NotifiedAt.IsZero() && now.Sub(cache.NotifiedAt) < CacheTTL && cache.Latest == latest
	if !suppressed {
		cache.NotifiedAt = now
	}
	saveCache(opts.CachePath, cache)
	if suppressed {
		return ""
	}
	return fmt.Sprintf("agent-env: %s is available (current: %s); run 'agent-env update' to upgrade",
		latest, version.Canonical(opts.Current))
}

// Start runs Check in the background and delivers at most one message.
func Start(opts Options) <-chan string {
	ch := make(chan string, 1)
	go func() { ch <- Check(opts) }()
	return ch
}

func truthy(v string) bool {
	v = strings.TrimSpace(v)
	return v == "1" || strings.EqualFold(v, "true")
}

func loadCache(path string) Cache {
	var c Cache
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(data, &c)
	return c
}

func saveCache(path string, c Cache) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

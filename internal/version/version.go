// Package version holds the agent-env version string.
//
// It is a package-level variable so release builds can inject the tag via
// -ldflags, e.g.:
//
//	go build -ldflags "-X github.com/yanickxia/agent-env/internal/version.Version=v1.2.3" ./cmd/agent-env
package version

// Version is the agent-env version. It defaults to a development version and
// is overridden at build time for tagged releases.
var Version = "0.1.0"

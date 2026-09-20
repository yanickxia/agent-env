package mcp

import (
	"strings"

	"github.com/yanickxia/agent-env/internal/config"
)

// pi and omp (oh-my-pi) share Claude's MCP config shape: a top-level
// "mcpServers" object of Claude-compatible server entries. They are written as
// plain JSON, so instead of a marker block the writer replaces the whole
// top-level "mcpServers" member and leaves every other member untouched.

// renderPiOMP renders the mcpServers object shared by pi and omp. It is the
// exact Claude renderer; label only affects diagnostics.
func renderPiOMP(entries []config.Server, resolve config.SecretResolver, warn func(string), label string) (string, error) {
	return renderClaudeCompat(entries, resolve, warn, label)
}

// patchMCPJSON replaces the top-level "mcpServers" member of a JSON document.
// Missing or empty input is treated as an empty object (equivalent to
// {"mcpServers": {}} once replaced). All other top-level members are preserved
// byte-for-byte, and the result always ends with a newline.
func patchMCPJSON(raw []byte, rendered string) []byte {
	if strings.TrimSpace(string(raw)) == "" {
		// "{\n}" gives patchClaudeJSON a canonical two-space base so a
		// freshly created file gets clean indentation.
		raw = []byte("{\n}")
	}
	return patchClaudeJSON(raw, rendered)
}

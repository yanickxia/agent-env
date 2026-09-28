// Package inventory implements the read-only `agent-env ls` view: what is
// actually installed on disk (skills directories, MCP config entries) versus
// what the unified config + repo selection declare. It mirrors the gating of
// skills/mcp apply so the two views stay consistent.
package inventory

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Providers supported by ls. The canonical names match the agents used in the
// unified config; "claude-code" (not the historical "claude") is the ls name.
var providerNames = []string{"codex", "claude-code", "opencode"}

// ValidProvider reports whether name is a known ls provider.
func ValidProvider(name string) bool {
	switch name {
	case "codex", "claude-code", "opencode", "claude":
		return true
	}
	return false
}

// CanonicalProvider maps the accepted "claude" alias to "claude-code".
func CanonicalProvider(name string) string {
	if name == "claude" {
		return "claude-code"
	}
	return name
}

// ProviderNames returns the canonical provider list in display order.
func ProviderNames() []string {
	return append([]string(nil), providerNames...)
}

// skillDir returns the on-disk skills directory of provider for one scope:
// global lives under the provider's home config, repo-level under root.
func skillDir(home, root string, provider string, global bool) string {
	if global {
		switch provider {
		case "codex":
			return filepath.Join(home, ".codex", "skills")
		case "claude-code":
			return filepath.Join(home, ".claude", "skills")
		case "opencode":
			return filepath.Join(home, ".config", "opencode", "skills")
		}
		return ""
	}
	switch provider {
	case "codex", "opencode":
		return filepath.Join(root, ".agents", "skills")
	case "claude-code":
		return filepath.Join(root, ".claude", "skills")
	}
	return ""
}

// listSkillsDir returns the sorted names of installed skills in dir: entries
// that are directories containing a SKILL.md (following symlinks, so store
// symlink layouts and per-agent links are counted as installed).
func listSkillsDir(dir string) []string {
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := []string{}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		skill := filepath.Join(dir, name, "SKILL.md")
		if fi, err := os.Stat(skill); err == nil && !fi.IsDir() {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// --- MCP config parsing -----------------------------------------------------

// Managed-block markers. These must stay in sync with internal/mcp/transform.go.
const (
	codexBeginMarker    = "AGENT_MCP_CODEX_MANAGED_BEGIN"
	opencodeBeginMarker = "// AGENT_MCP_OPENCODE_MANAGED_BEGIN"
	opencodeEndMarker   = "// AGENT_MCP_OPENCODE_MANAGED_END"
)

// codexTablePrefix is the table prefix of the agent-env managed MCP servers
// section in codex config.toml. It must stay in sync with
// internal/mcp/render.go (renderCodex).
const codexTablePrefix = "[mcp_servers."

// mcpConfigPaths returns the MCP config files of provider for one scope,
// first existing wins. claude-code project scope reads .mcp.json, the claude
// CLI's project target (verified: `claude mcp add --scope project` writes
// <repo>/.mcp.json).
func mcpConfigPaths(home, root string, provider string, global bool) []string {
	if global {
		switch provider {
		case "codex":
			return []string{filepath.Join(home, ".codex", "config.toml")}
		case "claude-code":
			return []string{filepath.Join(home, ".claude.json")}
		case "opencode":
			return []string{filepath.Join(home, ".config", "opencode", "opencode.jsonc")}
		}
		return nil
	}
	switch provider {
	case "codex":
		return []string{filepath.Join(root, ".codex", "config.toml")}
	case "claude-code":
		return []string{filepath.Join(root, ".mcp.json")}
	case "opencode":
		return []string{filepath.Join(root, ".opencode", "opencode.jsonc")}
	}
	return nil
}

// mcpKeyOf returns the MCP server names registered in the provider's config.
// For codex/opencode only the agent-env managed marker block is counted: ls
// shows what agent-env would manage, not the whole hand-edited file.
// claude-code has no markers: every top-level mcpServers key counts (user
// scope: ~/.claude.json, whose mcpServers agent-env fully owns; project
// scope: .mcp.json, fully claude-CLI-owned).
func mcpKeyOf(home, root, provider string, global bool) ([]string, string) {
	paths := mcpConfigPaths(home, root, provider, global)
	path := ""
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			path = p
			break
		}
	}
	if path == "" {
		return nil, ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, path
	}
	text := string(data)

	var names []string
	switch provider {
	case "codex":
		names = codexManagedTables(text)
	case "opencode":
		names = opencodeManagedKeys(text)
	case "claude-code":
		names = topLevelObjectKeysInMember(text, "mcpServers")
	}
	sort.Strings(names)
	return names, path
}

// codexManagedTables scans a codex config.toml for [mcp_servers.<name>]
// table headers inside the agent-env managed marker block, so hand-written
// tables outside the block are not counted.
func codexManagedTables(text string) []string {
	inBlock := false
	out := []string{}
	for line := range lines(text) {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, codexBeginMarker) {
			inBlock = true
			continue
		}
		if strings.Contains(trimmed, "AGENT_MCP_CODEX_MANAGED_END") {
			inBlock = false
			continue
		}
		if !inBlock || !strings.HasPrefix(trimmed, codexTablePrefix) {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(trimmed, codexTablePrefix), "]")
		name = strings.Trim(name, "\"'")
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// opencodeManagedKeys returns the keys of the "mcp" object inside the
// agent-env managed block of an opencode.jsonc. The block holds the bare
// member (no enclosing braces), so it is wrapped in braces before the member
// scan.
func opencodeManagedKeys(text string) []string {
	b := strings.Index(text, opencodeBeginMarker)
	if b < 0 {
		return nil
	}
	e := strings.Index(text, opencodeEndMarker)
	if e < 0 || e < b {
		return nil
	}
	block := stripJSONCComments(text[b+len(opencodeBeginMarker) : e])
	wrapped := "{\n" + block + "\n}"
	valStart, valEnd, _, found := findTopLevelValueOf(wrapped, "mcp")
	if !found {
		return nil
	}
	return objectKeys(wrapped[valStart:valEnd])
}

// topLevelObjectKeysInMember returns the keys of the top-level member "key"
// of a JSON/JSONC document (comments tolerated), e.g. mcpServers or mcp.
func topLevelObjectKeysInMember(text, key string) []string {
	text = stripJSONCComments(text)
	valStart, valEnd, _, found := findTopLevelValueOf(text, key)
	if !found {
		return nil
	}
	return objectKeys(text[valStart:valEnd])
}

// findTopLevelValueOf locates the value span of a top-level member of the
// first JSON object in text. It mirrors internal/mcp.findTopLevelValue.
func findTopLevelValueOf(text, key string) (valStart, valEnd, keyIndent int, found bool) {
	i := 0
	for i < len(text) && isSpaceByte(text[i]) {
		i++
	}
	if i >= len(text) || text[i] != '{' {
		return 0, 0, 0, false
	}
	i++
	for i < len(text) {
		for i < len(text) && isSpaceByte(text[i]) {
			i++
		}
		if i >= len(text) || text[i] == '}' {
			return 0, 0, 0, false
		}
		if text[i] != '"' {
			return 0, 0, 0, false
		}
		keyStart := i
		end := scanStringEndOf(text, i)
		if end < 0 {
			return 0, 0, 0, false
		}
		k, _ := unquoteJSONOf(text[keyStart:end])
		i = end
		for i < len(text) && isSpaceByte(text[i]) {
			i++
		}
		if i >= len(text) || text[i] != ':' {
			return 0, 0, 0, false
		}
		i++
		for i < len(text) && isSpaceByte(text[i]) {
			i++
		}
		vStart := i
		vEnd := scanValueEndOf(text, i)
		if vEnd < 0 {
			return 0, 0, 0, false
		}
		if k == key {
			return vStart, vEnd, lineIndentOf(text, keyStart), true
		}
		i = vEnd
		for i < len(text) && isSpaceByte(text[i]) {
			i++
		}
		if i < len(text) && text[i] == ',' {
			i++
			continue
		}
		return 0, 0, 0, false
	}
	return 0, 0, 0, false
}

// objectKeys returns the key literals of a JSON object text (from '{' to '}').
func objectKeys(text string) []string {
	i := 0
	for i < len(text) && isSpaceByte(text[i]) {
		i++
	}
	if i >= len(text) || text[i] != '{' {
		return nil
	}
	i++
	out := []string{}
	for i < len(text) {
		for i < len(text) && isSpaceByte(text[i]) {
			i++
		}
		if i >= len(text) || text[i] == '}' {
			return out
		}
		if text[i] != '"' {
			return out
		}
		keyStart := i
		end := scanStringEndOf(text, i)
		if end < 0 {
			return out
		}
		k, ok := unquoteJSONOf(text[keyStart:end])
		if ok {
			out = append(out, k)
		}
		i = end
		for i < len(text) && isSpaceByte(text[i]) {
			i++
		}
		if i < len(text) && text[i] == ':' {
			i++
		}
		for i < len(text) && isSpaceByte(text[i]) {
			i++
		}
		vEnd := scanValueEndOf(text, i)
		if vEnd < 0 {
			return out
		}
		i = vEnd
		for i < len(text) && isSpaceByte(text[i]) {
			i++
		}
		if i < len(text) && text[i] == ',' {
			i++
		}
	}
	return out
}

// stripJSONCComments removes // line comments outside string literals.
func stripJSONCComments(text string) string {
	var b strings.Builder
	for line := range lines(text) {
		b.WriteString(stripLineComment(line))
		b.WriteByte('\n')
	}
	return b.String()
}

// stripLineComment removes a trailing // comment (outside strings) from one
// line.
func stripLineComment(line string) string {
	inString := false
	escape := false
	for i := range len(line) {
		c := line[i]
		if inString {
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
		} else if c == '/' && i+1 < len(line) && line[i+1] == '/' {
			return strings.TrimRight(line[:i], " \t")
		}
	}
	return line
}

// lines iterates the lines of text (final line included even without a
// trailing newline).
func lines(text string) func(func(string) bool) {
	return func(yield func(string) bool) {
		for len(text) > 0 {
			idx := strings.IndexByte(text, '\n')
			var line string
			if idx < 0 {
				line, text = text, ""
			} else {
				line, text = text[:idx], text[idx+1:]
			}
			if !yield(line) {
				return
			}
		}
	}
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\v' || b == '\f'
}

func lineIndentOf(text string, pos int) int {
	lineStart := strings.LastIndexByte(text[:pos], '\n') + 1
	return pos - lineStart
}

func scanStringEndOf(text string, start int) int {
	i := start + 1
	for i < len(text) {
		switch text[i] {
		case '\\':
			i += 2
			continue
		case '"':
			return i + 1
		}
		i++
	}
	return -1
}

// scanValueEndOf mirrors internal/mcp.scanValueEnd.
func scanValueEndOf(text string, start int) int {
	if start >= len(text) {
		return -1
	}
	switch text[start] {
	case '"':
		return scanStringEndOf(text, start)
	case '{', '[':
		depth := 0
		i := start
		for i < len(text) {
			switch text[i] {
			case '"':
				e := scanStringEndOf(text, i)
				if e < 0 {
					return -1
				}
				i = e
				continue
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1
				}
			}
			i++
		}
		return -1
	default:
		i := start
		for i < len(text) {
			c := text[i]
			if c == ',' || c == '}' || c == ']' || c == '\n' || c == ' ' || c == '\t' || c == '\r' {
				break
			}
			i++
		}
		return i
	}
}

// unquoteJSONOf mirrors internal/mcp.unquoteJSON. The escape sequences index
// i explicitly because escapes consume a variable number of bytes.
func unquoteJSONOf(lit string) (string, bool) {
	if len(lit) < 2 || lit[0] != '"' || lit[len(lit)-1] != '"' {
		return "", false
	}
	body := lit[1 : len(lit)-1]
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(body) {
			return "", false
		}
		switch body[i] {
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case '/':
			b.WriteByte('/')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'u':
			if i+4 >= len(body) {
				return "", false
			}
			r, ok := hex4(body[i+1 : i+5])
			if !ok {
				return "", false
			}
			i += 4
			if r >= 0xD800 && r <= 0xDBFF && i+6 < len(body) && body[i+1] == '\\' && body[i+2] == 'u' {
				lo, ok := hex4(body[i+3 : i+7])
				if ok && lo >= 0xDC00 && lo <= 0xDFFF {
					r = 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00)
					i += 6
				}
			}
			b.WriteRune(rune(r))
		default:
			return "", false
		}
	}
	return b.String(), true
}

func hex4(s string) (int, bool) {
	r := 0
	for _, d := range []byte(s) {
		r <<= 4
		switch {
		case d >= '0' && d <= '9':
			r |= int(d - '0')
		case d >= 'a' && d <= 'f':
			r |= int(d-'a') + 10
		case d >= 'A' && d <= 'F':
			r |= int(d-'A') + 10
		default:
			return 0, false
		}
	}
	return r, true
}

// providerLabel returns the human label of a provider used in section headers.
func providerLabel(provider string) string {
	switch provider {
	case "codex":
		return "Codex"
	case "claude-code":
		return "Claude Code"
	case "opencode":
		return "OpenCode"
	}
	return provider
}

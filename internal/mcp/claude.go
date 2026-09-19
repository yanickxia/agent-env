package mcp

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/yanickxia/agent-env/internal/config"
)

// renderClaudeUser renders the ~/.claude.json top-level mcpServers object from
// the user-scope claude servers, sorted by name, matching the zsh renderer.
func renderClaudeUser(entries []config.Server, resolve config.SecretResolver, warn func(string)) (string, error) {
	seen := map[string]bool{}
	names := []string{}
	byName := map[string]config.Server{}
	for _, e := range entries {
		n := strings.TrimSpace(e.Name)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		names = append(names, n)
		byName[n] = e
	}
	sort.Strings(names)

	result := newObj()
	for _, name := range names {
		e := byName[name]
		t := strings.TrimSpace(e.Type)
		if t == "" {
			t = "stdio"
		}
		if t == "streamable-http" {
			t = "http"
		}

		obj := newObj()
		switch t {
		case "stdio":
			cmd := strings.TrimSpace(e.Command)
			if cmd == "" {
				return "", fmt.Errorf("%s: claude server '%s' requires a command-based MCP entry", config.Prog, name)
			}
			obj.set("type", "stdio")
			obj.set("command", cmd)
			args := make([]any, len(e.Args))
			for i, a := range e.Args {
				args[i] = a
			}
			obj.set("args", args)
			env := newObj()
			for _, kv := range e.Env {
				env.set(kv.Key, kv.Value)
			}
			obj.set("env", env)
		case "http", "sse":
			url := strings.TrimSpace(e.URL)
			if url == "" {
				return "", fmt.Errorf("%s: claude server '%s' requires a url for %s transport", config.Prog, name, t)
			}
			obj.set("type", t)
			obj.set("url", url)
			bt := strings.TrimSpace(e.BearerTokenEnvVar)
			if bt != "" {
				token := resolve(bt)
				if token == "" {
					warn(fmt.Sprintf("%s: warning: token for '%s' is not set or empty (checked secrets.toml and environment)", config.Prog, bt))
				}
				h := newObj()
				h.set("Authorization", "Bearer "+token)
				obj.set("headers", h)
			} else if len(e.Headers) > 0 {
				h := newObj()
				for _, kv := range e.Headers {
					h.set(kv.Key, kv.Value)
				}
				obj.set("headers", h)
			}
		default:
			return "", fmt.Errorf("%s: unsupported server type '%s' for claude server '%s'", config.Prog, t, name)
		}
		result.set(name, obj)
	}
	return marshalPy(result, 0, false), nil
}

// patchClaudeJSON replaces only the top-level "mcpServers" value in the raw
// ~/.claude.json text, leaving every other byte untouched. Go maps would
// scramble the 50+ runtime keys, so this is a text-level splice.
func patchClaudeJSON(raw []byte, rendered string) []byte {
	text := string(raw)
	valStart, valEnd, keyIndent, found := findTopLevelValue(text, "mcpServers")

	var out string
	if found {
		out = text[:valStart] + marshalIndentedRendered(rendered, keyIndent) + text[valEnd:]
	} else {
		out = insertTopLevelMember(text, "mcpServers", marshalIndentedRendered(rendered, 2))
	}
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return []byte(out)
}

// marshalIndentedRendered re-indents an already-rendered JSON value so it nests
// correctly at the given base indentation. The rendered text starts at column 0
// with an opening brace; we shift every line after the first.
func marshalIndentedRendered(rendered string, indent int) string {
	if indent == 0 {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	prefix := strings.Repeat(" ", indent)
	for i := 1; i < len(lines); i++ {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

// findTopLevelValue locates the value span of a top-level object member and the
// indentation of its key line.
func findTopLevelValue(text, key string) (valStart, valEnd, keyIndent int, found bool) {
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
		end := scanStringEnd(text, i)
		if end < 0 {
			return 0, 0, 0, false
		}
		k, _ := unquoteJSON(text[keyStart:end])
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
		vEnd := scanValueEnd(text, i)
		if vEnd < 0 {
			return 0, 0, 0, false
		}
		if k == key {
			return vStart, vEnd, lineIndent(text, keyStart), true
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

func lineIndent(text string, pos int) int {
	lineStart := strings.LastIndexByte(text[:pos], '\n') + 1
	return pos - lineStart
}

func scanStringEnd(text string, start int) int {
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

func scanValueEnd(text string, start int) int {
	if start >= len(text) {
		return -1
	}
	switch text[start] {
	case '"':
		return scanStringEnd(text, start)
	case '{', '[':
		depth := 0
		i := start
		for i < len(text) {
			switch text[i] {
			case '"':
				e := scanStringEnd(text, i)
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

func insertTopLevelMember(text, key, value string) string {
	rootClose := strings.LastIndexByte(text, '}')
	if rootClose < 0 {
		return "{\n  " + pyJSONString(key, false) + ": " + value + "\n}\n"
	}
	prefix := text[:rootClose]
	stripped := pyRstrip(prefix)
	suffix := text[rootClose:]

	indent := 2
	if first := firstTopLevelIndent(text); first > 0 {
		indent = first
	}
	member := strings.Repeat(" ", indent) + pyJSONString(key, false) + ": " + value
	if strings.HasSuffix(stripped, "{") {
		return stripped + "\n" + member + "\n" + suffix
	}
	return stripped + ",\n" + member + "\n" + suffix
}

func firstTopLevelIndent(text string) int {
	i := 0
	for i < len(text) && isSpaceByte(text[i]) {
		i++
	}
	if i >= len(text) || text[i] != '{' {
		return 0
	}
	i++
	for i < len(text) && isSpaceByte(text[i]) {
		i++
	}
	return lineIndent(text, i)
}

// unquoteJSON decodes a JSON string literal without depending on map ordering.
func unquoteJSON(lit string) (string, bool) {
	if len(lit) < 2 || lit[0] != '"' || lit[len(lit)-1] != '"' {
		return "", false
	}
	var b strings.Builder
	for i := 1; i < len(lit)-1; i++ {
		c := lit[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(lit)-1 {
			break
		}
		switch lit[i] {
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
			if i+4 < len(lit) {
				var r rune
				fmt.Sscanf(lit[i+1:i+5], "%04x", &r)
				b.WriteRune(r)
				i += 4
			}
		default:
			b.WriteByte(lit[i])
		}
	}
	return b.String(), true
}

// readFileOrEmpty returns file contents or "" when absent.
func readFileOrEmpty(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// scanTopLevelKeyCount counts the members of a top-level JSON object.
func scanTopLevelKeyCount(text string) int {
	i := 0
	for i < len(text) && isSpaceByte(text[i]) {
		i++
	}
	if i >= len(text) || text[i] != '{' {
		return 0
	}
	i++
	count := 0
	for {
		for i < len(text) && isSpaceByte(text[i]) {
			i++
		}
		if i >= len(text) || text[i] == '}' {
			return count
		}
		if text[i] != '"' {
			return count
		}
		end := scanStringEnd(text, i)
		if end < 0 {
			return count
		}
		i = end
		for i < len(text) && isSpaceByte(text[i]) {
			i++
		}
		if i >= len(text) || text[i] != ':' {
			return count
		}
		i++
		for i < len(text) && isSpaceByte(text[i]) {
			i++
		}
		vEnd := scanValueEnd(text, i)
		if vEnd < 0 {
			return count
		}
		i = vEnd
		count++
		for i < len(text) && isSpaceByte(text[i]) {
			i++
		}
		if i < len(text) && text[i] == ',' {
			i++
			continue
		}
		return count
	}
}

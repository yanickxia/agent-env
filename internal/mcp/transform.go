package mcp

import (
	"regexp"
	"strings"
	"unicode"
)

// Marker strings. These must stay byte-identical to the retired zsh
// agent-mcp-sync implementation; the upsert logic keys off them.
const (
	codexBegin    = "# AGENT_MCP_CODEX_MANAGED_BEGIN"
	codexEnd      = "# AGENT_MCP_CODEX_MANAGED_END"
	traeBegin     = "# AGENT_MCP_MANAGED_BEGIN"
	traeEnd       = "# AGENT_MCP_MANAGED_END"
	opencodeBegin = "// AGENT_MCP_OPENCODE_MANAGED_BEGIN"
	opencodeEnd   = "// AGENT_MCP_OPENCODE_MANAGED_END"

	codexAnchor = "# ================= 本地保留区 (自动抓取)"
)

func pyRstrip(s string) string { return strings.TrimRightFunc(s, unicode.IsSpace) }
func pyLstrip(s string) string { return strings.TrimLeftFunc(s, unicode.IsSpace) }
func pyStrip(s string) string  { return strings.TrimFunc(s, unicode.IsSpace) }

// markerUpsert is the user-level transform used by `apply --scope user` and
// `upsert-stdin`. It replaces [begin..end] plus the single newline after end,
// leaving everything else byte-for-byte intact.
func markerUpsert(content, begin, end, block, anchor string) string {
	b := strings.TrimRight(block, "\n")
	if strings.Contains(content, begin) && strings.Contains(content, end) {
		pre, rest, _ := strings.Cut(content, begin)
		_, post, _ := strings.Cut(rest, end)
		if strings.HasPrefix(post, "\n") {
			post = post[1:]
		}
		if b != "" {
			return pre + b + "\n" + post
		}
		return pre + post
	}
	if b == "" {
		return content
	}
	pre, post := content, ""
	if anchor != "" {
		if i := strings.Index(content, anchor); i >= 0 {
			pre, post = content[:i], content[i:]
		}
	}
	if pre != "" && !strings.HasSuffix(pre, "\n") {
		pre += "\n"
	}
	return pre + b + "\n" + post
}

// upsertManagedBlock is the project-level block upsert for codex and trae.
func upsertManagedBlock(old, block, begin, end string) string {
	if strings.Contains(old, begin) && strings.Contains(old, end) {
		pre, rest, _ := strings.Cut(old, begin)
		_, post, _ := strings.Cut(rest, end)
		return pyRstrip(pre) + "\n" + pyRstrip(block) + "\n" + pyLstrip(post)
	}
	extra := ""
	if pyStrip(old) != "" {
		extra = "\n\n"
	}
	return pyRstrip(old) + extra + pyRstrip(block) + "\n"
}

// upsertOpencodeManagedBlock is the project-level block upsert for opencode.
func upsertOpencodeManagedBlock(old, block string) string {
	begin, end := opencodeBegin, opencodeEnd
	if strings.Contains(old, begin) && strings.Contains(old, end) {
		pre, rest, _ := strings.Cut(old, begin)
		_, post, _ := strings.Cut(rest, end)
		return pyRstrip(pre) + "\n" + pyRstrip(block) + "\n" + pyLstrip(post)
	}
	idx := strings.LastIndex(old, "}")
	if idx == -1 {
		return "{\n" + pyRstrip(block) + "\n}\n"
	}
	before, after := old[:idx], old[idx:]
	j := len(before) - 1
	for j >= 0 && isSpaceByte(before[j]) {
		j--
	}
	sep := "\n"
	if j >= 0 && before[j] != '{' && before[j] != ',' {
		sep = ",\n"
	}
	return pyRstrip(before) + sep + pyRstrip(block) + "\n" + pyLstrip(after)
}

// opencodeUserTransform is the opencode branch of agent_user_transform: it
// removes any old managed block and any stale top-level "mcp" member, then
// re-inserts the freshly rendered block before the root closing brace.
func opencodeUserTransform(content, block string) string {
	c := content
	if strings.Contains(c, opencodeBegin) && strings.Contains(c, opencodeEnd) {
		pre, rest, _ := strings.Cut(c, opencodeBegin)
		_, post, _ := strings.Cut(rest, opencodeEnd)
		c = pre + post
	}
	c = removeTopMCP(c)
	if pyStrip(block) == "" {
		return c
	}
	idx := strings.LastIndex(c, "}")
	if idx == -1 {
		return "{\n" + pyRstrip(block) + "\n}\n"
	}
	before, after := c[:idx], c[idx:]
	stripped := pyRstrip(before)
	if stripped == "" || strings.HasSuffix(stripped, "{") || strings.HasSuffix(stripped, ",") {
		sep := ""
		if strings.HasSuffix(stripped, ",") {
			sep = "\n"
		}
		return stripped + sep + pyRstrip(block) + "\n" + pyLstrip(after)
	}
	return stripped + ",\n" + pyRstrip(block) + "\n" + pyLstrip(after)
}

var topMCPRe = regexp.MustCompile(`(?m)^[ \t]{0,4}"mcp"\s*:\s*\{`)

// removeTopMCP strips a top-level "mcp" member (key through balanced braces,
// plus an optional trailing comma and newline) from a JSONC document. It
// mirrors the zsh/python brace-counting that ignores string literals on
// purpose.
func removeTopMCP(c string) string {
	loc := topMCPRe.FindStringIndex(c)
	if loc == nil {
		return c
	}
	braceRel := strings.Index(c[loc[0]:], "{")
	if braceRel < 0 {
		return c
	}
	i := loc[0] + braceRel
	depth := 0
	for ; i < len(c); i++ {
		switch c[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				goto done
			}
		}
	}
done:
	endIdx := i + 1
	for endIdx < len(c) && (c[endIdx] == ' ' || c[endIdx] == '\t') {
		endIdx++
	}
	if endIdx < len(c) && c[endIdx] == ',' {
		endIdx++
	}
	for endIdx < len(c) && (c[endIdx] == '\r' || c[endIdx] == '\n') {
		endIdx++
	}
	return c[:loc[0]] + c[endIdx:]
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\v' || b == '\f'
}

// agentUserTransform renders + upserts an agent's user-level block.
func agentUserTransform(content, agent, block string) (string, error) {
	switch agent {
	case "codex":
		return markerUpsert(content, codexBegin, codexEnd, block, codexAnchor), nil
	case "trae", "trae-cn":
		return markerUpsert(content, traeBegin, traeEnd, block, ""), nil
	case "opencode":
		return opencodeUserTransform(content, block), nil
	case "pi", "omp":
		return string(patchMCPJSON([]byte(content), block)), nil
	default:
		return "", errUnsupportedUserAgent(agent)
	}
}

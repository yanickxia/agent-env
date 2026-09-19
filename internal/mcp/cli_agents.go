package mcp

import (
	"fmt"
	"strings"

	"github.com/yanickxia/agent-env/internal/config"
)

func mapAidenScope(global bool) string {
	if global {
		return "global"
	}
	return "project"
}

// handleAiden builds and (in apply mode) runs the `aiden mcp add` command for
// one server row.
func (r *runner) handleAiden(mode string, row config.Server) error {
	name := strings.TrimSpace(row.Name)
	cmd := []string{"aiden", "mcp", "add"}

	transport := strings.TrimSpace(row.Type)
	switch transport {
	case "", "stdio":
		transport = "stdio"
	case "streamable-http":
		transport = "http"
	}

	aidenScope := mapAidenScope(row.Global)

	switch transport {
	case "stdio":
		command := strings.TrimSpace(row.Command)
		if command == "" {
			fmt.Fprintf(r.errw, "%s: aiden server '%s' requires a command-based MCP entry\n", config.Prog, name)
			return nil
		}
		cmd = append(cmd, "-s", aidenScope, name)
		for _, kv := range row.Env {
			cmd = append(cmd, "-e", kv.Key+"="+kv.Value)
		}
		cmd = append(cmd, "--", command)
		cmd = append(cmd, row.Args...)
	case "http", "sse":
		url := strings.TrimSpace(row.URL)
		if url == "" {
			fmt.Fprintf(r.errw, "%s: aiden server '%s' requires a url for %s transport\n", config.Prog, name, transport)
			return nil
		}
		cmd = append(cmd, "--transport", transport, "-s", aidenScope, name, url)
		if bt := strings.TrimSpace(row.BearerTokenEnvVar); bt != "" {
			token := r.resolveSecret(bt)
			if token == "" {
				fmt.Fprintf(r.errw, "%s: warning: token for '%s' is not set or empty (checked secrets.toml and environment)\n", config.Prog, bt)
			}
			cmd = append(cmd, "-H", "Authorization: Bearer "+token)
		} else {
			for _, kv := range row.Headers {
				cmd = append(cmd, "-H", kv.Key+": "+kv.Value)
			}
		}
	default:
		fmt.Fprintf(r.errw, "%s: unsupported server type '%s' for aiden server '%s'\n", config.Prog, strings.TrimSpace(row.Type), name)
		return nil
	}

	r.printCommand(cmd)
	if mode == CmdApply {
		if _, err := lookPath("aiden"); err != nil {
			fmt.Fprintf(r.errw, "%s: missing required command for aiden: aiden (skipped for server: %s)\n", config.Prog, name)
			return nil
		}
		_ = r.runCommand(cmd) // zsh: `|| true`
	}
	return nil
}

// handleClaudeProject builds and (in apply mode) runs `claude mcp add` for a
// repo-level claude server (global claude servers are patched into
// ~/.claude.json instead).
func (r *runner) handleClaudeProject(mode string, row config.Server) error {
	name := strings.TrimSpace(row.Name)
	scope := "project"
	cmd := []string{"claude", "mcp", "add"}
	serverType := strings.TrimSpace(row.Type)

	switch {
	case serverType == "stdio" || serverType == "":
		command := strings.TrimSpace(row.Command)
		if command == "" {
			return fmt.Errorf("%s: claude server '%s' requires a command-based MCP entry", config.Prog, name)
		}
		cmd = append(cmd, name)
		if scope != "" {
			cmd = append(cmd, "--scope", scope)
		}
		for _, kv := range row.Env {
			cmd = append(cmd, "-e", kv.Key+"="+kv.Value)
		}
		cmd = append(cmd, "--", command)
		cmd = append(cmd, row.Args...)

	case serverType == "http" || serverType == "streamable-http":
		url := strings.TrimSpace(row.URL)
		if url == "" {
			return fmt.Errorf("%s: claude server '%s' requires a url for http transport", config.Prog, name)
		}
		cmd = append(cmd, "--transport", "http", name)
		if scope != "" {
			cmd = append(cmd, "--scope", scope)
		}
		cmd = append(cmd, url)
		cmd = r.appendClaudeHeaders(cmd, row)

	case serverType == "sse":
		url := strings.TrimSpace(row.URL)
		if url == "" {
			return fmt.Errorf("%s: claude server '%s' requires a url for sse transport", config.Prog, name)
		}
		cmd = append(cmd, "--transport", "sse", name)
		if scope != "" {
			cmd = append(cmd, "--scope", scope)
		}
		cmd = append(cmd, url)
		cmd = r.appendClaudeHeaders(cmd, row)

	default:
		return fmt.Errorf("%s: unsupported server type '%s' for claude server '%s'", config.Prog, serverType, name)
	}

	r.printCommand(cmd)
	if mode == CmdApply {
		if _, err := lookPath("claude"); err != nil {
			return fmt.Errorf("%s: missing required command for claude: claude", config.Prog)
		}
		_ = r.runCommand(cmd) // zsh: `|| true`
	}
	return nil
}

func (r *runner) appendClaudeHeaders(cmd []string, row config.Server) []string {
	if bt := strings.TrimSpace(row.BearerTokenEnvVar); bt != "" {
		token := r.resolveSecret(bt)
		if token == "" {
			fmt.Fprintf(r.errw, "%s: warning: token for '%s' is not set or empty (checked secrets.toml and environment)\n", config.Prog, bt)
		}
		return append(cmd, "--header", "Authorization: Bearer "+token)
	}
	for _, kv := range row.Headers {
		cmd = append(cmd, "--header", kv.Key+": "+kv.Value)
	}
	return cmd
}

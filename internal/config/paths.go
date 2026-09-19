// Package config resolves the on-disk configuration for agent-env:
// the unified global config ([[installs]] for skills, [[servers]] for MCP),
// the per-repo .agent-env.toml, and the secrets file.
package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Prog is the tool name used as the prefix of every user-facing message.
const Prog = "agent-env"

// GetenvFunc is the shape of os.Getenv, injectable for tests.
type GetenvFunc func(string) string

// ManifestPath resolves the unified config path. Precedence mirrors the zsh
// original: AGENT_ENV_CONFIG is canonical, AGENT_SKILLS_MANIFEST is a silent
// compatibility override, and the default is ~/.config/agent-env/config.toml.
func ManifestPath(getenv GetenvFunc, home string) string {
	if v := getenv("AGENT_ENV_CONFIG"); v != "" {
		return v
	}
	if v := getenv("AGENT_SKILLS_MANIFEST"); v != "" {
		return v
	}
	return filepath.Join(home, ".config", "agent-env", "config.toml")
}

// StatePath resolves the --skip-unchanged stamp file.
func StatePath(getenv GetenvFunc, home string) string {
	if v := getenv("AGENT_SKILLS_STATE"); v != "" {
		return v
	}
	return filepath.Join(home, ".local", "state", "agent-skills-sync", "state.tsv")
}

// RepoConfigPath resolves the per-repo config. AGENT_ENV_REPO_CONFIG is
// canonical; AGENT_SKILLS_REPO_CONFIG stays a silent alias. The default lives
// at <projectRoot>/.agent-env.toml.
func RepoConfigPath(getenv GetenvFunc, projectRoot string) string {
	if v := getenv("AGENT_ENV_REPO_CONFIG"); v != "" {
		return v
	}
	if v := getenv("AGENT_SKILLS_REPO_CONFIG"); v != "" {
		return v
	}
	return filepath.Join(projectRoot, ".agent-env.toml")
}

// SecretsPath resolves the MCP secrets file. AGENT_MCP_SECRETS overrides the
// default ~/.config/agent-env/secrets.toml.
func SecretsPath(getenv GetenvFunc, home string) string {
	if v := getenv("AGENT_MCP_SECRETS"); v != "" {
		return v
	}
	return filepath.Join(home, ".config", "agent-env", "secrets.toml")
}

// ClaudeJSONPath resolves the ~/.claude.json patch target (CLAUDE_JSON wins).
func ClaudeJSONPath(getenv GetenvFunc, home string) string {
	if v := getenv("CLAUDE_JSON"); v != "" {
		return v
	}
	return filepath.Join(home, ".claude.json")
}

// ProjectRoot returns the git top-level for dir, falling back to dir itself
// when git is unavailable or dir is not inside a repository.
func ProjectRoot(dir string) string {
	if _, err := exec.LookPath("git"); err == nil {
		cmd := exec.Command("git", "rev-parse", "--show-toplevel")
		cmd.Dir = dir
		if out, err := cmd.Output(); err == nil {
			if root := strings.TrimSpace(string(out)); root != "" {
				return root
			}
		}
	}
	return dir
}

// ExpandSource turns a leading "~/" into the user's home directory, matching
// the zsh expand_source helper.
func ExpandSource(home, source string) string {
	if strings.HasPrefix(source, "~/") {
		return filepath.Join(home, source[2:])
	}
	return source
}

// DefaultHome returns the current user's home directory, or "" on failure.
func DefaultHome() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

package config

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// HasInlineEntries allows a self-contained project config without a global
// manifest (or without one of its domain tables). Malformed files still fail.
func (c *RepoConfig) HasInlineEntries() bool {
	return c != nil && (len(c.Installs) > 0 || len(c.Servers) > 0)
}

func (c *RepoConfig) loadInlineEntries(raw map[string]any, md toml.MetaData) error {
	path, err := filepath.Abs(c.Path)
	if err != nil {
		return err
	}
	if val, ok := raw["installs"]; ok {
		if err := validateInlineKeys(val, "installs", path, "source name agents skills mode profiles post_install installer env"); err != nil {
			return err
		}
		entries, err := parseInstalls(val, path)
		if err != nil {
			return err
		}
		for i := range entries {
			e := &entries[i]
			if err := validateInlineProfiles(e.Profiles, path); err != nil {
				return err
			}
			if e.Mode != "" && e.Mode != "copy" && e.Mode != "symlink" {
				return fmt.Errorf("%s: inline install mode must be symlink or copy in %s", Prog, path)
			}
			e.RepoPath = path
			e.Source = relativeToConfig(e.Source, path)
		}
		c.Installs = entries
	}
	if val, ok := raw["servers"]; ok {
		if err := validateInlineKeys(val, "servers", path, "name type command url args agents profiles env headers env_vars bearer_token_env_var startup_timeout_sec"); err != nil {
			return err
		}
		entries, err := parseServerEntries(val, path, md, nil)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for i := range entries {
			e := &entries[i]
			if err := validateInlineProfiles(e.Profiles, path); err != nil {
				return err
			}
			if seen[e.Name] {
				return fmt.Errorf("%s: duplicate server name %q in %s", Prog, e.Name, path)
			}
			seen[e.Name] = true
			e.RepoPath = path
			e.Command = relativeToConfig(e.Command, path)
		}
		c.Servers = entries
	}
	return nil
}

func validateInlineKeys(value any, table, path, supported string) error {
	allowed := map[string]bool{}
	for _, key := range strings.Fields(supported) {
		allowed[key] = true
	}
	for _, item := range toEntryList(value) {
		entry, ok := item.(map[string]any)
		if !ok {
			continue // The shared parser reports table/type errors.
		}
		unknown := []string{}
		for key := range entry {
			if !allowed[key] {
				unknown = append(unknown, key)
			}
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			return fmt.Errorf("%s: inline [[%s]] key(s) not allowed: %s in %s (project-only definitions; scope is not permitted; post_install is skills-only and gated by --allow-hooks)", Prog, table, strings.Join(unknown, ", "), path)
		}
	}
	return nil
}

func validateInlineProfiles(profiles []string, path string) error {
	for _, p := range profiles {
		if IsGlobalProfile(p) {
			return fmt.Errorf("%s: inline definitions are project-only; profiles cannot contain \"global\" in %s", Prog, path)
		}
		if !KebabCase(p) {
			return fmt.Errorf("%s: profile names must be kebab-case, got %q in %s", Prog, p, path)
		}
	}
	return nil
}

// Only explicit relative paths are rebased: owner/repo, URLs, ~/ paths and
// bare executable names retain their existing meanings.
func relativeToConfig(value, path string) string {
	if value == "." || value == ".." || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") {
		return filepath.Join(filepath.Dir(path), value)
	}
	return value
}

// MergeInstalls overlays named project entries, nearest first. Unnamed
// installs accumulate; global entries are never shadowed by project entries.
func MergeInstalls(near, far []Install) []Install {
	out := append([]Install{}, near...)
	seen := map[string]bool{}
	for _, e := range near {
		if !e.Global && e.Name != "" {
			seen[e.Name] = true
		}
	}
	for _, e := range far {
		if e.Global || e.Name == "" || !seen[e.Name] {
			out = append(out, e)
		}
	}
	return out
}

// MergeServers uses the same project-only name precedence as MergeInstalls.
func MergeServers(near, far []Server) []Server {
	out := append([]Server{}, near...)
	seen := map[string]bool{}
	for _, e := range near {
		if !e.Global {
			seen[e.Name] = true
		}
	}
	for _, e := range far {
		if e.Global || !seen[e.Name] {
			out = append(out, e)
		}
	}
	return out
}

// LoadProjectInstalls combines the global manifest with the effective repo
// definitions. Inline entries inherit repo agents/mode when omitted.
func LoadProjectInstalls(path string, repo *RepoConfig) ([]Install, error) {
	entries, err := parseManifest(path, repo.HasInlineEntries())
	if err != nil || repo == nil {
		return entries, err
	}
	local := append([]Install{}, repo.Installs...)
	for i := range local {
		if len(local[i].Agents) == 0 {
			local[i].Agents = append([]string{}, repo.Agents...)
			local[i].AgentsRaw = strings.Join(local[i].Agents, ",")
		}
		if local[i].Mode == "" {
			local[i].Mode = repo.Mode
		}
	}
	return MergeInstalls(local, entries), nil
}

// LoadProjectServers resolves inline placeholders only in the MCP domain,
// using exactly the same secret resolver as global entries.
// The boolean reports whether a global servers table was present. A local-only
// run must not clear existing user configuration when that table is absent.
func LoadProjectServers(path string, repo *RepoConfig, resolve SecretResolver) ([]Server, bool, error) {
	entries, err := parseServers(path, resolve, repo.HasInlineEntries())
	if err != nil || repo == nil {
		return entries, entries != nil, err
	}
	local := append([]Server{}, repo.Servers...)
	for i := range local {
		e := &local[i]
		if len(e.Agents) == 0 {
			e.Agents = CanonicalAgents(repo.Agents)
		}
		e.URL = expandPlaceholders(e.URL, resolve)
		e.Args = expandAll(e.Args, resolve)
		e.Env = expandKV(e.Env, resolve)
		e.Headers = expandKV(e.Headers, resolve)
	}
	return MergeServers(local, entries), entries != nil, nil
}

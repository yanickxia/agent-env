package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// RepoConfig is a validated .agent-env.toml. It can only *select* profiles and
// agents; install details and hooks are rejected on purpose so a checked-in
// repo config can never execute code.
type RepoConfig struct {
	Path     string
	Profiles []string
	Names    []string
	Agents   []string
	// AgentsDeclared reports whether the agents key was present (so an explicit
	// empty array still counts as "declared" during layered merging).
	AgentsDeclared bool
	Mode           string
	Vars           map[string]any
}

// LoadRepoConfig reads and validates path. found=false means the file is
// absent (callers decide whether that is fatal); any invalid content returns
// an error so a repo selection is never half-applied.
func LoadRepoConfig(path string) (cfg *RepoConfig, found bool, err error) {
	return LoadRepoConfigWithHint(path, "install details and hooks belong in the global manifest")
}

// LoadRepoConfigWithHint is LoadRepoConfig with a domain-specific suffix for
// the unknown-key error, so the skills and MCP domains can each point at their
// own manifest location while sharing one parser.
func LoadRepoConfigWithHint(path, unknownKeyHint string) (cfg *RepoConfig, found bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("%s: cannot read repo config %s: %v", Prog, path, err)
	}

	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, false, fmt.Errorf("%s: repo config is not valid TOML: %s: %v", Prog, path, err)
	}

	allowed := []string{"agents", "mode", "names", "profiles", "vars"}
	unknown := []string{}
	for k := range raw {
		if k != "profiles" && k != "names" && k != "agents" && k != "mode" && k != "vars" {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, false, fmt.Errorf("%s: repo config only allows %s; found %s in %s (%s)",
			Prog, strings.Join(allowed, ", "), strings.Join(unknown, ", "), path, unknownKeyHint)
	}

	profiles, err := normalizeStringList(raw["profiles"], "profiles", path)
	if err != nil {
		return nil, false, err
	}
	names, err := normalizeStringList(raw["names"], "names", path)
	if err != nil {
		return nil, false, err
	}
	_, agentsPresent := raw["agents"]
	agents, err := normalizeStringList(raw["agents"], "agents", path)
	if err != nil {
		return nil, false, err
	}

	for _, name := range profiles {
		if !kebabRe.MatchString(name) {
			return nil, false, fmt.Errorf("%s: profile names must be kebab-case (e.g. \"ark-mlops\"), got \"%s\" in %s", Prog, name, path)
		}
		if IsGlobalProfile(name) {
			return nil, false, fmt.Errorf("%s: \"global\" is a reserved profile keyword and cannot be selected in repo config: %s", Prog, path)
		}
	}
	for _, name := range names {
		if !kebabRe.MatchString(name) {
			return nil, false, fmt.Errorf("%s: names must be kebab-case, got \"%s\" in %s", Prog, name, path)
		}
		if IsGlobalProfile(name) {
			return nil, false, fmt.Errorf("%s: \"global\" is a reserved keyword and cannot be used as a name in repo config: %s", Prog, path)
		}
	}

	mode := ""
	if mv, ok := raw["mode"]; ok && mv != nil {
		s, isStr := mv.(string)
		if !isStr {
			return nil, false, fmt.Errorf("%s: repo config \"mode\" must be \"symlink\" or \"copy\" in %s", Prog, path)
		}
		mode = s
		if mode != "symlink" && mode != "copy" {
			return nil, false, fmt.Errorf("%s: repo config \"mode\" must be \"symlink\" or \"copy\" in %s", Prog, path)
		}
	}

	vars := map[string]any{}
	if vv, ok := raw["vars"]; ok && vv != nil {
		tbl, isTbl := vv.(map[string]any)
		if !isTbl {
			return nil, false, fmt.Errorf("%s: repo config \"vars\" must be a table in %s", Prog, path)
		}
		vars = tbl
	}

	return &RepoConfig{
		Path:           path,
		Profiles:       profiles,
		Names:          names,
		Agents:         agents,
		AgentsDeclared: agentsPresent,
		Mode:           mode,
		Vars:           vars,
	}, true, nil
}

// normalizeStringList mirrors the zsh repo-config helper: nil -> [], a bare
// string -> [value], an all-string array -> trimmed non-empty items.
func normalizeStringList(value any, field, path string) ([]string, error) {
	if value == nil {
		return []string{}, nil
	}
	var items []string
	switch t := value.(type) {
	case string:
		items = []string{t}
	case []any:
		for _, item := range t {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s: \"%s\" must be a string array in %s", Prog, field, path)
			}
			items = append(items, s)
		}
	default:
		return nil, fmt.Errorf("%s: \"%s\" must be a string array in %s", Prog, field, path)
	}

	out := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out, nil
}

// DefaultRepoConfigPath is the conventional single-repo path. When an explicit
// override equals this path it is treated as "no override" so layered discovery
// can still run (the file itself is discovered as the repo-root layer).
func DefaultRepoConfigPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".agent-env.toml")
}

// RepoConfigOverride returns the explicit repo config path from the canonical
// env var or the legacy alias, or "" when neither is set.
func RepoConfigOverride(getenv GetenvFunc) string {
	if v := getenv("AGENT_ENV_REPO_CONFIG"); v != "" {
		return v
	}
	if v := getenv("AGENT_SKILLS_REPO_CONFIG"); v != "" {
		return v
	}
	return ""
}

// ancestorDirs returns start's absolute path and every ancestor up to the
// filesystem root, nearest first.
func ancestorDirs(start string) []string {
	abs, err := filepath.Abs(start)
	if err != nil {
		abs = filepath.Clean(start)
	}
	dirs := []string{}
	for {
		dirs = append(dirs, abs)
		parent := filepath.Dir(abs)
		if parent == abs {
			break
		}
		abs = parent
	}
	return dirs
}

// WalkRepoConfigs collects and validates .agent-env.toml from startDir up to
// "/", nearest first. An invalid file fails the walk with its full path.
func WalkRepoConfigs(startDir, hint string) ([]*RepoConfig, []string, error) {
	configs := []*RepoConfig{}
	paths := []string{}
	for _, dir := range ancestorDirs(startDir) {
		path := filepath.Join(dir, ".agent-env.toml")
		fi, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, nil, fmt.Errorf("%s: cannot read repo config %s: %v", Prog, path, err)
		}
		if fi.IsDir() {
			continue
		}
		cfg, found, err := LoadRepoConfigWithHint(path, hint)
		if err != nil {
			return nil, nil, err
		}
		if !found {
			continue
		}
		configs = append(configs, cfg)
		paths = append(paths, path)
	}
	return configs, paths, nil
}

// MergeRepoConfigs overlays layers nearest-first: profiles are unioned with
// nearest priority, agents/mode use the nearest declaring layer, and vars are
// merged per key with nearest override.
func MergeRepoConfigs(configs []*RepoConfig) *RepoConfig {
	merged := &RepoConfig{Vars: map[string]any{}}
	seenProfiles := map[string]bool{}
	seenNames := map[string]bool{}
	for _, cfg := range configs {
		for _, p := range cfg.Profiles {
			if !seenProfiles[p] {
				seenProfiles[p] = true
				merged.Profiles = append(merged.Profiles, p)
			}
		}
		for _, n := range cfg.Names {
			if !seenNames[n] {
				seenNames[n] = true
				merged.Names = append(merged.Names, n)
			}
		}
		if !merged.AgentsDeclared && cfg.AgentsDeclared {
			merged.Agents = append([]string{}, cfg.Agents...)
			merged.AgentsDeclared = true
		}
		if merged.Mode == "" && cfg.Mode != "" {
			merged.Mode = cfg.Mode
		}
		for k, v := range cfg.Vars {
			if _, ok := merged.Vars[k]; !ok {
				merged.Vars[k] = v
			}
		}
	}
	return merged
}

// LoadRepoSelection resolves the effective repo selection. An explicit path
// (from Options, AGENT_ENV_REPO_CONFIG or AGENT_SKILLS_REPO_CONFIG) forces
// single-file mode; otherwise .agent-env.toml files are discovered from
// startDir up to "/" and merged. It returns the merged config, the discovered
// layer paths (nearest first), and found=false when no layer exists.
func LoadRepoSelection(explicit, startDir string, getenv GetenvFunc, hint string) (*RepoConfig, []string, bool, error) {
	if explicit == "" {
		explicit = RepoConfigOverride(getenv)
	}
	if explicit != "" {
		cfg, found, err := LoadRepoConfigWithHint(explicit, hint)
		if err != nil {
			return nil, nil, false, err
		}
		if !found {
			return nil, []string{}, false, nil
		}
		return cfg, []string{explicit}, true, nil
	}
	if startDir == "" {
		startDir = "."
	}
	configs, paths, err := WalkRepoConfigs(startDir, hint)
	if err != nil {
		return nil, nil, false, err
	}
	if len(configs) == 0 {
		return nil, []string{}, false, nil
	}
	return MergeRepoConfigs(configs), paths, true, nil
}

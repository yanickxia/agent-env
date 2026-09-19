package config

import (
	"fmt"
	"os"
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
	Agents   []string
	Mode     string
	Vars     map[string]any
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

	allowed := []string{"agents", "mode", "profiles", "vars"}
	unknown := []string{}
	for k := range raw {
		if k != "profiles" && k != "agents" && k != "mode" && k != "vars" {
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
	agents, err := normalizeStringList(raw["agents"], "agents", path)
	if err != nil {
		return nil, false, err
	}

	if len(profiles) == 0 {
		return nil, false, fmt.Errorf("%s: repo config must declare at least one profile: %s", Prog, path)
	}
	for _, name := range profiles {
		if !kebabRe.MatchString(name) {
			return nil, false, fmt.Errorf("%s: profile names must be kebab-case (e.g. \"ark-mlops\"), got \"%s\" in %s", Prog, name, path)
		}
		if IsGlobalProfile(name) {
			return nil, false, fmt.Errorf("%s: \"global\" is a reserved profile keyword and cannot be selected in repo config: %s", Prog, path)
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
		Path:     path,
		Profiles: profiles,
		Agents:   agents,
		Mode:     mode,
		Vars:     vars,
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

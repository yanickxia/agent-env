package config

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Stream separators for the entry payload. The exact bytes matter: they are
// part of the --skip-unchanged signature so the encoding must stay
// byte-compatible with the zsh implementation.
const (
	SepUS = "\x1f" // between an entry's fields
	SepRS = "\x1e" // between post_install/env records
	SepGS = "\x1d" // between a record's key/run and value/if_missing
)

const ctrlChars = "\t\n\r\x1c" + SepGS + SepRS + SepUS

var kebabRe = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// EnvVar is one environment variable for the primary installer.
type EnvVar struct {
	Key   string
	Value string
}

// PostInstall is one dependency-installer hook.
type PostInstall struct {
	Run       string
	IfMissing string
}

// Install is a fully validated [[installs]] entry.
type Install struct {
	Source    string
	AgentsRaw string
	SkillsRaw string
	Agents    []string
	Skills    []string

	Scope string
	Mode  string

	ProfilesRaw string
	Profiles    []string

	PostInstall []PostInstall
	Installer   string

	Env    []EnvVar
	EnvRaw string
}

// ParseManifest reads and validates the unified config's [[installs]] table.
func ParseManifest(path string) ([]Install, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s: manifest not found: %s", Prog, path)
		}
		return nil, fmt.Errorf("%s: cannot read manifest %s: %v", Prog, path, err)
	}

	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, fmt.Errorf("%s: manifest is not valid TOML: %s: %v", Prog, path, err)
	}

	installsVal, ok := raw["installs"]
	if !ok {
		return nil, fmt.Errorf("%s: manifest must contain an \"installs\" array: %s", Prog, path)
	}
	entries := toEntryList(installsVal)
	if entries == nil {
		return nil, fmt.Errorf("%s: manifest must contain an \"installs\" array: %s", Prog, path)
	}

	out := make([]Install, 0, len(entries))
	for _, e := range entries {
		inst, err := parseInstall(e, path)
		if err != nil {
			return nil, err
		}
		out = append(out, inst)
	}
	return out, nil
}

// toEntryList normalizes the generic TOML representation of [[installs]] into
// a []any so non-table entries can be reported with a precise error.
func toEntryList(v any) []any {
	switch t := v.(type) {
	case []map[string]any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = e
		}
		return out
	case []any:
		return t
	default:
		return nil
	}
}

func parseInstall(e any, manifestPath string) (Install, error) {
	entry, ok := e.(map[string]any)
	if !ok {
		return Install{}, fmt.Errorf("%s: each install entry must be a table: %s", Prog, manifestPath)
	}

	srcVal, ok := entry["source"]
	src, isStr := srcVal.(string)
	if !ok || !isStr || strings.TrimSpace(src) == "" {
		return Install{}, fmt.Errorf("%s: each install entry must include a non-empty \"source\": %s", Prog, manifestPath)
	}
	source := strings.TrimSpace(src)

	agentsList, err := normalizeList(entry["agents"], "agents", source)
	if err != nil {
		return Install{}, err
	}
	skillsList, err := normalizeList(entry["skills"], "skills", source)
	if err != nil {
		return Install{}, err
	}
	agentsRaw := strings.Join(agentsList, ",")
	skillsRaw := strings.Join(skillsList, ",")

	mode := ""
	if mv, ok := entry["mode"]; ok && mv != nil {
		s, isStr := mv.(string)
		if !isStr {
			return Install{}, fmt.Errorf("%s: \"mode\" must be a string in source: %s", Prog, source)
		}
		mode = s
	}

	scope, err := expandScopes(entry["scope"], source)
	if err != nil {
		return Install{}, err
	}
	postInstall, _, err := normalizePostInstall(entry["post_install"], source)
	if err != nil {
		return Install{}, err
	}
	installer, err := normalizeInstaller(entry["installer"], source)
	if err != nil {
		return Install{}, err
	}
	env, envRaw, err := normalizeEnv(entry["env"], source)
	if err != nil {
		return Install{}, err
	}
	profilesRaw, profiles, err := normalizeProfiles(entry["profiles"], source)
	if err != nil {
		return Install{}, err
	}

	inst := Install{
		Source:      source,
		AgentsRaw:   agentsRaw,
		SkillsRaw:   skillsRaw,
		Agents:      splitTrimNonEmpty(agentsRaw),
		Skills:      splitTrimNonEmpty(skillsRaw),
		Scope:       scope,
		Mode:        mode,
		ProfilesRaw: profilesRaw,
		Profiles:    profiles,
		PostInstall: postInstall,
		Installer:   installer,
		Env:         env,
		EnvRaw:      envRaw,
	}
	return inst, nil
}

// normalizeList mirrors the zsh normalize_list helper: nil -> [], a bare
// string -> [value], an all-string array -> itself, anything else -> error.
func normalizeList(value any, field, source string) ([]string, error) {
	if value == nil {
		return []string{}, nil
	}
	if s, ok := value.(string); ok {
		return []string{s}, nil
	}
	if arr, ok := value.([]any); ok {
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s: \"%s\" must be a string or array in source: %s", Prog, field, source)
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("%s: \"%s\" must be a string or array in source: %s", Prog, field, source)
}

// expandScopes validates the single-select scope field and returns exactly one
// canonical value ("user" or "project").
func expandScopes(raw any, source string) (string, error) {
	if raw == nil {
		return "", fmt.Errorf("%s: each install entry must declare \"scope\" as \"user\" or \"project\": %s", Prog, source)
	}
	if s, ok := raw.(string); ok {
		v := strings.TrimSpace(s)
		if v == "user" || v == "project" {
			return v, nil
		}
		hint := ""
		if v == "global" || v == "" {
			hint = " (use \"user\")"
		} else if v == "local" {
			hint = " (use \"project\")"
		}
		return "", fmt.Errorf("%s: scope must be \"user\" or \"project\", got \"%s\"%s in source: %s", Prog, v, hint, source)
	}
	if _, ok := raw.([]any); ok {
		return "", fmt.Errorf("%s: \"scope\" is single-select now and must be the string \"user\" or \"project\"; arrays are no longer supported, remove the array in source: %s", Prog, source)
	}
	return "", fmt.Errorf("%s: \"scope\" must be the string \"user\" or \"project\" in source: %s", Prog, source)
}

func normalizePostInstall(value any, source string) ([]PostInstall, string, error) {
	if value == nil {
		return nil, "", nil
	}
	arr, ok := value.([]any)
	if !ok {
		return nil, "", fmt.Errorf("%s: \"post_install\" must be an array in source: %s", Prog, source)
	}

	records := make([]string, 0, len(arr))
	out := make([]PostInstall, 0, len(arr))
	for _, item := range arr {
		var tbl map[string]any
		switch t := item.(type) {
		case string:
			tbl = map[string]any{"run": t}
		case map[string]any:
			tbl = t
		default:
			return nil, "", fmt.Errorf("%s: \"post_install\" items must be strings or tables in source: %s", Prog, source)
		}

		unknown := []string{}
		for k := range tbl {
			if k != "run" && k != "if_missing" {
				unknown = append(unknown, k)
			}
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			return nil, "", fmt.Errorf("%s: unknown post_install key(s) %s in source: %s (supported: run, if_missing)", Prog, strings.Join(unknown, ", "), source)
		}

		runVal, ok := tbl["run"]
		run, isStr := runVal.(string)
		if !ok || !isStr || strings.TrimSpace(run) == "" {
			return nil, "", fmt.Errorf("%s: post_install \"run\" must be a non-empty string in source: %s", Prog, source)
		}
		ifMissing := ""
		if mv, ok := tbl["if_missing"]; ok && mv != nil {
			s, isStr := mv.(string)
			if !isStr {
				return nil, "", fmt.Errorf("%s: post_install \"if_missing\" must be a string in source: %s", Prog, source)
			}
			ifMissing = s
		}

		run = strings.TrimSpace(run)
		ifMissing = strings.TrimSpace(ifMissing)
		if containsAny(run, ctrlChars) || containsAny(ifMissing, ctrlChars) {
			field := "run"
			if containsAny(run, ctrlChars) {
				field = "run"
			} else {
				field = "if_missing"
			}
			return nil, "", fmt.Errorf("%s: post_install \"%s\" must not contain tabs, newlines or ASCII separator control characters in source: %s", Prog, field, source)
		}

		out = append(out, PostInstall{Run: run, IfMissing: ifMissing})
		records = append(records, run+SepGS+ifMissing)
	}
	return out, strings.Join(records, SepRS), nil
}

func normalizeProfiles(value any, source string) (string, []string, error) {
	if value == nil {
		return "", nil, nil
	}
	items, err := normalizeList(value, "profiles", source)
	if err != nil {
		return "", nil, err
	}
	ordered := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		if !kebabRe.MatchString(name) {
			return "", nil, fmt.Errorf("%s: profile names must be kebab-case (e.g. \"ark-mlops\"), got \"%s\" in source: %s", Prog, name, source)
		}
		if !seen[name] {
			seen[name] = true
			ordered = append(ordered, name)
		}
	}
	return strings.Join(ordered, ","), ordered, nil
}

func normalizeInstaller(value any, source string) (string, error) {
	if value == nil {
		return "skills", nil
	}
	s, ok := value.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("%s: \"installer\" must be a non-empty string in source: %s", Prog, source)
	}
	installer := strings.TrimSpace(s)
	switch installer {
	case "skills":
		return "skills", nil
	case "agentbuddy":
		return "", fmt.Errorf("%s: installer \"agentbuddy\" is not supported by agent-env (supported: skills); migrate this entry to installer = \"skills\" in source: %s", Prog, source)
	default:
		return "", fmt.Errorf("%s: unsupported installer \"%s\" in source: %s (supported: skills)", Prog, installer, source)
	}
}

func normalizeEnv(value any, source string) ([]EnvVar, string, error) {
	if value == nil {
		return nil, "", nil
	}
	tbl, ok := value.(map[string]any)
	if !ok {
		return nil, "", fmt.Errorf("%s: \"env\" must be a table in source: %s", Prog, source)
	}

	keys := make([]string, 0, len(tbl))
	for k := range tbl {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	records := make([]string, 0, len(keys))
	out := make([]EnvVar, 0, len(keys))
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			return nil, "", fmt.Errorf("%s: env keys must be non-empty strings in source: %s", Prog, source)
		}
		if strings.Contains(key, "=") || containsAny(key, ctrlChars) {
			return nil, "", fmt.Errorf("%s: env key \"%s\" is not a valid environment variable name in source: %s", Prog, key, source)
		}
		envValue, isStr := tbl[key].(string)
		if !isStr {
			return nil, "", fmt.Errorf("%s: env value for \"%s\" must be a string in source: %s", Prog, key, source)
		}
		if containsAny(envValue, ctrlChars) {
			return nil, "", fmt.Errorf("%s: env value for \"%s\" must not contain tabs, newlines or ASCII separator control characters in source: %s", Prog, key, source)
		}
		out = append(out, EnvVar{Key: strings.TrimSpace(key), Value: envValue})
		records = append(records, strings.TrimSpace(key)+SepGS+envValue)
	}
	return out, strings.Join(records, SepRS), nil
}

func splitTrimNonEmpty(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func containsAny(s, chars string) bool {
	for _, r := range chars {
		if strings.ContainsRune(s, r) {
			return true
		}
	}
	return false
}

// KebabCase reports whether name is a valid profile identifier.
func KebabCase(name string) bool {
	return kebabRe.MatchString(name)
}

// JoinEnvArgs renders entry env vars as KEY=VALUE pairs for `env`.
func (i Install) EnvArgs() []string {
	out := make([]string, 0, len(i.Env))
	for _, e := range i.Env {
		out = append(out, e.Key+"="+e.Value)
	}
	return out
}

// PostInstallPayload re-encodes the hooks with the same separators the zsh
// implementation used, so the per-entry dedup key stays identical.
func (i Install) PostInstallPayload() string {
	if len(i.PostInstall) == 0 {
		return ""
	}
	records := make([]string, 0, len(i.PostInstall))
	for _, p := range i.PostInstall {
		records = append(records, p.Run+SepGS+p.IfMissing)
	}
	return strings.Join(records, SepRS)
}

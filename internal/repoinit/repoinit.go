// Package repoinit implements `agent-env init`: create or merge the per-repo
// .agent-env.toml (profiles/agents selection only), then optionally run the
// project-scope skills apply. Behaviour mirrors the retired zsh
// agent-skills-init script.
package repoinit

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/yanickxia/agent-env/internal/config"
)

// Options carries resolved paths and injectable IO.
type Options struct {
	RepoConfigPath string
	ProjectRoot    string
	Stdout         io.Writer
	Stderr         io.Writer

	LookupEnv  func(string) string
	Executable func() (string, error)
	RunApply   func(bin string, dir string, stdout, stderr io.Writer) error
}

// Filters is the parsed CLI surface.
type Filters struct {
	Profiles []string
	Agents   []string
	Apply    bool
	DryRun   bool
}

// ApplyFailedError carries the child sync exit code so the CLI can propagate it.
type ApplyFailedError struct{ Code int }

func (e *ApplyFailedError) Error() string {
	return fmt.Sprintf("%s: skills apply exited with code %d", config.Prog, e.Code)
}

// Run executes the init command.
func Run(opts Options, f Filters) error {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.LookupEnv == nil {
		opts.LookupEnv = os.Getenv
	}
	if opts.Executable == nil {
		opts.Executable = os.Executable
	}

	profiles := dedupeTrim(f.Profiles)
	agents := dedupeTrim(f.Agents)

	if len(profiles) == 0 {
		return fmt.Errorf("%s: at least one PROFILE is required", config.Prog)
	}
	for _, name := range profiles {
		if !config.KebabCase(name) {
			return fmt.Errorf("%s: invalid profile name '%s' (expected kebab-case matching ^[a-z][a-z0-9]*(-[a-z0-9]+)*$)", config.Prog, name)
		}
		if config.IsGlobalProfile(name) {
			return fmt.Errorf("%s: \"global\" is a reserved profile keyword and cannot be selected in a repo config; list the repo-level tags you need instead", config.Prog)
		}
	}

	configPath := opts.RepoConfigPath
	if configPath == "" {
		configPath = config.RepoConfigPath(opts.LookupEnv, opts.ProjectRoot)
	}

	var existing *existingConfig
	if fileExists(configPath) {
		parsed, err := parseExisting(configPath)
		if err != nil {
			return err
		}
		existing = parsed
	}

	finalProfiles := []string{}
	finalAgents := []string{}
	mode := ""
	vars := map[string]any{}
	if existing != nil {
		finalProfiles = append(finalProfiles, existing.profiles...)
		finalAgents = append(finalAgents, existing.agents...)
		mode = existing.mode
		vars = existing.vars
	}
	finalProfiles = appendUniqueItems(finalProfiles, profiles)
	finalAgents = appendUniqueItems(finalAgents, agents)

	content := renderContent(finalProfiles, finalAgents, mode, vars)

	if f.DryRun {
		fmt.Fprintf(opts.Stdout, "would write %s\n", configPath)
		fmt.Fprint(opts.Stdout, content)
	} else {
		if err := atomicWrite(configPath, content); err != nil {
			return err
		}
		fmt.Fprintf(opts.Stdout, "wrote %s\n", configPath)
		fmt.Fprintf(opts.Stdout, "profiles: %s\n", strings.Join(finalProfiles, ", "))
		if len(finalAgents) > 0 {
			fmt.Fprintf(opts.Stdout, "agents: %s\n", strings.Join(finalAgents, ", "))
		}
	}

	if f.Apply {
		bin, err := syncBin(opts)
		if err != nil {
			return err
		}
		printCommand(opts.Stdout, bin, "skills", "apply", "--non-interactive", "--skip-unchanged")
		if f.DryRun {
			return nil
		}
		run := opts.RunApply
		if run == nil {
			run = defaultRunApply
		}
		return run(bin, opts.ProjectRoot, opts.Stdout, opts.Stderr)
	}
	if !f.DryRun {
		fmt.Fprintf(opts.Stdout, "next: agent-env skills apply --non-interactive\n")
	}
	return nil
}

func defaultRunApply(bin, dir string, stdout, stderr io.Writer) error {
	cmd := exec.Command(bin, "skills", "apply", "--non-interactive", "--skip-unchanged")
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		return &ApplyFailedError{Code: ee.ExitCode()}
	}
	if err != nil {
		return fmt.Errorf("%s: cannot run %s: %v", config.Prog, bin, err)
	}
	return nil
}

func syncBin(opts Options) (string, error) {
	if v := opts.LookupEnv("AGENT_SKILLS_SYNC_BIN"); v != "" {
		return v, nil
	}
	exe, err := opts.Executable()
	if err != nil {
		return "", fmt.Errorf("%s: cannot resolve the agent-env executable: %v", config.Prog, err)
	}
	return exe, nil
}

// --- existing config parsing -------------------------------------------------

type existingConfig struct {
	profiles []string
	agents   []string
	mode     string
	vars     map[string]any
}

func parseExisting(path string) (*existingConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: cannot read repo config: %s: %v", config.Prog, path, err)
	}
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, fmt.Errorf("%s: invalid TOML in repo config: %s: %v", config.Prog, path, err)
	}

	allowed := map[string]bool{"profiles": true, "agents": true, "vars": true, "mode": true}
	unknown := []string{}
	for k := range raw {
		if !allowed[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("%s: repo config key(s) not allowed: %s in %s; repo config may only declare profiles/agents/vars/mode (installation details and hooks such as post_install/source/skills/scope are not permitted)",
			config.Prog, strings.Join(unknown, ", "), path)
	}

	out := &existingConfig{vars: map[string]any{}}

	if v, ok := raw["profiles"]; ok {
		list, err := stringArray(v)
		if err != nil {
			return nil, fmt.Errorf("%s: \"profiles\" must be an array of strings in repo config: %s", config.Prog, path)
		}
		for _, name := range list {
			if !config.KebabCase(name) {
				return nil, fmt.Errorf("%s: \"profiles\" entry \"%s\" must match ^[a-z][a-z0-9]*(-[a-z0-9]+)*$ in repo config: %s", config.Prog, name, path)
			}
			if config.IsGlobalProfile(name) {
				return nil, fmt.Errorf("%s: \"global\" is a reserved profile keyword and cannot be selected in repo config: %s", config.Prog, path)
			}
		}
		out.profiles = list
	}

	if v, ok := raw["agents"]; ok {
		list, err := stringArray(v)
		if err != nil {
			return nil, fmt.Errorf("%s: \"agents\" must be an array of strings in repo config: %s", config.Prog, path)
		}
		trimmed := make([]string, 0, len(list))
		for _, name := range list {
			if strings.TrimSpace(name) == "" {
				return nil, fmt.Errorf("%s: \"agents\" entries must be non-empty strings in repo config: %s", config.Prog, path)
			}
			trimmed = append(trimmed, strings.TrimSpace(name))
		}
		out.agents = trimmed
	}

	if v, ok := raw["vars"]; ok {
		vars, isMap := v.(map[string]any)
		if !isMap {
			return nil, fmt.Errorf("%s: \"vars\" must be a table in repo config: %s", config.Prog, path)
		}
		for key, value := range vars {
			if isVarsValue(value) {
				continue
			}
			return nil, fmt.Errorf("%s: \"vars.%s\" must be a string, number, boolean, or array of strings (nested tables are not supported) in repo config: %s", config.Prog, key, path)
		}
		out.vars = vars
	}

	if v, ok := raw["mode"]; ok {
		s, isStr := v.(string)
		if !isStr || (s != "symlink" && s != "copy") {
			return nil, fmt.Errorf("%s: \"mode\" must be \"symlink\" or \"copy\" in repo config: %s", config.Prog, path)
		}
		out.mode = s
	}

	return out, nil
}

func stringArray(v any) ([]string, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("not an array")
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("not a string")
		}
		out = append(out, s)
	}
	return out, nil
}

func (e *existingConfig) isVarsValue(v any) bool {
	switch t := v.(type) {
	case bool, string, int64, float64:
		return true
	case []any:
		for _, item := range t {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func isVarsValue(v any) bool {
	return (&existingConfig{}).isVarsValue(v)
}

// --- rendering ---------------------------------------------------------------

func renderContent(profiles, agents []string, mode string, vars map[string]any) string {
	var b strings.Builder
	b.WriteString("# Managed by agent-env init. Profiles select which skill groups\n")
	b.WriteString("# agent-env installs into this repo. Safe to edit by hand.\n")
	b.WriteString("\n")
	b.WriteString("profiles = " + tomlArray(profiles) + "\n")
	if len(agents) > 0 {
		b.WriteString("agents = " + tomlArray(agents) + "\n")
	}
	if mode != "" {
		b.WriteString("mode = " + tomlQuote(mode) + "\n")
	}
	if len(vars) > 0 {
		b.WriteString("\n[vars]\n")
		keys := make([]string, 0, len(vars))
		for k := range vars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(tomlKey(k) + " = " + tomlValue(vars[k]) + "\n")
		}
	}
	return b.String()
}

func tomlQuote(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString("\\\\")
		case '"':
			b.WriteString("\\\"")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func tomlArray(items []string) string {
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = tomlQuote(item)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func tomlKey(key string) string {
	if isBareKey(key) {
		return key
	}
	return tomlQuote(key)
}

func isBareKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func tomlValue(value any) string {
	switch t := value.(type) {
	case bool:
		if t {
			return "true"
		}
		return "false"
	case string:
		return tomlQuote(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		s := strconv.FormatFloat(t, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eEnN") {
			s += ".0"
		}
		return s
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				parts = append(parts, tomlQuote(s))
			}
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return tomlQuote(fmt.Sprintf("%v", value))
	}
}

// --- helpers -----------------------------------------------------------------

func dedupeTrim(items []string) []string {
	out := []string{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = appendUniqueOne(out, item)
	}
	return out
}

func appendUniqueItems(list, items []string) []string {
	for _, item := range items {
		found := false
		for _, existing := range list {
			if existing == item {
				found = true
				break
			}
		}
		if !found {
			list = append(list, item)
		}
	}
	return list
}

func atomicWrite(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("%s: cannot create %s: %v", config.Prog, dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("%s: cannot create temp file in %s: %v", config.Prog, dir, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("%s: cannot write %s: %v", config.Prog, tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("%s: cannot write %s: %v", config.Prog, tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("%s: cannot replace %s: %v", config.Prog, path, err)
	}
	return nil
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func printCommand(w io.Writer, args ...string) {
	for _, a := range args {
		fmt.Fprintf(w, "%s ", zshQuote(a))
	}
	fmt.Fprintln(w)
}

const zshSafe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-"

func zshQuote(s string) string {
	if s == "" {
		return "''"
	}
	needs := false
	for _, r := range s {
		if !strings.ContainsRune(zshSafe, r) {
			needs = true
			break
		}
	}
	if !needs {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(zshSafe, r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('\\')
			b.WriteRune(r)
		}
	}
	return b.String()
}
func appendUniqueOne(list []string, item string) []string {
	for _, existing := range list {
		if existing == item {
			return list
		}
	}
	return append(list, item)
}

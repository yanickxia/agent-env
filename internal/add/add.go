// Package add implements `agent-env skills add NAME...` and
// `agent-env mcp add NAME...`: npm-install--save semantics for single entries.
// Each name is resolved against the global config (AGENT_ENV_CONFIG priority
// chain), recorded in the repo's .agent-env.toml `names` list, and installed
// by running the domain's normal apply. Matching is by the `name` field only;
// resolution happens for every argument before anything is modified
// (fail-fast, no partial changes).
package add

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/yanickxia/agent-env/internal/config"
	"github.com/yanickxia/agent-env/internal/mcp"
	"github.com/yanickxia/agent-env/internal/repoinit"
	"github.com/yanickxia/agent-env/internal/skills"
)

// Domain selects which manifest table names resolve against.
type Domain string

const (
	// DomainSkills resolves names against [[installs]] entries that carry a
	// name field (entries without one cannot be added yet).
	DomainSkills Domain = "skills"
	// DomainMCP resolves names against [[servers]] entries (all have names).
	DomainMCP Domain = "mcp"
)

// Options carries resolved paths and injectable IO. It is the union of the
// fields the skills and MCP apply runs need.
type Options struct {
	Domain         Domain
	ManifestPath   string
	SecretsPath    string // MCP apply only
	StatePath      string // skills apply only
	RepoConfigPath string
	ProjectRoot    string
	StartDir       string
	Home           string
	ClaudeJSON     string // MCP apply only

	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader

	LookupEnv func(string) string

	// InRepo reports a repo context (git toplevel found, or the repo config
	// path is pinned via AGENT_ENV_REPO_CONFIG). add is repo-only and fails
	// without it.
	InRepo bool
}

// Filters is the parsed CLI surface.
type Filters struct {
	Names  []string
	DryRun bool
	NoRepo bool
}

// Run executes one add command.
func Run(opts Options, f Filters) error {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.LookupEnv == nil {
		opts.LookupEnv = os.Getenv
	}
	if opts.Domain != DomainSkills && opts.Domain != DomainMCP {
		return fmt.Errorf("%s: add: unknown domain %q", config.Prog, opts.Domain)
	}

	if f.NoRepo {
		return outOfRepoError(opts, "add cannot be combined with --no-repo: there would be no repo config to record the name in")
	}
	if !opts.InRepo {
		return outOfRepoError(opts, "add needs a repo context (git toplevel or AGENT_ENV_REPO_CONFIG) to record the name in")
	}

	names := dedupeTrim(f.Names)
	if len(names) == 0 {
		return fmt.Errorf("%s: %s add requires at least one NAME", config.Prog, opts.Domain)
	}
	for _, name := range names {
		if config.IsGlobalProfile(name) {
			return fmt.Errorf("%s: \"global\" is a reserved keyword and cannot be used as a name", config.Prog)
		}
		if !config.KebabCase(name) {
			return fmt.Errorf("%s: invalid name '%s' (expected kebab-case matching ^[a-z][a-z0-9]*(-[a-z0-9]+)*$)", config.Prog, name)
		}
	}

	index, err := resolveIndex(opts)
	if err != nil {
		return err
	}

	// Resolve everything before touching anything (fail-fast, no partial
	// modifications).
	persist := []string{}
	globals := []string{}
	unknown := []string{}
	for _, name := range names {
		entry, ok := index[name]
		switch {
		case !ok:
			unknown = append(unknown, name)
		case entry:
			globals = append(globals, name)
		default:
			persist = append(persist, name)
		}
	}
	if len(unknown) > 0 {
		return unknownNameError(opts, unknown)
	}
	for _, name := range globals {
		fmt.Fprintf(opts.Stdout, "note: %s is global, installs everywhere; nothing to record\n", name)
	}

	repoConfigPath := opts.RepoConfigPath
	if repoConfigPath == "" {
		repoConfigPath = config.RepoConfigPath(opts.LookupEnv, opts.ProjectRoot)
	}

	var mergedContent string
	if len(persist) > 0 {
		content, _, _, changed, err := repoinit.MergeNames(repoConfigPath, persist)
		if err != nil {
			return err
		}
		mergedContent = content
		switch {
		case f.DryRun:
			fmt.Fprintf(opts.Stdout, "would write %s (names += %s)\n", repoConfigPath, strings.Join(persist, ", "))
			fmt.Fprint(opts.Stdout, content)
		case !changed:
			fmt.Fprintf(opts.Stdout, "names already recorded in %s; config unchanged\n", repoConfigPath)
		default:
			if err := repoinit.WriteAtomic(repoConfigPath, content); err != nil {
				return err
			}
			fmt.Fprintf(opts.Stdout, "wrote %s\n", repoConfigPath)
			fmt.Fprintf(opts.Stdout, "names: %s\n", strings.Join(persist, ", "))
		}
	}

	// Record + install: the persisted names go through the normal apply, so
	// new entries install and already-installed ones skip via stamps. Under
	// --dry-run the merged config is staged in a temp file instead of written,
	// so the dry-run apply preview includes the new names without touching the
	// real repo config.
	applyRepoConfig := opts.RepoConfigPath
	if f.DryRun && len(persist) > 0 {
		staged, err := stageRepoConfig(mergedContent)
		if err != nil {
			return err
		}
		defer os.Remove(staged)
		applyRepoConfig = staged
	}
	if err := runApply(opts, applyRepoConfig, f.DryRun); err != nil {
		return err
	}

	if len(persist) > 0 {
		verb := "added"
		if f.DryRun {
			verb = "would add"
		}
		fmt.Fprintf(opts.Stdout, "%s: %s -> %s\n", verb, strings.Join(persist, ", "), repoConfigPath)
	}
	return nil
}

// stageRepoConfig writes content to a throwaway file so a --dry-run apply can
// resolve the planned names against it.
func stageRepoConfig(content string) (string, error) {
	tmp, err := os.CreateTemp("", "agent-env-add-dry-run-*.toml")
	if err != nil {
		return "", fmt.Errorf("%s: add: cannot stage the dry-run repo config: %v", config.Prog, err)
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("%s: add: cannot stage the dry-run repo config: %v", config.Prog, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("%s: add: cannot stage the dry-run repo config: %v", config.Prog, err)
	}
	return tmp.Name(), nil
}

func runApply(opts Options, repoConfigPath string, dryRun bool) error {
	command := skills.CmdApply
	if dryRun {
		command = skills.CmdDryRun
	}
	switch opts.Domain {
	case DomainSkills:
		return skills.Run(skills.Options{
			ManifestPath:   opts.ManifestPath,
			StatePath:      opts.StatePath,
			RepoConfigPath: repoConfigPath,
			ProjectRoot:    opts.ProjectRoot,
			StartDir:       opts.StartDir,
			Home:           opts.Home,
			Stdout:         opts.Stdout,
			Stderr:         opts.Stderr,
		}, skills.Filters{
			Command:        command,
			NonInteractive: true,
			SkipUnchanged:  true,
		})
	case DomainMCP:
		return mcp.Run(mcp.Options{
			ManifestPath:   opts.ManifestPath,
			SecretsPath:    opts.SecretsPath,
			RepoConfigPath: repoConfigPath,
			ProjectRoot:    opts.ProjectRoot,
			Home:           opts.Home,
			ClaudeJSON:     opts.ClaudeJSON,
			Stdout:         opts.Stdout,
			Stderr:         opts.Stderr,
			Stdin:          opts.Stdin,
			LookupEnv:      opts.LookupEnv,
		}, mcp.Filters{
			Command:        command,
			NonInteractive: true,
		})
	}
	return fmt.Errorf("%s: add: unknown domain %q", config.Prog, opts.Domain)
}

// resolveIndex parses the global config and returns name -> isGlobal.
// Skills only indexes entries that carry a name field; MCP indexes all
// servers (every server must declare a name).
func resolveIndex(opts Options) (map[string]bool, error) {
	switch opts.Domain {
	case DomainSkills:
		entries, err := config.ParseManifest(opts.ManifestPath)
		if err != nil {
			return nil, err
		}
		index := map[string]bool{}
		for _, e := range entries {
			if e.Name != "" {
				index[e.Name] = e.Global
			}
		}
		return index, nil
	case DomainMCP:
		// Name/profile resolution never needs secret values; a no-op resolver
		// keeps ${VAR} placeholders from leaking anything into the index.
		servers, err := config.ParseServers(opts.ManifestPath, func(string) string { return "" })
		if err != nil {
			return nil, err
		}
		index := map[string]bool{}
		for _, s := range servers {
			index[s.Name] = s.Global
		}
		return index, nil
	}
	return nil, fmt.Errorf("%s: add: unknown domain %q", config.Prog, opts.Domain)
}

func availableNames(opts Options) []string {
	index, err := resolveIndex(opts)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(index))
	for name := range index {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func outOfRepoError(opts Options, detail string) error {
	return fmt.Errorf("%s: %s add only works inside a repository: %s\n"+
		"  record names by hand in <repo>/.agent-env.toml, or install global entries with: agent-env %s apply --no-repo",
		config.Prog, opts.Domain, detail, opts.Domain)
}

func unknownNameError(opts Options, unknown []string) error {
	noun := "skill"
	table := "[[installs]]"
	if opts.Domain == DomainMCP {
		noun = "server"
		table = "[[servers]]"
	}
	msg := fmt.Sprintf("%s: unknown %s name(s): %s", config.Prog, noun, strings.Join(unknown, ", "))
	if available := availableNames(opts); len(available) > 0 {
		msg += fmt.Sprintf("\n%s available for %s add:\n  %s", noun, opts.Domain, strings.Join(available, ", "))
	} else {
		msg += fmt.Sprintf("\nno named %s entries found in %s", noun, opts.ManifestPath)
	}
	if opts.Domain == DomainSkills {
		msg += fmt.Sprintf("\nentries without a name field cannot be added yet; add name = \"...\" to their %s entry in %s first", table, opts.ManifestPath)
	}
	return fmt.Errorf("%s", msg)
}

func dedupeTrim(items []string) []string {
	out := []string{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		found := false
		for _, existing := range out {
			if existing == item {
				found = true
				break
			}
		}
		if !found {
			out = append(out, item)
		}
	}
	return out
}

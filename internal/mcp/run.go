// Package mcp implements the MCP domain: parsing/selection of [[servers]],
// the codex/trae/opencode/claude writers, the aiden CLI path, and the
// chezmoi-facing upsert-stdin command. Behaviour mirrors the retired zsh
// agent-mcp-sync script.
package mcp

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yanickxia/agent-env/internal/config"
)

// Commands.
const (
	CmdApply       = "apply"
	CmdDryRun      = "dry-run"
	CmdList        = "list"
	CmdProfiles    = "profiles"
	CmdUpsertStdin = "upsert-stdin"
)

// Options carries resolved paths and injectable IO.
type Options struct {
	ManifestPath   string
	SecretsPath    string
	RepoConfigPath string
	ProjectRoot    string
	Home           string
	WorkDir        string
	ClaudeJSON     string

	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader

	LookupEnv func(string) string
}

// Filters is the normalized CLI filter set plus its seen flags.
type Filters struct {
	Command string

	Agents   []string
	Names    []string
	Profiles []string

	AgentSeen   bool
	ProfileSeen bool

	NonInteractive bool
	Interactive    bool
}

type runner struct {
	opts    Options
	f       Filters
	out     io.Writer
	errw    io.Writer
	stdin   io.Reader
	getenv  func(string) string
	secrets map[string]string
	values  []string // secret values, for redaction

	servers        []config.Server
	repo           *config.RepoConfig
	repoLayers     []string
	effectiveProf  string
	effectiveList  []string
	effectiveNames []string
}

func errUnsupportedUserAgent(agent string) error {
	return fmt.Errorf("%s: no user-level writer for agent '%s' (expected codex, trae, opencode, pi, omp)", config.Prog, agent)
}

// Run executes one mcp command.
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
	if opts.WorkDir == "" {
		if opts.ProjectRoot != "" {
			opts.WorkDir = opts.ProjectRoot
		} else if wd, err := os.Getwd(); err == nil {
			opts.WorkDir = wd
		}
	}
	r := &runner{opts: opts, f: f, out: opts.Stdout, errw: opts.Stderr, stdin: opts.Stdin, getenv: opts.LookupEnv}

	if err := r.normalize(); err != nil {
		return err
	}
	if r.f.Interactive && r.f.NonInteractive {
		return fmt.Errorf("%s: --interactive and --non-interactive cannot be used together", config.Prog)
	}
	if r.f.Interactive {
		return fmt.Errorf("%s: --interactive is not supported by agent-env (fzf selection was not ported); pass --non-interactive or explicit filters", config.Prog)
	}

	switch f.Command {
	case CmdList:
		return r.cmdList()
	case CmdProfiles:
		return r.cmdProfiles()
	case CmdUpsertStdin:
		return r.cmdUpsertStdin()
	case CmdApply, CmdDryRun:
		return r.cmdSync()
	default:
		return fmt.Errorf("%s: unknown mcp command %q", config.Prog, f.Command)
	}
}

func (r *runner) normalize() error {
	agents := []string{}
	for _, raw := range r.f.Agents {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			continue
		}
		agents = appendUnique(agents, config.CanonicalAgent(tok))
	}
	r.f.Agents = agents

	names := []string{}
	for _, raw := range r.f.Names {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			continue
		}
		names = appendUnique(names, tok)
	}
	r.f.Names = names

	profiles := []string{}
	for _, raw := range r.f.Profiles {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			continue
		}
		if !config.KebabCase(tok) {
			return fmt.Errorf("%s: invalid profile name '%s' (expected kebab-case, e.g. ark-mlops)", config.Prog, tok)
		}
		if config.IsGlobalProfile(tok) {
			return fmt.Errorf("%s: \"global\" is a reserved profile keyword and cannot be selected with --profile", config.Prog)
		}
		profiles = appendUnique(profiles, tok)
	}
	r.f.Profiles = profiles
	return nil
}

func appendUnique(list []string, item string) []string {
	for _, existing := range list {
		if existing == item {
			return list
		}
	}
	return append(list, item)
}

func (r *runner) anyFilterSeen() bool {
	return r.f.AgentSeen || r.f.ProfileSeen || len(r.f.Names) > 0
}

// loadSecrets parses secrets.toml once and caches redaction values.
func (r *runner) loadSecrets() error {
	if r.secrets != nil {
		return nil
	}
	secrets, err := config.ParseSecrets(r.opts.SecretsPath)
	if err != nil {
		return err
	}
	r.secrets = secrets
	for _, v := range secrets {
		if v != "" {
			r.values = append(r.values, v)
		}
	}
	return nil
}

func (r *runner) resolveSecret(key string) string {
	if key == "" {
		return ""
	}
	if v, ok := r.secrets[key]; ok {
		return v
	}
	if v, ok := r.secrets[strings.ToLower(key)]; ok {
		return v
	}
	return r.getenv(key)
}

func (r *runner) redact(text string) string {
	for _, v := range r.values {
		text = strings.ReplaceAll(text, v, "***redacted***")
	}
	return text
}

func (r *runner) loadServers() error {
	if r.servers != nil {
		return nil
	}
	if err := r.loadSecrets(); err != nil {
		return err
	}
	servers, err := config.ParseServers(r.opts.ManifestPath, r.resolveSecret)
	if err != nil {
		return err
	}
	r.servers = servers
	return nil
}

// loadRepo resolves the repo selection once. An explicit RepoConfigPath (or
// AGENT_ENV_REPO_CONFIG) is single-file mode; otherwise .agent-env.toml files
// are discovered from the working directory up to "/" and merged.
func (r *runner) loadRepo() error {
	start := r.opts.WorkDir
	if start == "" {
		start = r.opts.ProjectRoot
	}
	explicit := r.opts.RepoConfigPath
	if explicit == config.DefaultRepoConfigPath(r.opts.ProjectRoot) {
		explicit = ""
	}
	cfg, layers, found, err := config.LoadRepoSelection(
		explicit, start, r.getenv,
		"server definitions belong in the global manifest")
	if err != nil {
		return err
	}
	r.repoLayers = layers
	if found {
		r.repo = cfg
	}
	return nil
}

func (r *runner) computeEffectiveProfiles() {
	var selected []string
	if len(r.f.Profiles) > 0 {
		selected = r.f.Profiles
	} else if r.repo != nil {
		selected = r.repo.Profiles
	}
	unique := []string{}
	for _, item := range selected {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		unique = appendUnique(unique, item)
	}
	sort.Strings(unique)
	r.effectiveList = unique
	r.effectiveProf = strings.Join(unique, ",")

	names := []string{}
	if r.repo != nil {
		for _, n := range r.repo.Names {
			names = appendUnique(names, n)
		}
	}
	r.effectiveNames = names
}

func (r *runner) nameMatches(name string) bool {
	if len(r.f.Names) == 0 {
		return true
	}
	for _, n := range r.f.Names {
		if name == n {
			return true
		}
	}
	return false
}

func profilesIntersect(profiles []string, selection string) bool {
	sel := map[string]bool{}
	for _, tok := range strings.Split(selection, ",") {
		tok = strings.TrimSpace(tok)
		if tok != "" {
			sel[tok] = true
		}
	}
	for _, p := range profiles {
		p = strings.TrimSpace(p)
		if p != "" && sel[p] {
			return true
		}
	}
	return false
}

// --- read-only commands ------------------------------------------------------

func (r *runner) cmdList() error {
	if r.anyFilterSeen() {
		return fmt.Errorf("%s: filters are supported for apply and dry-run, not list", config.Prog)
	}
	data, err := os.ReadFile(r.opts.ManifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s: manifest not found: %s", config.Prog, r.opts.ManifestPath)
		}
		return fmt.Errorf("%s: cannot read manifest %s: %v", config.Prog, r.opts.ManifestPath, err)
	}
	_, err = r.out.Write(data)
	return err
}

func (r *runner) cmdProfiles() error {
	if r.anyFilterSeen() {
		return fmt.Errorf("%s: filters are supported for apply and dry-run, not profiles", config.Prog)
	}
	if _, err := os.Stat(r.opts.ManifestPath); err != nil {
		return fmt.Errorf("%s: manifest not found: %s", config.Prog, r.opts.ManifestPath)
	}
	if err := r.loadServers(); err != nil {
		return err
	}
	profiles := []string{}
	for _, s := range r.servers {
		for _, p := range s.Profiles {
			if config.IsGlobalProfile(p) {
				continue
			}
			profiles = appendUnique(profiles, p)
		}
	}
	if len(profiles) == 0 {
		return fmt.Errorf("%s: no selectable profiles declared in %s (\"global\" is a reserved keyword and is excluded)", config.Prog, r.opts.ManifestPath)
	}
	sort.Strings(profiles)
	for _, p := range profiles {
		fmt.Fprintln(r.out, p)
	}
	return nil
}

func (r *runner) cmdUpsertStdin() error {
	if len(r.f.Agents) != 1 {
		return fmt.Errorf("%s: upsert-stdin requires exactly one --agent AGENT", config.Prog)
	}
	if r.f.ProfileSeen || len(r.f.Names) > 0 {
		return fmt.Errorf("%s: upsert-stdin does not accept filters", config.Prog)
	}
	if _, err := os.Stat(r.opts.ManifestPath); err != nil {
		return fmt.Errorf("%s: manifest not found: %s", config.Prog, r.opts.ManifestPath)
	}
	agent := r.f.Agents[0]
	switch agent {
	case "codex", "trae", "trae-cn", "opencode", "pi", "omp":
	default:
		return errUnsupportedUserAgent(agent)
	}

	data, err := io.ReadAll(r.stdin)
	if err != nil {
		return fmt.Errorf("%s: cannot read stdin: %v", config.Prog, err)
	}
	if err := r.loadServers(); err != nil {
		return err
	}
	entries := r.userEntriesAll(agent)
	block := ""
	if len(entries) > 0 {
		block, err = r.renderUserBlock(agent, entries)
		if err != nil {
			return err
		}
	}
	out, err := agentUserTransform(string(data), agent, block)
	if err != nil {
		return err
	}
	_, err = io.WriteString(r.out, out)
	return err
}

// --- apply / dry-run ---------------------------------------------------------

func (r *runner) cmdSync() error {
	if r.f.Command == CmdApply && !r.f.NonInteractive {
		return fmt.Errorf("%s: interactive selection is not supported yet; pass --non-interactive or an explicit filter (--agent/--name/--profile)", config.Prog)
	}
	if err := r.loadServers(); err != nil {
		return err
	}
	if len(r.f.Agents) > 0 {
		if err := r.validateAgentFilters(); err != nil {
			return err
		}
	}
	if err := r.loadRepo(); err != nil {
		return err
	}
	r.computeEffectiveProfiles()
	return r.processManifest(r.f.Command)
}

func (r *runner) validateAgentFilters() error {
	known := []string{}
	for _, s := range r.servers {
		for _, a := range s.Agents {
			known = appendUnique(known, a)
		}
	}
	knownSet := map[string]bool{}
	for _, a := range known {
		knownSet[a] = true
	}
	unknown := []string{}
	for _, a := range r.f.Agents {
		if !knownSet[a] {
			unknown = append(unknown, a)
		}
	}
	if len(unknown) > 0 {
		msg := fmt.Sprintf("%s: unknown agent(s): %s", config.Prog, strings.Join(unknown, ", "))
		if len(known) > 0 {
			msg += fmt.Sprintf("\n%s: agents available: %s", config.Prog, strings.Join(known, ", "))
		} else {
			msg += fmt.Sprintf("\n%s: no MCP entries found", config.Prog)
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

type collected struct {
	traeProject     []config.Server
	codexProject    []config.Server
	opencodeProject []config.Server
	piProject       []config.Server
	ompProject      []config.Server
	traeUser        []config.Server
	codexUser       []config.Server
	opencodeUser    []config.Server
	piUser          []config.Server
	ompUser         []config.Server
	claudeUser      []config.Server
}

func addByName(list []config.Server, seen map[string]bool, s config.Server) []config.Server {
	if seen[s.Name] {
		return list
	}
	seen[s.Name] = true
	return append(list, s)
}

func (r *runner) processManifest(mode string) error {
	projectRoot := r.opts.ProjectRoot

	var c collected
	seen := map[string]map[string]bool{
		"trae-project": {}, "codex-project": {}, "opencode-project": {},
		"pi-project": {}, "omp-project": {},
		"trae-user": {}, "codex-user": {}, "opencode-user": {},
		"pi-user": {}, "omp-user": {}, "claude-user": {},
	}

	repoLevelSkips := 0
	entryCount := 0

	for _, row := range r.servers {
		if !r.nameMatches(row.Name) {
			continue
		}
		if !row.Global {
			named := row.Name != "" && containsString(r.effectiveNames, row.Name)
			profiled := len(row.Profiles) > 0 && profilesIntersect(row.Profiles, r.effectiveProf)
			if !named && !profiled {
				if r.effectiveProf == "" && len(r.effectiveNames) == 0 {
					repoLevelSkips++
				}
				continue
			}
		}

		agents := append([]string{}, row.Agents...)
		if len(r.f.Agents) > 0 {
			agents = intersectOrdered(agents, r.f.Agents)
			if len(agents) == 0 {
				continue
			}
		}
		if !row.Global && r.repo != nil && len(r.repo.Agents) > 0 {
			agents = intersectOrdered(agents, r.repo.Agents)
			if len(agents) == 0 {
				continue
			}
		}

		for _, agent := range agents {
			switch config.CanonicalAgent(agent) {
			case "codex":
				if row.Global {
					c.codexUser = addByName(c.codexUser, seen["codex-user"], row)
				} else {
					c.codexProject = addByName(c.codexProject, seen["codex-project"], row)
				}
			case "trae", "trae-cn":
				if row.Global {
					c.traeUser = addByName(c.traeUser, seen["trae-user"], row)
				} else {
					c.traeProject = addByName(c.traeProject, seen["trae-project"], row)
				}
			case "opencode":
				if row.Global {
					c.opencodeUser = addByName(c.opencodeUser, seen["opencode-user"], row)
				} else {
					c.opencodeProject = addByName(c.opencodeProject, seen["opencode-project"], row)
				}
			case "pi":
				if row.Global {
					c.piUser = addByName(c.piUser, seen["pi-user"], row)
				} else {
					c.piProject = addByName(c.piProject, seen["pi-project"], row)
				}
			case "omp":
				if row.Global {
					c.ompUser = addByName(c.ompUser, seen["omp-user"], row)
				} else {
					c.ompProject = addByName(c.ompProject, seen["omp-project"], row)
				}
			case "aiden":
				if err := r.handleAiden(mode, row); err != nil {
					return err
				}
			case "claude-code":
				if row.Global {
					c.claudeUser = addByName(c.claudeUser, seen["claude-user"], row)
				} else if err := r.handleClaudeProject(mode, row); err != nil {
					return err
				}
			case "":
				// no agent declared: nothing to do
			default:
				fmt.Fprintf(r.errw, "%s: unsupported agent '%s' for server '%s'; skipped\n", config.Prog, agent, row.Name)
			}
		}
		entryCount++
	}

	if repoLevelSkips > 0 {
		start := r.opts.WorkDir
		if start == "" {
			start = projectRoot
		}
		fmt.Fprintf(r.errw, "%s: no .agent-env.toml found from %s up to / and no --profile given; skipped %d repo-level servers (use agent-env init or --profile)\n",
			config.Prog, start, repoLevelSkips)
	}

	traeProjectPath := filepath.Join(projectRoot, ".trae", "traecli.yaml")
	codexProjectPath := filepath.Join(projectRoot, ".codex", "config.toml")
	opencodeProjectPath := filepath.Join(projectRoot, ".opencode", "opencode.jsonc")
	piProjectPath := filepath.Join(projectRoot, ".pi", "mcp.json")
	ompProjectPath := filepath.Join(projectRoot, ".omp", "mcp.json")

	if len(c.traeProject) > 0 {
		rendered := renderTrae(c.traeProject)
		if mode == CmdDryRun {
			fmt.Fprintf(r.out, "# %s: would write Trae project MCP config: %s\n", config.Prog, traeProjectPath)
			r.printBlock(rendered)
		} else if err := writeProjectBlock(traeProjectPath, rendered, traeBegin, traeEnd); err != nil {
			return err
		}
	}
	if len(c.codexProject) > 0 {
		rendered := renderCodex(c.codexProject)
		if mode == CmdDryRun {
			fmt.Fprintf(r.out, "# %s: would write Codex project MCP config: %s\n", config.Prog, codexProjectPath)
			r.printBlock(rendered)
		} else if err := writeProjectBlock(codexProjectPath, rendered, codexBegin, codexEnd); err != nil {
			return err
		}
	}
	if len(c.opencodeProject) > 0 {
		rendered := renderOpencode(c.opencodeProject)
		if mode == CmdDryRun {
			fmt.Fprintf(r.out, "# %s: would write OpenCode project MCP config: %s\n", config.Prog, opencodeProjectPath)
			r.printBlock(rendered)
		} else if err := writeOpencodeProjectBlock(opencodeProjectPath, rendered); err != nil {
			return err
		}
	}
	if len(c.piProject) > 0 {
		if err := r.writeMCPJSONTarget(mode, "pi", piProjectPath, "Pi project", c.piProject); err != nil {
			return err
		}
	}
	if len(c.ompProject) > 0 {
		if err := r.writeMCPJSONTarget(mode, "omp", ompProjectPath, "OMP project", c.ompProject); err != nil {
			return err
		}
	}

	// Zero active entries is success, not an error: the writer pipeline below
	// still runs with empty sets so stale managed blocks / claude mcpServers get
	// cleaned up. Manifest-level protection lives in the parser.
	if entryCount == 0 {
		fmt.Fprintf(r.errw, "%s: no active entries; nothing to install\n", config.Prog)
	}

	// User-scope writers always run so an empty selection cleans up stale
	// managed blocks; writeUserTarget is a no-op when the target does not exist
	// and the block is empty.
	if err := r.writeUserTarget(mode, "trae", filepath.Join(r.opts.Home, ".trae", "traecli.yaml"), r.userEntriesAll("trae")); err != nil {
		return err
	}
	if err := r.writeUserTarget(mode, "codex", filepath.Join(r.opts.Home, ".codex", "config.toml"), r.userEntriesAll("codex")); err != nil {
		return err
	}
	if err := r.writeUserTarget(mode, "opencode", filepath.Join(r.opts.Home, ".config", "opencode", "opencode.jsonc"), r.userEntriesAll("opencode")); err != nil {
		return err
	}

	// claude's user target is patched in place and always runs: an empty set
	// replaces the top-level mcpServers with {} (clearing any stale entries).
	rendered, err := renderClaudeUser(c.claudeUser, r.resolveSecret, func(m string) { fmt.Fprintln(r.errw, m) })
	if err != nil {
		return err
	}
	if mode == CmdDryRun {
		fmt.Fprintf(r.out, "# %s: would write Claude user MCP config: %s\n", config.Prog, r.opts.ClaudeJSON)
		r.printBlock(rendered)
	} else if err := r.patchClaude(rendered); err != nil {
		return err
	}

	// pi/omp user targets are whole-file mcpServers writers: like claude they
	// run even with an empty set so a stale mcpServers gets cleaned up, but a
	// missing target is not created when there is nothing to write.
	if err := r.writeMCPJSONTarget(mode, "pi", filepath.Join(r.opts.Home, ".pi", "agent", "mcp.json"), userTargetLabel("pi"), c.piUser); err != nil {
		return err
	}
	if err := r.writeMCPJSONTarget(mode, "omp", filepath.Join(r.opts.Home, ".omp", "agent", "mcp.json"), userTargetLabel("omp"), c.ompUser); err != nil {
		return err
	}

	return nil
}

func intersectOrdered(declared, filter []string) []string {
	out := []string{}
	for _, item := range declared {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		for _, f := range filter {
			if item == config.CanonicalAgent(strings.TrimSpace(f)) {
				out = append(out, item)
				break
			}
		}
	}
	return out
}

// userEntriesAll is the unfiltered global-entry collection shared with
// upsert-stdin (agent_user_transform semantics): entries whose profiles
// contain the reserved "global" keyword.
func (r *runner) userEntriesAll(agent string) []config.Server {
	out := []config.Server{}
	seen := map[string]bool{}
	for _, s := range r.servers {
		if !s.Global {
			continue
		}
		if !containsString(s.Agents, agent) {
			continue
		}
		if seen[s.Name] {
			continue
		}
		seen[s.Name] = true
		out = append(out, s)
	}
	return out
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func (r *runner) renderUserBlock(agent string, entries []config.Server) (string, error) {
	switch agent {
	case "codex":
		return renderCodex(entries), nil
	case "trae", "trae-cn":
		return renderTrae(entries), nil
	case "opencode":
		return renderOpencode(entries), nil
	case "pi", "omp":
		return renderPiOMP(entries, r.resolveSecret, func(m string) { fmt.Fprintln(r.errw, m) }, agent)
	default:
		return "", errUnsupportedUserAgent(agent)
	}
}

func (r *runner) writeUserTarget(mode, agent, target string, entries []config.Server) error {
	existed := false
	input := ""
	if fi, err := os.Stat(target); err == nil && !fi.IsDir() {
		existed = true
		input = readFileOrEmpty(target)
	}
	block := ""
	if len(entries) > 0 {
		var err error
		block, err = r.renderUserBlock(agent, entries)
		if err != nil {
			return err
		}
	}

	if mode == CmdDryRun {
		transformed, err := agentUserTransform(input, agent, block)
		if err != nil {
			return err
		}
		fmt.Fprintf(r.out, "# %s: would write %s MCP config: %s\n", config.Prog, userTargetLabel(agent), target)
		r.printBlock(transformed)
		return nil
	}

	// Nothing to clean up and nothing to write: don't create an empty target.
	if !existed && block == "" {
		return nil
	}

	transformed, err := agentUserTransform(input, agent, block)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("%s: cannot create %s: %v", config.Prog, filepath.Dir(target), err)
	}
	if err := os.WriteFile(target, []byte(transformed), 0o644); err != nil {
		return fmt.Errorf("%s: cannot write %s: %v", config.Prog, target, err)
	}
	if !existed && agent == "codex" {
		if err := os.Chmod(target, 0o600); err != nil {
			return fmt.Errorf("%s: cannot chmod %s: %v", config.Prog, target, err)
		}
	}
	return nil
}

// writeMCPJSONTarget writes a pi/omp mcp.json by replacing the top-level
// "mcpServers" member, preserving any other top-level members. It runs even
// with an empty set (so a stale mcpServers gets cleaned up), but a missing
// target is not created when there is nothing to write. label names the
// target in dry-run output.
func (r *runner) writeMCPJSONTarget(mode, agent, target, label string, entries []config.Server) error {
	existed := false
	if fi, err := os.Stat(target); err == nil && !fi.IsDir() {
		existed = true
	}
	if !existed && len(entries) == 0 {
		return nil
	}
	rendered, err := r.renderUserBlock(agent, entries)
	if err != nil {
		return err
	}

	if mode == CmdDryRun {
		transformed := string(patchMCPJSON([]byte(readFileOrEmpty(target)), rendered))
		fmt.Fprintf(r.out, "# %s: would write %s MCP config: %s\n", config.Prog, label, target)
		r.printBlock(transformed)
		return nil
	}

	raw := []byte(readFileOrEmpty(target))
	out := patchMCPJSON(raw, rendered)
	if err := writePreservingMode(target, out, 0o600); err != nil {
		return err
	}
	return nil
}

func (r *runner) patchClaude(rendered string) error {
	target := r.opts.ClaudeJSON
	if target == "" {
		target = config.ClaudeJSONPath(r.getenv, r.opts.Home)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(r.errw, "%s: %s not found, skip\n", config.Prog, target)
			return nil
		}
		return fmt.Errorf("%s: cannot read %s: %v", config.Prog, target, err)
	}
	out := patchClaudeJSON(raw, rendered)
	if err := os.WriteFile(target, out, 0o600); err != nil {
		return fmt.Errorf("%s: cannot write %s: %v", config.Prog, target, err)
	}
	count := countTopLevelKeys(rendered)
	fmt.Fprintf(r.out, "%s: patched mcpServers in %s (%d servers)\n", config.Prog, target, count)
	return nil
}

func countTopLevelKeys(rendered string) int {
	return scanTopLevelKeyCount(rendered)
}

func userTargetLabel(agent string) string {
	switch agent {
	case "codex":
		return "Codex user"
	case "trae", "trae-cn":
		return "Trae user"
	case "opencode":
		return "OpenCode user"
	case "pi":
		return "Pi user"
	case "omp":
		return "OMP user"
	default:
		return agent + " user"
	}
}

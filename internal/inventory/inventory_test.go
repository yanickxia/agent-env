package inventory

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type lsFixture struct {
	t        *testing.T
	dir      string
	home     string
	manifest string
	repo     string
	out      bytes.Buffer
	errb     bytes.Buffer
	opts     Options
}

func newLsFixture(t *testing.T) *lsFixture {
	t.Helper()
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	f := &lsFixture{
		t:        t,
		dir:      dir,
		home:     home,
		manifest: filepath.Join(dir, "config.toml"),
		repo:     filepath.Join(dir, ".agent-env.toml"),
	}
	f.opts = Options{
		ManifestPath:   f.manifest,
		RepoConfigPath: f.repo,
		ProjectRoot:    dir,
		StartDir:       dir,
		Home:           home,
		Stdout:         &f.out,
		Stderr:         &f.errb,
		LookupEnv:      func(string) string { return "" },
	}
	return f
}

func (f *lsFixture) write(path, body string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// installSkill creates a skills directory entry with a SKILL.md.
func (f *lsFixture) installSkill(dir, name string) {
	f.write(filepath.Join(dir, name, "SKILL.md"), "---\nname: "+name+"\n---\nbody\n")
}

func (f *lsFixture) run(filters Filters) error {
	f.out.Reset()
	f.errb.Reset()
	return Run(f.opts, filters)
}

func (f *lsFixture) stdout() string { return f.out.String() }

// --- scan: skills directories -------------------------------------------------

func TestListSkillsDirRequiresSkillMD(t *testing.T) {
	f := newLsFixture(t)
	dir := filepath.Join(f.dir, "skills")
	f.installSkill(dir, "real-skill")
	f.write(filepath.Join(dir, "no-skill-md", "other.txt"), "x")
	// hidden entries never count
	f.installSkill(dir, ".hidden")
	f.write(filepath.Join(dir, ".system", "SKILL.md"), "x")

	got := listSkillsDir(dir)
	want := []string{"real-skill"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("listSkillsDir = %v, want %v", got, want)
	}
	if listSkillsDir(filepath.Join(f.dir, "missing")) != nil {
		t.Fatal("missing dir must yield nil")
	}
}

func TestListSkillsDirFollowsSymlinks(t *testing.T) {
	f := newLsFixture(t)
	store := filepath.Join(f.dir, "store")
	f.installSkill(store, "stored-skill")
	linkDir := filepath.Join(f.dir, "linked")
	if err := os.Symlink(store, linkDir); err != nil {
		t.Fatal(err)
	}
	got := listSkillsDir(linkDir)
	if len(got) != 1 || got[0] != "stored-skill" {
		t.Fatalf("symlinked dir = %v, want [stored-skill]", got)
	}
}

// --- scan: MCP config parsing ---------------------------------------------------

func TestCodexManagedTablesOnlyInsideMarker(t *testing.T) {
	text := `# AGENT_MCP_CODEX_MANAGED_BEGIN
[mcp_servers.hindsight]
url = "https://example/mcp"

[mcp_servers.second]
command = "npx"
# AGENT_MCP_CODEX_MANAGED_END

[mcp_servers.outside]
command = "hand-written"
`
	got := codexManagedTables(text)
	if len(got) != 2 || got[0] != "hindsight" || got[1] != "second" {
		t.Fatalf("codexManagedTables = %v", got)
	}

	// No markers: nothing managed.
	if got := codexManagedTables("[mcp_servers.x]\ncommand = \"npx\"\n"); len(got) != 0 {
		t.Fatalf("unmarked codex config counted %v", got)
	}
}

func TestOpenCodeManagedKeys(t *testing.T) {
	text := `{
  "plugin": ["oh-my-opencode-slim@latest"],
  // AGENT_MCP_OPENCODE_MANAGED_BEGIN
  "mcp": {
    "hindsight": {
      "type": "remote",
      "url": "https://example/mcp"
    },
    "second": { "type": "local", "command": ["npx"] }
  }
  // AGENT_MCP_OPENCODE_MANAGED_END
}
`
	got := opencodeManagedKeys(text)
	if len(got) != 2 || got[0] != "hindsight" || got[1] != "second" {
		t.Fatalf("opencodeManagedKeys = %v", got)
	}
	if got := opencodeManagedKeys("{\"mcp\": {\"x\": {}}}"); got != nil {
		t.Fatalf("unmarked opencode config counted %v", got)
	}
}

func TestTopLevelObjectKeysInMemberClaude(t *testing.T) {
	text := `{
  "mcpServers": {
    "hindsight": {"type": "http", "url": "https://a"},
    "browseros": {"type": "http", "url": "https://b"}
  },
  "otherTopLevel": {"nested": {"deep": "value"}},
  "autoUpdates": true
}`
	got := topLevelObjectKeysInMember(text, "mcpServers")
	if len(got) != 2 || got[0] != "hindsight" || got[1] != "browseros" {
		t.Fatalf("mcpServers keys = %v", got)
	}

	// JSONC comments and escapes tolerated.
	jsonc := "{\n  // comment\n  \"mcpServers\": {\"a b\": {}, \"céd\": {}}\n}"
	got = topLevelObjectKeysInMember(jsonc, "mcpServers")
	if len(got) != 2 || got[0] != "a b" || got[1] != "céd" {
		t.Fatalf("jsonc mcpServers keys = %v", got)
	}
}

func TestTopLevelObjectKeysInMemberMissing(t *testing.T) {
	if got := topLevelObjectKeysInMember(`{"other": {}}`, "mcpServers"); got != nil {
		t.Fatalf("missing member counted %v", got)
	}
	if got := topLevelObjectKeysInMember(`not json`, "mcpServers"); got != nil {
		t.Fatalf("invalid json counted %v", got)
	}
}

// --- gating: declared sets -------------------------------------------------------

const gatingManifest = `
[[installs]]
source = "global/one"
agents = ["codex", "claude-code", "opencode"]
skills = ["alpha"]
profiles = ["global"]

[[installs]]
source = "group/two"
agents = ["codex", "claude-code", "opencode"]
skills = ["beta"]
profiles = ["base"]

[[installs]]
source = "name/three"
name = "clickup"
agents = ["codex", "claude-code", "opencode"]
skills = ["gamma"]

[[servers]]
name = "global-server"
agents = ["codex", "claude-code", "opencode"]
type = "stdio"
command = "npx"
args = ["-y", "x"]
profiles = ["global"]

[[servers]]
name = "group-server"
agents = ["codex", "claude-code", "opencode"]
type = "stdio"
command = "npx"
args = ["-y", "x"]
profiles = ["base"]

[[servers]]
name = "named-server"
agents = ["claude-code"]
type = "stdio"
command = "npx"
args = ["-y", "x"]
`

func TestDeclaredGatingByProfile(t *testing.T) {
	f := newLsFixture(t)
	f.write(f.manifest, gatingManifest)
	f.write(f.repo, "profiles = [\"base\"]\n")

	if err := f.run(Filters{}); err != nil {
		t.Fatal(err)
	}
	out := f.stdout()

	// Repo scope: group entry beta is declared, global alpha is not.
	if !strings.Contains(out, "Codex (repo):") {
		t.Fatalf("missing repo section:\n%s", out)
	}
	// Default view (no --declared) shows installed only, and nothing is
	// installed: repo sections must be absent entirely.
	if strings.Contains(out, "alpha") || strings.Contains(out, "beta") {
		t.Fatalf("default view leaked declared names:\n%s", out)
	}

	// --declared shows beta (declared) in repo scope, alpha never there.
	if err := f.run(Filters{Declared: true}); err != nil {
		t.Fatal(err)
	}
	out = f.stdout()
	repo := sectionOf(out, "Codex (repo):")
	if !strings.Contains(repo, "beta  (not installed)") {
		t.Fatalf("repo section missing declared beta:\n%s", repo)
	}
	if strings.Contains(repo, "alpha") {
		t.Fatalf("global skill alpha leaked into repo section:\n%s", repo)
	}
	if !strings.Contains(repo, "group-server  (not installed)") {
		t.Fatalf("repo section missing declared group-server:\n%s", repo)
	}
	if strings.Contains(repo, "global-server") || strings.Contains(repo, "named-server") {
		t.Fatalf("global/named servers leaked into repo section:\n%s", repo)
	}

	global := sectionOf(f.stdout(), "Codex (global):")
	if !strings.Contains(global, "alpha  (not installed)") {
		t.Fatalf("global section missing declared alpha:\n%s", global)
	}
	if !strings.Contains(global, "global-server  (not installed)") {
		t.Fatalf("global section missing declared global-server:\n%s", global)
	}
	if strings.Contains(global, "beta") || strings.Contains(global, "group-server") {
		t.Fatalf("repo-level names leaked into global section:\n%s", global)
	}
}

func TestDeclaredGatingByNameAndAgentNarrowing(t *testing.T) {
	f := newLsFixture(t)
	f.write(f.manifest, gatingManifest)
	// names 点名跨域命中：clickup（[[installs]]）与 named-server（[[servers]]，
	// 游离条目，仅 agents = claude-code）。repo agents 不含 claude-code 时，
	// claude 专属 server 被收窄掉（mcp 语义：条目 agents ∩ repo agents）。
	f.write(f.repo, "names = [\"clickup\"]\nagents = [\"codex\"]\n")

	if err := f.run(Filters{Declared: true}); err != nil {
		t.Fatal(err)
	}
	out := f.stdout()

	codexRepo := sectionOf(out, "Codex (repo):")
	if !strings.Contains(codexRepo, "gamma  (not installed)") {
		t.Fatalf("named entry gamma missing from codex repo:\n%s", codexRepo)
	}
	if strings.Contains(out, "named-server") {
		t.Fatalf("claude-only named-server survived the codex repo-agents narrowing:\n%s", out)
	}
	// gamma targets all providers, so it appears for claude too.
	claudeRepo := sectionOf(out, "Claude Code (repo):")
	if !strings.Contains(claudeRepo, "gamma  (not installed)") {
		t.Fatalf("named entry gamma missing from claude repo:\n%s", claudeRepo)
	}

	// With claude-code re-added to the repo agents, the named server crosses
	// the profile gate via names and lands in the claude repo section only.
	f.write(f.repo, "names = [\"named-server\"]\nagents = [\"codex\", \"claude-code\"]\n")
	if err := f.run(Filters{Declared: true}); err != nil {
		t.Fatal(err)
	}
	out = f.stdout()
	claudeRepo = sectionOf(out, "Claude Code (repo):")
	if !strings.Contains(claudeRepo, "named-server  (not installed)") {
		t.Fatalf("named server missing from claude repo after name selection:\n%s", claudeRepo)
	}
	codexRepo = sectionOf(out, "Codex (repo):")
	if strings.Contains(codexRepo, "named-server") {
		t.Fatalf("named-server leaked to codex (its agents list has no codex):\n%s", codexRepo)
	}
}

func TestNoSelectionShowsNoRepoDeclared(t *testing.T) {
	f := newLsFixture(t)
	f.write(f.manifest, gatingManifest)
	// No repo config at all: repo sections only appear when something is
	// installed; nothing is, so only global sections render.
	if err := f.run(Filters{Declared: true}); err != nil {
		t.Fatal(err)
	}
	out := f.stdout()
	if strings.Contains(out, "(repo):") {
		t.Fatalf("repo sections rendered without selection or installs:\n%s", out)
	}
	if !strings.Contains(out, "alpha  (not installed)") {
		t.Fatalf("global declared missing:\n%s", out)
	}
}

func TestNoRepoHidesRepoScope(t *testing.T) {
	f := newLsFixture(t)
	f.write(f.manifest, gatingManifest)
	f.write(f.repo, "profiles = [\"base\"]\n")

	if err := f.run(Filters{NoRepo: true, Declared: true}); err != nil {
		t.Fatal(err)
	}
	out := f.stdout()
	if strings.Contains(out, "(repo):") {
		t.Fatalf("--no-repo still rendered repo sections:\n%s", out)
	}
	if !strings.Contains(out, "alpha  (not installed)") {
		t.Fatalf("global declared missing under --no-repo:\n%s", out)
	}
}

// --- merged view: installed + declared ------------------------------------------

func TestMergedViewStaleAndMissingAnnotations(t *testing.T) {
	f := newLsFixture(t)
	f.write(f.manifest, gatingManifest)
	f.write(f.repo, "profiles = [\"base\"]\n")

	// Install: beta (declared) + stale-skill (no declaration) for codex repo
	// scope; alpha (declared) in codex global scope.
	codexRepoSkills := filepath.Join(f.dir, ".agents", "skills")
	f.installSkill(codexRepoSkills, "beta")
	f.installSkill(codexRepoSkills, "stale-skill")
	codexGlobalSkills := filepath.Join(f.home, ".codex", "skills")
	f.installSkill(codexGlobalSkills, "alpha")

	if err := f.run(Filters{Declared: true}); err != nil {
		t.Fatal(err)
	}
	out := f.stdout()

	repo := sectionOf(out, "Codex (repo):")
	if !strings.Contains(repo, "beta\n") {
		t.Fatalf("installed beta must have no annotation:\n%s", repo)
	}
	if !strings.Contains(repo, "stale-skill  (no active declaration)") {
		t.Fatalf("stale annotation missing:\n%s", repo)
	}

	global := sectionOf(out, "Codex (global):")
	if !strings.Contains(global, "alpha\n") {
		t.Fatalf("installed alpha must have no annotation:\n%s", global)
	}

	// Default view: annotations suppressed, stale still listed (installed).
	if err := f.run(Filters{}); err != nil {
		t.Fatal(err)
	}
	out = f.stdout()
	if strings.Contains(out, "(no active declaration)") || strings.Contains(out, "(not installed)") {
		t.Fatalf("default view leaked annotations:\n%s", out)
	}
	repo = sectionOf(out, "Codex (repo):")
	if !strings.Contains(repo, "stale-skill") || !strings.Contains(repo, "beta") {
		t.Fatalf("default view missing installed skills:\n%s", repo)
	}
}

// --- MCP installed view -----------------------------------------------------------

func TestMCPInstalledFromDiskPerFormat(t *testing.T) {
	f := newLsFixture(t)
	f.write(f.manifest, gatingManifest)

	f.write(filepath.Join(f.home, ".codex", "config.toml"), `# AGENT_MCP_CODEX_MANAGED_BEGIN
[mcp_servers.hindsight]
url = "https://example/mcp"
# AGENT_MCP_CODEX_MANAGED_END
[mcp_servers.unmanaged]
command = "npx"
`)
	f.write(filepath.Join(f.home, ".claude.json"), `{
  "mcpServers": {"hindsight": {"type": "http"}},
  "runtimeKey": 42
}`)
	f.write(filepath.Join(f.home, ".config", "opencode", "opencode.jsonc"), `{
  // AGENT_MCP_OPENCODE_MANAGED_BEGIN
  "mcp": {"hindsight": {"type": "remote"}}
  // AGENT_MCP_OPENCODE_MANAGED_END
}`)

	if err := f.run(Filters{}); err != nil {
		t.Fatal(err)
	}
	out := f.stdout()
	for _, section := range []string{"Codex (global):", "Claude Code (global):", "OpenCode (global):"} {
		s := sectionOf(out, section)
		if !strings.Contains(s+"\n", "mcp:\n    hindsight\n") {
			t.Fatalf("%s missing mcp hindsight:\n%s", section, s)
		}
	}
	if strings.Contains(out, "unmanaged") {
		t.Fatalf("unmanaged codex server leaked:\n%s", out)
	}
}

func TestMCPProjectScopeCodexAndClaude(t *testing.T) {
	f := newLsFixture(t)
	f.write(f.manifest, gatingManifest)
	f.write(f.repo, "profiles = [\"base\"]\n")

	f.write(filepath.Join(f.dir, ".codex", "config.toml"), `# AGENT_MCP_CODEX_MANAGED_BEGIN
[mcp_servers.repo-server]
command = "npx"
# AGENT_MCP_CODEX_MANAGED_END
`)
	f.write(filepath.Join(f.dir, ".mcp.json"), `{"mcpServers": {"claude-repo": {"type": "stdio"}}}`)

	if err := f.run(Filters{}); err != nil {
		t.Fatal(err)
	}
	out := f.stdout()
	if !strings.Contains(sectionOf(out, "Codex (repo):"), "repo-server") {
		t.Fatalf("codex project server missing:\n%s", out)
	}
	if !strings.Contains(sectionOf(out, "Claude Code (repo):"), "claude-repo") {
		t.Fatalf("claude project server missing:\n%s", out)
	}
}

// --- wildcard sources resolve via lock --------------------------------------------

func TestWildcardSourceResolvesFromLock(t *testing.T) {
	f := newLsFixture(t)
	f.write(f.manifest, `
[[installs]]
source = "yanickxia/skills"
agents = ["codex"]
skills = ["*"]
profiles = ["global"]
`)
	// Global lock maps the source to two concrete skills; write it in the
	// skills CLI's on-disk shape (read back via skills.ReadLock).
	f.write(filepath.Join(f.home, ".agents", ".skill-lock.json"), `{
  "version": 3,
  "skills": {
    "brave-search": {"source": "yanickxia/skills", "sourceUrl": "https://github.com/yanickxia/skills.git"},
    "youcom-search": {"source": "yanickxia/skills", "sourceUrl": "https://github.com/yanickxia/skills.git"},
    "unrelated": {"source": "other/repo", "sourceUrl": ""}
  }
}`)
	f.installSkill(filepath.Join(f.home, ".codex", "skills"), "brave-search")

	if err := f.run(Filters{Declared: true}); err != nil {
		t.Fatal(err)
	}
	out := sectionOf(f.stdout(), "Codex (global):")
	if !strings.Contains(out, "brave-search  (wildcard source)") {
		t.Fatalf("installed wildcard skill missing annotation:\n%s", out)
	}
	if !strings.Contains(out, "youcom-search  (not installed)") {
		t.Fatalf("wildcard skill not resolved from lock:\n%s", out)
	}
	if strings.Contains(out, "unrelated") {
		t.Fatalf("unrelated lock skill leaked:\n%s", out)
	}
}

// --- providers filter ---------------------------------------------------------------

func TestProviderFilterAndValidation(t *testing.T) {
	f := newLsFixture(t)
	f.write(f.manifest, gatingManifest)

	if err := f.run(Filters{Providers: []string{"claude"}}); err != nil {
		t.Fatal(err)
	}
	out := f.stdout()
	if strings.Contains(out, "Codex") || strings.Contains(out, "OpenCode") {
		t.Fatalf("provider filter leaked other providers:\n%s", out)
	}
	if !strings.Contains(out, "Claude Code") {
		t.Fatalf("claude alias did not render claude-code:\n%s", out)
	}

	if err := f.run(Filters{Providers: []string{"bogus"}}); err == nil {
		t.Fatal("bogus provider must fail")
	}
}

// --- claude symlink notice -------------------------------------------------------------

func TestClaudeSymlinkNotice(t *testing.T) {
	f := newLsFixture(t)
	f.write(f.manifest, gatingManifest)

	store := filepath.Join(f.home, ".agents", "skills")
	f.installSkill(store, "alpha")
	if err := os.MkdirAll(filepath.Join(f.home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(store, filepath.Join(f.home, ".claude", "skills")); err != nil {
		t.Fatal(err)
	}

	if err := f.run(Filters{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.errb.String(), "is a symlink to the skills store") {
		t.Fatalf("symlink notice missing from stderr:\n%s", f.errb.String())
	}
	// The store contents are still listed (symlink followed).
	if !strings.Contains(f.stdout(), "alpha") {
		t.Fatalf("store skills not listed through symlink:\n%s", f.stdout())
	}
}

// sectionOf returns the text from the "Header:" line to the next section
// header line ("Word (scope):") or EOF.
func sectionOf(out, header string) string {
	lines := strings.Split(out, "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, header) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if strings.HasSuffix(l, ":") && strings.Contains(l, " (") && l != "skills:" && l != "mcp:" {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

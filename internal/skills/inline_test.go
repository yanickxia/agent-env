package skills

import (
	"path/filepath"
	"strings"
	"testing"
)

const inlineSkills = `
agents = ["codex"]
mode = "copy"
[[installs]]
name = "local"
source = "./local-skills"
skills = ["local-skill"]
`

func TestInlineSkillsSelectionAndFilters(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    Filters
		want bool
	}{
		{"automatic", Filters{Command: CmdDryRun}, true},
		{"profile does not hide inline", Filters{Command: CmdDryRun, Profiles: []string{"other"}, ProfileSeen: true}, true},
		{"matching agent", Filters{Command: CmdDryRun, Agents: []string{"codex"}, AgentSeen: true}, true},
		{"other agent", Filters{Command: CmdDryRun, Agents: []string{"opencode"}, AgentSeen: true}, false},
		{"matching skill", Filters{Command: CmdDryRun, Skills: []string{"local-skill"}, SkillSeen: true}, true},
		{"other skill", Filters{Command: CmdDryRun, Skills: []string{"other"}, SkillSeen: true}, false},
		{"no repo", Filters{Command: CmdDryRun, NoRepo: true}, false},
		{"resolve", Filters{Command: CmdResolve}, true},
		{"status", Filters{Command: CmdStatus}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.writeRepo(inlineSkills)
			f.writeManifest(`
[[installs]]
source = "example/global"
agents = ["codex"]
skills = ["global-skill"]
profiles = ["global"]
[[installs]]
source = "example/unselected"
agents = ["codex"]
skills = ["unselected"]
profiles = ["base"]
`)
			out, errb, err := f.run(tc.f)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out, "local-skill") != tc.want || strings.Contains(out, "example/unselected") || strings.Contains(errb, "no .agent-env.toml") {
				t.Fatalf("selection wrong: out=%s stderr=%s", out, errb)
			}
			if tc.name == "automatic" {
				if !strings.Contains(out, "example/global -g") || !strings.Contains(out, "local-skills --copy -a codex") {
					t.Fatalf("global/local scope or inherited defaults wrong: %s", out)
				}
			}
		})
	}
}

func TestInlineSkillsApplyAndStampsWithoutGlobalManifest(t *testing.T) {
	f := newFixture(t)
	f.writeRepo(inlineSkills)
	log := filepath.Join(f.dir, "npx.log")
	t.Setenv("NPX_LOG", log)
	filters := Filters{Command: CmdApply, NonInteractive: true, SkipUnchanged: true}
	if _, _, err := f.run(filters); err != nil {
		t.Fatal(err)
	}
	calls := readFileOrEmpty(t, log)
	if !strings.HasPrefix(calls, f.dir+"\t") || strings.Contains(calls, " -g ") || !strings.Contains(calls, filepath.Join(f.dir, "local-skills")) || !strings.Contains(calls, "--copy") {
		t.Fatalf("must install into project from config-relative source: %s", calls)
	}
	status, _, err := f.run(Filters{Command: CmdStatus})
	if err != nil || !strings.Contains(status, "stamp: unchanged") || !strings.Contains(status, "config: "+f.repo) {
		t.Fatalf("status=%s err=%v", status, err)
	}
	if _, _, err := f.run(filters); err != nil || lineCount(t, log) != 1 {
		t.Fatalf("unchanged inline entry should skip: err=%v calls=%s", err, readFileOrEmpty(t, log))
	}
	f.writeRepo(strings.Replace(inlineSkills, `mode = "copy"`, `mode = "symlink"`, 1))
	status, _, err = f.run(Filters{Command: CmdStatus})
	if err != nil || !strings.Contains(status, "stamp: not installed (or changed)") {
		t.Fatalf("changed inline defaults should invalidate stamp: %s %v", status, err)
	}
	if _, _, err := f.run(filters); err != nil || lineCount(t, log) != 2 {
		t.Fatalf("changed inline entry should reinstall: err=%v calls=%s", err, readFileOrEmpty(t, log))
	}
	opts := f.opts
	opts.Force = true
	if _, _, err := f.runWith(opts, filters); err != nil || lineCount(t, log) != 3 {
		t.Fatalf("force must reinstall inline entries: err=%v calls=%s", err, readFileOrEmpty(t, log))
	}
}

func TestPruneKeepsInlineSkillsWithoutGlobalManifest(t *testing.T) {
	f := newFixture(t)
	f.writeRepo(inlineSkills)
	source := filepath.Join(f.dir, "local-skills")
	storeDir := filepath.Join(f.dir, ".agents", "skills")
	mkdirs(t, storeDir, "local-skill", "retired-skill")
	writeLockFile(t, filepath.Join(f.dir, "skills-lock.json"), map[string][]string{
		source: {"local-skill"}, "example/retired": {"retired-skill"},
	})
	f.write(f.state, source+"\tproject:"+f.dir+"\tOLD\nexample/retired\tproject:"+f.dir+"\tOLD\n")
	opts := f.opts
	opts.Prune = true
	// CLI narrowing must not turn an active inline entry into a prune target.
	for _, cmd := range []string{CmdDryRun, CmdApply} {
		out, _, err := f.runWith(opts, Filters{Command: cmd, NonInteractive: true, Skills: []string{"other"}, SkillSeen: true})
		if err != nil || strings.Contains(out, "prune: local-skill") || strings.Contains(out, "pruned: local-skill") {
			t.Fatalf("prune retired an active inline entry: %s %v", out, err)
		}
		mustExist(t, filepath.Join(storeDir, "local-skill"))
		if cmd == CmdDryRun {
			mustExist(t, filepath.Join(storeDir, "retired-skill"))
		} else {
			mustNotExist(t, filepath.Join(storeDir, "retired-skill"))
		}
	}
	if !strings.Contains(readFileOrEmpty(t, f.state), source+"\tproject:") {
		t.Fatal("prune removed active inline stamp")
	}
}

func TestInlineSkillsOverrideNamedProjectEntry(t *testing.T) {
	f := newFixture(t)
	f.writeRepo("profiles = [\"base\"]\n" + inlineSkills)
	f.writeManifest(`
[[installs]]
name = "local"
source = "example/manifest"
agents = ["codex"]
skills = ["manifest-skill"]
profiles = ["base"]
`)
	out, _, err := f.run(Filters{Command: CmdDryRun})
	if err != nil || !strings.Contains(out, "local-skill") || strings.Contains(out, "manifest-skill") {
		t.Fatalf("inline must override named project definition: %s %v", out, err)
	}
	// The same name in user scope must still install independently.
	f.writeManifest(`
[[installs]]
name = "local"
source = "example/global"
agents = ["codex"]
skills = ["global-skill"]
profiles = ["global"]
`)
	out, _, err = f.run(Filters{Command: CmdDryRun})
	if err != nil || !strings.Contains(out, "local-skill") || !strings.Contains(out, "example/global -g") {
		t.Fatalf("inline must not shadow global entries: %s %v", out, err)
	}
}

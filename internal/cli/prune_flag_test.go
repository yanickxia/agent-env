package cli

import "testing"

func TestPruneFlagRegistered(t *testing.T) {
	all := newAllCmd("apply", "test")
	if all.Flags().Lookup("prune") == nil {
		t.Fatal("all-in-one apply must expose --prune")
	}

	skillsCmd := newSkillsCmd()
	subs := skillsCmd.Commands()
	if len(subs) == 0 {
		t.Fatal("skills command has no subcommands")
	}
	for _, sub := range subs {
		if sub.Flags().Lookup("prune") == nil {
			t.Fatalf("skills %s must expose --prune", sub.Name())
		}
	}
}

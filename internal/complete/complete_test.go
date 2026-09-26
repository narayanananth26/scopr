package complete_test

import (
	"slices"
	"testing"

	"scopr/internal/complete"
)

func env() complete.Env {
	return complete.Env{
		Root: func(workspace string) string {
			if workspace != "" {
				return "/w/" + workspace
			}
			return "/w/here"
		},
		Scopes: func(root string) []complete.Candidate {
			return []complete.Candidate{
				{Value: "surfaces", Desc: "gl-panel gl-api"},
				{Value: "webapp", Desc: "gl-webapp"},
			}
		},
		Repos: func(root string) []complete.Candidate {
			return []complete.Candidate{{Value: "gl-api"}, {Value: "gl-panel"}, {Value: "gl-webapp"}}
		},
		Workspaces: func() []complete.Candidate {
			return []complete.Candidate{{Value: "Goodlife", Desc: "/w/Goodlife"}, {Value: "scopr", Desc: "/w/scopr"}}
		},
	}
}

func values(t *testing.T, argv ...string) []string {
	t.Helper()

	r := complete.Complete(env(), argv)
	out := make([]string, 0, len(r.Candidates))
	for _, c := range r.Candidates {
		out = append(out, c.Value)
	}
	return out
}

func has(t *testing.T, got []string, want ...string) {
	t.Helper()

	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("candidates %v do not include %q", got, w)
		}
	}
}

func lacks(t *testing.T, got []string, unwanted ...string) {
	t.Helper()

	for _, u := range unwanted {
		if slices.Contains(got, u) {
			t.Errorf("candidates %v include %q", got, u)
		}
	}
}

func TestEmptyOffersCommandsAndMembers(t *testing.T) {
	got := values(t, "")

	has(t, got, "list", "save", "workspace", "gl-api", "@surfaces")
}

func TestPrefixNarrows(t *testing.T) {
	got := values(t, "li")

	if want := []string{"list"}; !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
}

func TestSigilNarrowsToScopes(t *testing.T) {
	got := values(t, "@")

	if want := []string{"@surfaces", "@webapp"}; !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
}

func TestSubcommandsAfterWorkspace(t *testing.T) {
	got := values(t, "workspace", "")

	if want := []string{"list", "add", "remove"}; !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
}

func TestWorkspaceRemoveOffersWorkspaces(t *testing.T) {
	got := values(t, "workspace", "remove", "")

	if want := []string{"Goodlife", "scopr"}; !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
}

func TestWorkspaceAddAsksForFiles(t *testing.T) {
	r := complete.Complete(env(), []string{"workspace", "add", ""})

	if !r.Files {
		t.Error("Files not set for a path operand")
	}
	if len(r.Candidates) != 0 {
		t.Errorf("candidates = %v, want none", r.Candidates)
	}
}

func TestShowOffersExistingScopes(t *testing.T) {
	got := values(t, "show", "")

	if want := []string{"@surfaces", "@webapp"}; !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
}

func TestSaveOffersNothingForTheNewName(t *testing.T) {
	if got := values(t, "save", ""); len(got) != 0 {
		t.Errorf("candidates = %v, want none for a new name", got)
	}
}

func TestSaveOffersReposAfterTheName(t *testing.T) {
	got := values(t, "save", "@new", "")

	if want := []string{"gl-api", "gl-panel", "gl-webapp"}; !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
	lacks(t, got, "@surfaces")
}

func TestRenameOffersScopesThenNothing(t *testing.T) {
	has(t, values(t, "rename", ""), "@surfaces")

	if got := values(t, "rename", "@surfaces", ""); len(got) != 0 {
		t.Errorf("candidates = %v, want none for the new name", got)
	}
}

func TestArityStopsCandidates(t *testing.T) {
	if got := values(t, "show", "@surfaces", ""); len(got) != 0 {
		t.Errorf("candidates = %v, want none past the maximum", got)
	}
	if got := values(t, "list", ""); len(got) != 0 {
		t.Errorf("candidates = %v, want none for a command taking no operands", got)
	}
}

func TestFlagsForTheResolvedCommand(t *testing.T) {
	got := values(t, "list", "-")
	has(t, got, "--workspace", "--json", "--help")
	lacks(t, got, "--prompt", "--verbose")

	got = values(t, "infer", "-")
	has(t, got, "--verbose", "--prompt")
	lacks(t, got, "--json")
}

func TestFlagValueForWorkspace(t *testing.T) {
	got := values(t, "-w", "")

	if want := []string{"Goodlife", "scopr"}; !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
}

func TestFlagValueForPromptOffersNothing(t *testing.T) {
	if got := values(t, "-p", ""); len(got) != 0 {
		t.Errorf("candidates = %v, want none", got)
	}
}

func TestBooleanFlagDoesNotSwallowTheNextWord(t *testing.T) {
	has(t, values(t, "--verbose", ""), "list", "save")
}

func TestTerminatorStopsFlagCompletion(t *testing.T) {
	if got := values(t, "run", "--", "-"); slices.Contains(got, "--workspace") {
		t.Errorf("candidates = %v, want no flags past the terminator", got)
	}
}

func TestHelpOffersCommands(t *testing.T) {
	has(t, values(t, "help", ""), "list", "save", "workspace")
}

func TestInferOffersNothing(t *testing.T) {
	if got := values(t, "infer", ""); len(got) != 0 {
		t.Errorf("candidates = %v, want none for free text", got)
	}
}

func TestWorkspaceFlagPicksTheRoot(t *testing.T) {
	var asked string

	e := env()
	e.Scopes = func(root string) []complete.Candidate {
		asked = root
		return nil
	}

	complete.Complete(e, []string{"-w", "Goodlife", "show", ""})

	if asked != "/w/Goodlife" {
		t.Errorf("scopes read from %q, want /w/Goodlife", asked)
	}
}

func TestNoArgvIsTheSameAsEmpty(t *testing.T) {
	has(t, values(t), "list", "save")
}

func TestHiddenCommandsAreNotOffered(t *testing.T) {
	lacks(t, values(t, ""), "__complete")
}

func TestCandidatesCarryAGroup(t *testing.T) {
	for _, tc := range []struct {
		argv  []string
		value string
		group string
	}{
		{[]string{""}, "list", "commands"},
		{[]string{""}, "gl-api", "repos"},
		{[]string{""}, "@surfaces", "scopes"},
		{[]string{"workspace", "remove", ""}, "Goodlife", "workspaces"},
		{[]string{"list", "-"}, "--json", "flags"},
	} {
		var found string
		for _, c := range complete.Complete(env(), tc.argv).Candidates {
			if c.Value == tc.value {
				found = c.Group
			}
		}
		if found != tc.group {
			t.Errorf("%q: %s grouped as %q, want %q", tc.argv, tc.value, found, tc.group)
		}
	}
}

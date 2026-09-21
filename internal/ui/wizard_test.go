package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"scopr/internal/files"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func wizard() Wizard {
	spaces := []Space{
		{Name: "Goodlife", Root: "/w/Goodlife"},
		{Name: "scopr", Root: "/w/scopr"},
	}
	entries := []Entry{
		{Root: "/w/Goodlife", Scope: "surfaces", Repos: []string{"apps/web", "apps/shared"}},
		{Root: "/w/Goodlife", Scope: "native", Repos: []string{"apps/web", "services/api"}},
		{Root: "/w/scopr", Scope: "core", Repos: []string{"cmd"}},
	}

	load := func(root, name string) ([]string, error) {
		switch root + "/" + name {
		case "/w/Goodlife/surfaces":
			return []string{"apps/web", "apps/shared"}, nil
		case "/w/Goodlife/native":
			return []string{"apps/web", "services/api"}, nil
		case "/w/scopr/core":
			return []string{"cmd"}, nil
		}
		return nil, errors.New("no such scope: " + name)
	}

	repos := func(root string) []string {
		if root == "/w/scopr" {
			return []string{"cmd", "internal"}
		}
		return []string{"apps/web", "apps/shared", "services/api"}
	}

	return NewWizard(spaces, entries, load, repos)
}

func type_(w Wizard, s string) Wizard {
	for _, r := range s {
		w = w.Key(string(r))
	}
	return w
}

func pick(w Wizard, name string) Wizard {
	for i, sp := range w.Spaces {
		if sp.Name == name {
			for range i {
				w = w.Key("down")
			}
			return w.Key("enter")
		}
	}
	panic("no such workspace in fixture: " + name)
}

func TestWizardStartsAtWorkspace(t *testing.T) {
	got := wizard().View()

	if !strings.Contains(got, "which workspace") {
		t.Errorf("wizard did not start at the workspace step:\n%s", got)
	}
	for _, want := range []string{"Goodlife", "scopr"} {
		if !strings.Contains(got, want) {
			t.Errorf("workspace step missing %q:\n%s", want, got)
		}
	}
}

func TestWizardWorkspaceStepCountsScopes(t *testing.T) {
	got := wizard().View()

	if !strings.Contains(got, "2 scopes") || !strings.Contains(got, "1 scope") {
		t.Errorf("workspace step does not show what each holds:\n%s", got)
	}
}

func TestWizardSkipsWorkspaceStepWhenThereIsOne(t *testing.T) {
	w := NewWizard(
		[]Space{{Name: "Only", Root: "/w/only"}},
		[]Entry{{Root: "/w/only", Scope: "surfaces"}},
		func(string, string) ([]string, error) { return nil, nil },
		func(string) []string { return nil },
	)

	if w.step != stepName {
		t.Errorf("step = %v, want the name step", w.step)
	}
	if w.Root() != "/w/only" {
		t.Errorf("Root = %q, want the only workspace", w.Root())
	}
}

func TestWizardNameStepShowsOnlyThatWorkspacesScopes(t *testing.T) {
	w := pick(wizard(), "scopr")

	got := w.View()
	if !strings.Contains(got, "core") {
		t.Errorf("name step missing that workspace's scope:\n%s", got)
	}
	if strings.Contains(got, "surfaces") {
		t.Errorf("name step showed another workspace's scope:\n%s", got)
	}
	if !strings.Contains(got, "in scopr") {
		t.Errorf("name step does not say which workspace:\n%s", got)
	}
}

func TestWizardPickingAScopeLoadsIt(t *testing.T) {
	w := type_(pick(wizard(), "scopr"), "core").Key("enter")

	if w.Root() != "/w/scopr" {
		t.Errorf("Root = %q, want /w/scopr", w.Root())
	}
	if want := []string{"cmd"}; !slices.Equal(w.scope.Chosen, want) {
		t.Errorf("scope = %v, want %v", w.scope.Chosen, want)
	}
	if w.Name() != "" {
		t.Errorf("Name = %q; an existing scope needs no saving", w.Name())
	}
	if w.Label() != "core" {
		t.Errorf("Label = %q, want core", w.Label())
	}
}

func TestWizardUnnamedStartsFresh(t *testing.T) {
	w := pick(wizard(), "scopr")

	hits := w.matchesName()
	last := hits[len(hits)-1]
	for range len(hits) - 1 {
		w = w.Key("down")
	}
	w = w.Key("enter")

	if last.Scope != "" {
		t.Fatalf("last entry = %+v, want the unnamed one", last)
	}
	if len(w.scope.Chosen) != 0 {
		t.Errorf("scope = %v, want empty", w.scope.Chosen)
	}
	if w.Name() != "" || w.Label() != "" {
		t.Errorf("Name = %q, Label = %q, want both empty", w.Name(), w.Label())
	}
}

func TestWizardNewNameSavesInThatWorkspace(t *testing.T) {
	w := type_(pick(wizard(), "Goodlife"), "checkout").Key("enter")

	if w.Name() != "checkout" {
		t.Errorf("Name = %q, want checkout", w.Name())
	}
	if w.Root() != "/w/Goodlife" {
		t.Errorf("Root = %q, want the chosen workspace", w.Root())
	}
	if len(w.scope.Chosen) != 0 {
		t.Errorf("scope = %v, want empty for a new name", w.scope.Chosen)
	}
}

func TestWizardNameEscapeGoesBackToWorkspace(t *testing.T) {
	w := pick(wizard(), "scopr").Key("esc")

	if w.step != stepWorkspace {
		t.Errorf("step = %v, want the workspace step", w.step)
	}
	if w.Cancelled {
		t.Error("escape from the name step cancelled the wizard")
	}
}

func TestWizardNameEscapeCancelsWithOneWorkspace(t *testing.T) {
	w := NewWizard(
		[]Space{{Name: "Only", Root: "/w/only"}},
		nil,
		func(string, string) ([]string, error) { return nil, nil },
		func(string) []string { return nil },
	)

	if got := w.Key("esc"); !got.Cancelled {
		t.Error("escape did not cancel when there was no workspace step")
	}
}

func TestWizardCursorMoves(t *testing.T) {
	w := pick(wizard(), "Goodlife").Key("down")

	if w.nameAt != 1 {
		t.Errorf("cursor = %d, want 1", w.nameAt)
	}

	w = w.Key("enter")
	if w.Label() != "native" {
		t.Errorf("Label = %q, want the second entry", w.Label())
	}
}

func TestWizardCursorResetsOnTyping(t *testing.T) {
	w := pick(wizard(), "Goodlife").Key("down").Key("s")

	if w.nameAt != 0 {
		t.Errorf("cursor = %d, want it reset", w.nameAt)
	}
}

func TestWizardLoadFailureStaysOnName(t *testing.T) {
	broken := NewWizard(
		[]Space{{Name: "W", Root: "/w"}},
		[]Entry{{Root: "/w", Scope: "broken"}},
		func(string, string) ([]string, error) { return nil, errors.New("scope file is a mess") },
		func(string) []string { return nil },
	)

	w := type_(broken, "broken").Key("enter")

	if w.step != stepName {
		t.Error("a failed load advanced anyway")
	}
	if w.Err == nil {
		t.Fatal("a failed load reported no error")
	}
	if !strings.Contains(w.View(), "mess") {
		t.Errorf("view does not show the failure:\n%s", w.View())
	}
}

func TestWizardWalksToPrompt(t *testing.T) {
	w := type_(pick(wizard(), "Goodlife"), "surfaces").Key("enter")
	w = w.Key("enter")

	if w.step != stepPrompt {
		t.Fatalf("step = %v, want the prompt step", w.step)
	}
	if !strings.Contains(w.View(), "working on") {
		t.Errorf("prompt step does not ask:\n%s", w.View())
	}
}

func TestWizardFinishesWithPrompt(t *testing.T) {
	w := type_(pick(wizard(), "Goodlife"), "surfaces").Key("enter")
	w = w.Key("enter")
	w = type_(w, "trace the checkout call").Key("enter")

	if !w.Done() {
		t.Fatal("wizard did not finish")
	}
	if w.Prompt() != "trace the checkout call" {
		t.Errorf("Prompt = %q", w.Prompt())
	}
	if want := []string{"apps/web", "apps/shared"}; !slices.Equal(w.Repos(), want) {
		t.Errorf("Repos = %v, want %v", w.Repos(), want)
	}
}

func TestWizardFinishesWithoutPrompt(t *testing.T) {
	w := type_(pick(wizard(), "Goodlife"), "surfaces").Key("enter")
	w = w.Key("enter").Key("enter")

	if !w.Done() {
		t.Fatal("wizard did not finish on a blank prompt")
	}
	if w.Prompt() != "" {
		t.Errorf("Prompt = %q, want empty", w.Prompt())
	}
}

func TestWizardEscapeGoesBack(t *testing.T) {
	w := type_(pick(wizard(), "Goodlife"), "surfaces").Key("enter")

	w = w.Key("esc")
	if w.step != stepName {
		t.Errorf("escape from the scope step went to %v, want the name step", w.step)
	}
	if w.Cancelled {
		t.Error("escape from the scope step cancelled the wizard")
	}

	w = w.Key("enter").Key("enter")
	w = w.Key("esc")
	if w.step != stepScope {
		t.Errorf("escape from the prompt step went to %v, want the scope step", w.step)
	}
}

func TestWizardBackFromPromptCanEditAgain(t *testing.T) {
	w := type_(pick(wizard(), "Goodlife"), "surfaces").Key("enter")
	w = w.Key("enter").Key("esc")

	w = w.Key("x")
	if len(w.scope.Chosen) != 1 {
		t.Errorf("scope = %v, want one entry after removing", w.scope.Chosen)
	}
	if w.step != stepScope {
		t.Errorf("step = %v, want to still be editing", w.step)
	}
}

func TestWizardCancelsFromWorkspace(t *testing.T) {
	w := wizard().Key("esc")

	if !w.Cancelled || w.Done() {
		t.Error("escape at the name step did not cancel")
	}
}

func TestWizardCancelsFromScope(t *testing.T) {
	w := type_(pick(wizard(), "Goodlife"), "surfaces").Key("enter").Key("q")

	if !w.Cancelled || w.Done() {
		t.Error("quitting the scope step did not cancel the wizard")
	}
}

func TestWizardEmptyScopeCannotFinish(t *testing.T) {
	w := type_(pick(wizard(), "Goodlife"), "surfaces").Key("enter")
	w = w.Key("x").Key("x").Key("enter")

	if w.step == stepPrompt {
		t.Error("an empty scope advanced to the prompt")
	}
}

func tagging() Wizard {
	w := type_(pick(wizard(), "Goodlife"), "surfaces").Key("enter").Key("enter")
	w.Files = []files.File{
		{Rel: "src/cart.ts", Base: "cart.ts"},
		{Rel: "src/checkout.ts", Base: "checkout.ts"},
		{Rel: "../shared/util.ts", Base: "util.ts"},
	}
	w.filesLoaded = true
	return w
}

func TestTagOpensOnAt(t *testing.T) {
	w := tagging().Key("@")

	if !w.tagging {
		t.Fatal("@ did not start a tag")
	}
	if !strings.Contains(w.View(), "src/checkout.ts") {
		t.Errorf("tag list does not offer files:\n%s", w.View())
	}
}

func TestTagFiltersAsYouType(t *testing.T) {
	w := type_(tagging().Key("@"), "checkout")

	m := w.matches()
	if len(m) == 0 || m[0].Rel != "src/checkout.ts" {
		t.Errorf("best match = %v, want src/checkout.ts", m)
	}
}

func TestTagInsertsPath(t *testing.T) {
	w := type_(tagging(), "look at ")
	w = type_(w.Key("@"), "checkout").Key("enter")

	if want := "look at @src/checkout.ts "; w.prompt != want {
		t.Errorf("prompt = %q, want %q", w.prompt, want)
	}
	if w.tagging {
		t.Error("still tagging after inserting")
	}
}

func TestTagTabInserts(t *testing.T) {
	w := type_(tagging().Key("@"), "cart").Key("tab")

	if !strings.Contains(w.prompt, "@src/cart.ts") {
		t.Errorf("prompt = %q, want the tag inserted", w.prompt)
	}
}

func TestTagEscapeDropsTheAt(t *testing.T) {
	w := type_(tagging(), "look at ")
	w = type_(w.Key("@"), "check").Key("esc")

	if want := "look at "; w.prompt != want {
		t.Errorf("prompt = %q, want %q", w.prompt, want)
	}
	if w.tagging {
		t.Error("still tagging after escape")
	}
}

func TestTagBackspaceOffTheAtExits(t *testing.T) {
	w := tagging().Key("@").Key("backspace")

	if w.tagging {
		t.Error("backspace on an empty query stayed in tag mode")
	}
	if w.prompt != "" {
		t.Errorf("prompt = %q, want empty", w.prompt)
	}
}

func TestTagBackspaceNarrowsQuery(t *testing.T) {
	w := type_(tagging().Key("@"), "checkout").Key("backspace")

	if w.query != "checkou" {
		t.Errorf("query = %q, want checkou", w.query)
	}
	if !strings.HasSuffix(w.prompt, "@checkou") {
		t.Errorf("prompt = %q, want it to track the query", w.prompt)
	}
}

func TestTagSpaceEndsTagging(t *testing.T) {
	w := type_(tagging().Key("@"), "zzz").Key(" ")

	if w.tagging {
		t.Error("space did not end tagging")
	}
	if want := "@zzz "; w.prompt != want {
		t.Errorf("prompt = %q, want %q", w.prompt, want)
	}
}

func TestTagWithNoMatchesInsertsNothing(t *testing.T) {
	w := type_(tagging().Key("@"), "zzzzz").Key("enter")

	if strings.Contains(w.prompt, ".ts") {
		t.Errorf("prompt = %q, want no path inserted", w.prompt)
	}
	if w.tagging {
		t.Error("still tagging after a miss")
	}
}

func TestTagSaysWhenStillLoading(t *testing.T) {
	w := type_(pick(wizard(), "Goodlife"), "surfaces").Key("enter").Key("enter").Key("@")

	if !strings.Contains(w.View(), "still reading") {
		t.Errorf("tag list does not report loading:\n%s", w.View())
	}
}

func TestTagThenFinish(t *testing.T) {
	w := type_(tagging(), "why is ")
	w = type_(w.Key("@"), "checkout").Key("enter")
	w = type_(w, "called twice").Key("enter")

	if !w.Done() {
		t.Fatal("wizard did not finish")
	}
	if want := "why is @src/checkout.ts called twice"; w.Prompt() != want {
		t.Errorf("Prompt = %q, want %q", w.Prompt(), want)
	}
}

func TestTagCursorMoves(t *testing.T) {
	w := tagging().Key("@").Key("down")

	m := w.matches()
	if len(m) < 2 {
		t.Fatalf("need at least two matches, got %v", m)
	}
	if w.tagCursor != 1 {
		t.Errorf("cursor = %d, want 1", w.tagCursor)
	}

	w = w.Key("enter")
	if !strings.Contains(w.prompt, m[1].Rel) {
		t.Errorf("prompt = %q, want the second match %q", w.prompt, m[1].Rel)
	}
}

func TestTagCursorStopsAtEdges(t *testing.T) {
	w := tagging().Key("@").Key("up").Key("up")
	if w.tagCursor != 0 {
		t.Errorf("cursor = %d, want 0 at the top", w.tagCursor)
	}

	w = tagging().Key("@")
	for range 20 {
		w = w.Key("down")
	}
	if w.tagCursor >= len(w.matches()) {
		t.Errorf("cursor = %d, past %d matches", w.tagCursor, len(w.matches()))
	}
}

func TestTagCtrlNAndPMove(t *testing.T) {
	w := tagging().Key("@").Key("ctrl+n")
	if w.tagCursor != 1 {
		t.Errorf("ctrl+n did not move down: %d", w.tagCursor)
	}

	w = w.Key("ctrl+p")
	if w.tagCursor != 0 {
		t.Errorf("ctrl+p did not move up: %d", w.tagCursor)
	}
}

func TestTagCursorResetsOnQueryChange(t *testing.T) {
	w := tagging().Key("@").Key("down")
	if w.tagCursor != 1 {
		t.Fatalf("cursor = %d, want 1 before typing", w.tagCursor)
	}

	w = w.Key("c")
	if w.tagCursor != 0 {
		t.Errorf("cursor = %d, want it reset after typing", w.tagCursor)
	}

	w = w.Key("down").Key("backspace")
	if w.tagCursor != 0 {
		t.Errorf("cursor = %d, want it reset after backspace", w.tagCursor)
	}
}

func TestTagViewMarksTheCursor(t *testing.T) {
	w := tagging().Key("@").Key("down")

	m := w.matches()
	for _, line := range strings.Split(w.View(), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), ">") && !strings.Contains(line, m[1].Rel) {
			t.Errorf("marker is not on the cursor row: %q", line)
		}
	}
}

func TestWizardLabelIsTheChosenScope(t *testing.T) {
	w := type_(pick(wizard(), "Goodlife"), "surfaces").Key("enter")

	if w.Label() != "surfaces" {
		t.Errorf("Label = %q, want surfaces", w.Label())
	}
	if w.Name() != "" {
		t.Errorf("Name = %q; an existing scope needs no saving", w.Name())
	}
}

func TestWizardLabelEmptyWhenUnnamed(t *testing.T) {
	w := pick(wizard(), "Goodlife")

	for range len(w.matchesName()) - 1 {
		w = w.Key("down")
	}
	w = w.Key("enter")

	if got := w.Label(); got != "" {
		t.Errorf("Label = %q, want empty for an unnamed session", got)
	}
}

func TestWizardDoesNotOfferUnsaveableNames(t *testing.T) {
	for _, bad := range []string{"../../escape", "a/b", ".hidden", ".."} {
		w := type_(pick(wizard(), "Goodlife"), bad)

		for _, e := range w.matchesName() {
			if e.Scope == bad {
				t.Errorf("%q was offered as a new scope", bad)
			}
		}
	}
}

func TestWizardExplainsAnUnsaveableName(t *testing.T) {
	w := type_(pick(wizard(), "Goodlife"), "../../escape")

	got := w.View()
	if !strings.Contains(got, "separator") {
		t.Errorf("view does not say why the name is refused:\n%s", got)
	}
	if strings.Contains(got, "new") {
		t.Errorf("view still offers to create it:\n%s", got)
	}
}

func TestWizardRefusedNameCannotContinue(t *testing.T) {
	w := type_(pick(wizard(), "Goodlife"), "../../escape").Key("enter")

	if w.step != stepName {
		t.Errorf("step = %v, want to stay on the name step", w.step)
	}
	if w.Label() != "" {
		t.Errorf("Label = %q, want no scope chosen", w.Label())
	}
}

func TestWizardStillOffersValidNames(t *testing.T) {
	w := type_(pick(wizard(), "Goodlife"), "checkout")

	var found bool
	for _, e := range w.matchesName() {
		if e.Scope == "checkout" {
			found = true
		}
	}
	if !found {
		t.Error("a valid new name was not offered")
	}
}

func TestWizardNameStepShowsTheAtPrefix(t *testing.T) {
	got := pick(wizard(), "Goodlife").View()

	for _, want := range []string{"@surfaces", "@native"} {
		if !strings.Contains(got, want) {
			t.Errorf("name step missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "(unnamed)") {
		t.Errorf("the unnamed entry should not be prefixed:\n%s", got)
	}
}

func TestWizardNameStepPrefixesWhatYouType(t *testing.T) {
	got := type_(pick(wizard(), "Goodlife"), "checkout").View()

	if !strings.Contains(got, "@checkout") {
		t.Errorf("typed name is not shown as a scope:\n%s", got)
	}
}

func TestWizardNameDoesNotIncludeThePrefix(t *testing.T) {
	w := type_(pick(wizard(), "Goodlife"), "checkout").Key("enter")

	if w.Name() != "checkout" {
		t.Errorf("Name = %q, want checkout without the prefix", w.Name())
	}
}

func atPrompt(t *testing.T) Wizard {
	t.Helper()

	w := type_(pick(wizard(), "Goodlife"), "surfaces").Key("enter").Key("enter")
	w.Files = []files.File{
		{Rel: "src/cart.ts", Base: "cart.ts"},
		{Rel: "src/checkout.ts", Base: "checkout.ts"},
	}
	w.filesLoaded = true
	return w
}

func TestPromptCursorMovesLeftAndRight(t *testing.T) {
	w := type_(atPrompt(t), "abc")

	if w.promptAt != 3 {
		t.Fatalf("cursor = %d, want 3 after typing", w.promptAt)
	}

	w = w.Key("left").Key("left")
	if w.promptAt != 1 {
		t.Errorf("cursor = %d, want 1", w.promptAt)
	}

	w = w.Key("right")
	if w.promptAt != 2 {
		t.Errorf("cursor = %d, want 2", w.promptAt)
	}
}

func TestPromptCursorStopsAtEdges(t *testing.T) {
	w := type_(atPrompt(t), "ab")

	for range 5 {
		w = w.Key("left")
	}
	if w.promptAt != 0 {
		t.Errorf("cursor = %d, want 0", w.promptAt)
	}

	for range 5 {
		w = w.Key("right")
	}
	if w.promptAt != 2 {
		t.Errorf("cursor = %d, want 2", w.promptAt)
	}
}

func TestPromptInsertsAtCursor(t *testing.T) {
	w := type_(atPrompt(t), "ac").Key("left")
	w = w.Key("b")

	if w.prompt != "abc" {
		t.Errorf("prompt = %q, want abc", w.prompt)
	}
	if w.promptAt != 2 {
		t.Errorf("cursor = %d, want it after the inserted rune", w.promptAt)
	}
}

func TestPromptBackspaceDeletesBeforeCursor(t *testing.T) {
	w := type_(atPrompt(t), "abc").Key("left").Key("backspace")

	if w.prompt != "ac" {
		t.Errorf("prompt = %q, want ac", w.prompt)
	}
	if w.promptAt != 1 {
		t.Errorf("cursor = %d, want 1", w.promptAt)
	}
}

func TestPromptDeleteRemovesAtCursor(t *testing.T) {
	w := type_(atPrompt(t), "abc").Key("left").Key("delete")

	if w.prompt != "ab" {
		t.Errorf("prompt = %q, want ab", w.prompt)
	}
}

func TestPromptHomeAndEnd(t *testing.T) {
	w := type_(atPrompt(t), "abc").Key("home")
	if w.promptAt != 0 {
		t.Errorf("home left cursor at %d", w.promptAt)
	}

	w = w.Key("end")
	if w.promptAt != 3 {
		t.Errorf("end left cursor at %d", w.promptAt)
	}
}

func TestPromptKillLineBothWays(t *testing.T) {
	w := type_(atPrompt(t), "abcdef").Key("left").Key("left")

	if got := w.Key("ctrl+k"); got.prompt != "abcd" {
		t.Errorf("ctrl+k gave %q, want abcd", got.prompt)
	}
	if got := w.Key("ctrl+u"); got.prompt != "ef" {
		t.Errorf("ctrl+u gave %q, want ef", got.prompt)
	}
}

func TestPromptHandlesMultibyte(t *testing.T) {
	w := type_(atPrompt(t), "héllo").Key("left").Key("backspace")

	if w.prompt != "hélo" {
		t.Errorf("prompt = %q, want hélo", w.prompt)
	}
}

func TestTagInsertsAtCursor(t *testing.T) {
	w := type_(atPrompt(t), "look at  please")
	for range 7 {
		w = w.Key("left")
	}

	w = type_(w.Key("@"), "checkout").Key("enter")

	if want := "look at @src/checkout.ts  please"; w.prompt != want {
		t.Errorf("prompt = %q, want %q", w.prompt, want)
	}
}

func TestTagEscapeKeepsWhatFollowed(t *testing.T) {
	w := type_(atPrompt(t), "ab")
	w = w.Key("left")
	w = type_(w.Key("@"), "che").Key("esc")

	if w.prompt != "ab" {
		t.Errorf("prompt = %q, want ab", w.prompt)
	}
	if w.promptAt != 1 {
		t.Errorf("cursor = %d, want it back where the tag started", w.promptAt)
	}
}

func TestPromptCursorIsVisibleMidString(t *testing.T) {
	w := type_(atPrompt(t), "abc").Key("left").Key("left")

	got := w.promptLine()
	if got == "abc" {
		t.Errorf("promptLine = %q, want the cursor rendered", got)
	}
	if !strings.Contains(got, cursor.Render("b")) {
		t.Errorf("promptLine = %q, want b under the cursor", got)
	}
}

func TestPromptCursorVisibleAtEnd(t *testing.T) {
	w := type_(atPrompt(t), "abc")

	if got := w.promptLine(); !strings.HasPrefix(got, "abc") || got == "abc" {
		t.Errorf("promptLine = %q, want a cursor after the text", got)
	}
}

func TestPromptCtrlOAsksForTheEditor(t *testing.T) {
	w := type_(atPrompt(t), "why is checkout called twice").Key("ctrl+o")

	if !w.editing {
		t.Error("ctrl+o did not ask for the editor")
	}
	if w.prompt != "why is checkout called twice" {
		t.Errorf("prompt = %q, want it untouched", w.prompt)
	}
}

func TestPromptTakesBackWhatTheEditorWrote(t *testing.T) {
	w := atPrompt(t)

	out, _ := w.Update(EditedMsg{Text: "rewritten in the editor"})
	w = out.(Wizard)

	if w.prompt != "rewritten in the editor" {
		t.Errorf("prompt = %q, want the edited text", w.prompt)
	}
	if w.promptAt != len([]rune(w.prompt)) {
		t.Errorf("cursor = %d, want it at the end", w.promptAt)
	}
}

func TestPromptKeepsTextWhenTheEditorFails(t *testing.T) {
	w := type_(atPrompt(t), "typed by hand")

	out, _ := w.Update(EditedMsg{Err: errors.New("editor exploded")})
	w = out.(Wizard)

	if w.prompt != "typed by hand" {
		t.Errorf("prompt = %q, want it kept", w.prompt)
	}
	if w.Err == nil {
		t.Fatal("a failed edit reported no error")
	}
	if !strings.Contains(w.View(), "exploded") {
		t.Errorf("view does not show the failure:\n%s", w.View())
	}
}

func TestPromptCtrlEStillMovesToEnd(t *testing.T) {
	w := type_(atPrompt(t), "abc").Key("home").Key("ctrl+e")

	if w.editing {
		t.Error("ctrl+e asked for the editor")
	}
	if w.promptAt != 3 {
		t.Errorf("cursor = %d, want it at the end", w.promptAt)
	}
}

func TestLoadFilesReceivesTheChosenWorkspace(t *testing.T) {
	var gotRoot string
	var gotRepos []string

	w := wizard()
	w.LoadFiles = func(root string, names []string) []files.File {
		gotRoot, gotRepos = root, names
		return []files.File{{Rel: "cmd/main.go", Base: "main.go"}}
	}

	w = type_(pick(w, "scopr"), "core").Key("enter")
	out, cmd := w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	w = out.(Wizard)

	if cmd == nil {
		t.Fatal("entering the prompt step issued no load")
	}
	msg := cmd()

	if gotRoot != "/w/scopr" {
		t.Errorf("LoadFiles got root %q, want /w/scopr", gotRoot)
	}
	if want := []string{"cmd"}; !slices.Equal(gotRepos, want) {
		t.Errorf("LoadFiles got repos %v, want %v", gotRepos, want)
	}

	out, _ = w.Update(msg)
	if got := out.(Wizard); !got.filesLoaded || len(got.Files) != 1 {
		t.Errorf("files not taken up: loaded=%v files=%v", got.filesLoaded, got.Files)
	}
}

func TestTagSaysWhenTheScopeHasNoFiles(t *testing.T) {
	w := atPrompt(t)
	w.Files = nil
	w.filesLoaded = true

	got := w.Key("@").View()
	if !strings.Contains(got, "no files found") {
		t.Errorf("view does not distinguish empty from unread:\n%s", got)
	}
	if strings.Contains(got, "still reading") {
		t.Errorf("view still claims to be reading:\n%s", got)
	}
}

func TestPromptWrapsToTheTerminalWidth(t *testing.T) {
	w := atPrompt(t)
	out, _ := w.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	w = out.(Wizard)

	w = type_(w, strings.Repeat("word ", 30))

	for _, line := range strings.Split(w.View(), "\n") {
		if n := ansi.StringWidth(line); n > 40 {
			t.Errorf("line is %d columns wide, want 40 or fewer: %q", n, line)
		}
	}
}

func TestListRowsAreClippedToWidth(t *testing.T) {
	w := wizard()
	out, _ := w.Update(tea.WindowSizeMsg{Width: 30, Height: 24})
	w = out.(Wizard)

	w = pick(w, "Goodlife")

	for _, line := range strings.Split(w.View(), "\n") {
		if n := ansi.StringWidth(line); n > 30 {
			t.Errorf("row is %d columns wide, want 30 or fewer: %q", n, line)
		}
	}
}

func TestWindowSizeReachesTheScopeEditor(t *testing.T) {
	w := wizard()
	out, _ := w.Update(tea.WindowSizeMsg{Width: 33, Height: 24})

	if got := out.(Wizard).scope.width; got != 33 {
		t.Errorf("scope editor width = %d, want 33", got)
	}
}

func TestWithPromptPutsTheCursorAtTheEnd(t *testing.T) {
	w := atPrompt(t).WithPrompt("fix the build")

	if w.promptAt != len("fix the build") {
		t.Errorf("cursor = %d, want %d", w.promptAt, len("fix the build"))
	}

	w = w.Key("!")
	if w.prompt != "fix the build!" {
		t.Errorf("prompt = %q, want the keystroke appended", w.prompt)
	}
}

func TestWithPromptCountsRunesNotBytes(t *testing.T) {
	w := atPrompt(t).WithPrompt("héllo")

	if w.promptAt != 5 {
		t.Errorf("cursor = %d, want 5", w.promptAt)
	}

	w = w.Key("left").Key("x")
	if w.prompt != "héllxo" {
		t.Errorf("prompt = %q, want x before the last rune", w.prompt)
	}
}

func TestWithPromptCanBeCleared(t *testing.T) {
	w := atPrompt(t).WithPrompt("abc")
	for range 3 {
		w = w.Key("backspace")
	}

	if w.Prompt() != "" {
		t.Errorf("prompt = %q, want it cleared", w.Prompt())
	}
}

func TestWithPromptEmptyLeavesTheWizardAlone(t *testing.T) {
	w := atPrompt(t).WithPrompt("")

	if w.prompt != "" || w.promptAt != 0 {
		t.Errorf("prompt = %q at %d, want empty", w.prompt, w.promptAt)
	}
}

func TestWithPromptSurvivesToTheEnd(t *testing.T) {
	w := atPrompt(t).WithPrompt("fix the build").Key("enter")

	if !w.Done() {
		t.Fatal("wizard not done")
	}
	if w.Prompt() != "fix the build" {
		t.Errorf("prompt = %q, want it carried through", w.Prompt())
	}
}

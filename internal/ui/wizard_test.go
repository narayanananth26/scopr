package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func wizard() Wizard {
	return NewWizard(
		[]string{"surfaces", "native"},
		[]string{"apps/web", "apps/shared", "services/api"},
		func(name string) ([]string, error) {
			switch name {
			case "surfaces":
				return []string{"apps/web", "apps/shared"}, nil
			case "native":
				return []string{"apps/web", "services/api"}, nil
			}
			return nil, errors.New("no such scope: " + name)
		},
	)
}

func type_(w Wizard, s string) Wizard {
	for _, r := range s {
		w = w.Key(string(r))
	}
	return w
}

func TestWizardStartsAtName(t *testing.T) {
	got := wizard().View()

	if !strings.Contains(got, "name this session") {
		t.Errorf("wizard did not start at the name step:\n%s", got)
	}
}

// An existing name loads that scope rather than starting empty.
func TestWizardExistingNameLoadsScope(t *testing.T) {
	w := type_(wizard(), "surfaces")
	w = w.Key("enter")

	if want := []string{"apps/web", "apps/shared"}; !slices.Equal(w.scope.Chosen, want) {
		t.Errorf("scope = %v, want %v", w.scope.Chosen, want)
	}
	if w.Name() != "" {
		t.Errorf("Name = %q; an existing scope needs no saving", w.Name())
	}
}

func TestWizardNewNameStartsEmptyAndSaves(t *testing.T) {
	w := type_(wizard(), "checkout")
	w = w.Key("enter")

	if len(w.scope.Chosen) != 0 {
		t.Errorf("scope = %v, want empty for a new name", w.scope.Chosen)
	}
	if w.Name() != "checkout" {
		t.Errorf("Name = %q, want checkout", w.Name())
	}
}

func TestWizardBlankNameSavesNothing(t *testing.T) {
	w := wizard().Key("enter")

	if w.Name() != "" {
		t.Errorf("Name = %q, want empty", w.Name())
	}
	if w.step != stepScope {
		t.Error("blank name did not continue to the scope step")
	}
}

// The name step says what will happen before you commit to it.
func TestWizardNameStepExplainsItself(t *testing.T) {
	blank := wizard().View()
	if !strings.Contains(blank, "not be saved") {
		t.Errorf("blank name does not say it will not be saved:\n%s", blank)
	}

	existing := type_(wizard(), "surfaces").View()
	if !strings.Contains(existing, "loads @surfaces") {
		t.Errorf("existing name does not say it loads:\n%s", existing)
	}

	fresh := type_(wizard(), "checkout").View()
	if !strings.Contains(fresh, "saves as @checkout") {
		t.Errorf("new name does not say it saves:\n%s", fresh)
	}
}

func TestWizardNameBackspace(t *testing.T) {
	w := type_(wizard(), "surfaces")
	w = w.Key("backspace")

	if w.existing() {
		t.Error("backspace left the name matching a saved scope")
	}
}

func TestWizardLoadFailureStaysOnName(t *testing.T) {
	w := NewWizard([]string{"broken"}, []string{"apps/web"},
		func(string) ([]string, error) { return nil, errors.New("scope file is a mess") })

	w = type_(w, "broken").Key("enter")

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
	w := type_(wizard(), "surfaces").Key("enter")
	w = w.Key("enter") // accept the scope

	if w.step != stepPrompt {
		t.Fatalf("step = %v, want the prompt step", w.step)
	}
	if !strings.Contains(w.View(), "working on") {
		t.Errorf("prompt step does not ask:\n%s", w.View())
	}
}

func TestWizardFinishesWithPrompt(t *testing.T) {
	w := type_(wizard(), "surfaces").Key("enter")
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
	w := type_(wizard(), "surfaces").Key("enter")
	w = w.Key("enter").Key("enter")

	if !w.Done() {
		t.Fatal("wizard did not finish on a blank prompt")
	}
	if w.Prompt() != "" {
		t.Errorf("Prompt = %q, want empty", w.Prompt())
	}
}

// Escape goes back a step rather than out, so a mistyped name costs one key.
func TestWizardEscapeGoesBack(t *testing.T) {
	w := type_(wizard(), "surfaces").Key("enter")

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

// Escaping back into the scope step must not immediately finish again.
func TestWizardBackFromPromptCanEditAgain(t *testing.T) {
	w := type_(wizard(), "surfaces").Key("enter")
	w = w.Key("enter").Key("esc")

	w = w.Key("x")
	if len(w.scope.Chosen) != 1 {
		t.Errorf("scope = %v, want one entry after removing", w.scope.Chosen)
	}
	if w.step != stepScope {
		t.Errorf("step = %v, want to still be editing", w.step)
	}
}

func TestWizardCancelsFromName(t *testing.T) {
	w := wizard().Key("esc")

	if !w.Cancelled || w.Done() {
		t.Error("escape at the name step did not cancel")
	}
}

func TestWizardCancelsFromScope(t *testing.T) {
	w := type_(wizard(), "surfaces").Key("enter").Key("q")

	if !w.Cancelled || w.Done() {
		t.Error("quitting the scope step did not cancel the wizard")
	}
}

// An empty scope must not start a session, however it was reached.
func TestWizardEmptyScopeCannotFinish(t *testing.T) {
	w := type_(wizard(), "surfaces").Key("enter")
	w = w.Key("x").Key("x").Key("enter")

	if w.step == stepPrompt {
		t.Error("an empty scope advanced to the prompt")
	}
}

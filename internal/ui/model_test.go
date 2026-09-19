package ui

import (
	"slices"
	"strings"
	"testing"
)

func editor() Model {
	return New(
		[]string{"services/api", "apps/web"},
		[]string{"apps/shared", "apps/web", "services/api", "services/billing"},
	)
}

// press applies keystrokes in order.
func press(m Model, keys ...string) Model {
	for _, k := range keys {
		m = m.key(k)
	}
	return m
}

func TestStartsWithGivenScope(t *testing.T) {
	m := editor()

	if want := []string{"services/api", "apps/web"}; !slices.Equal(m.Chosen, want) {
		t.Errorf("Chosen = %v, want %v", m.Chosen, want)
	}
}

func TestRemoveDropsUnderCursor(t *testing.T) {
	m := press(editor(), "j", "x")

	if want := []string{"services/api"}; !slices.Equal(m.Chosen, want) {
		t.Errorf("Chosen = %v, want %v", m.Chosen, want)
	}
}

// Removing the last entry must not leave the cursor past the end.
func TestRemoveLastKeepsCursorValid(t *testing.T) {
	m := press(editor(), "j", "x")

	if m.cursor >= len(m.Chosen) {
		t.Errorf("cursor %d out of range for %v", m.cursor, m.Chosen)
	}
}

func TestRemoveEverythingThenEnterDoesNotStart(t *testing.T) {
	m := press(editor(), "x", "x", "enter")

	if m.Done {
		t.Error("an empty scope must not start a session")
	}
	if m.Result() != nil {
		t.Errorf("Result = %v, want nil", m.Result())
	}
}

// Reordering is how the primary is chosen: the first entry becomes cwd.
func TestReorderPromotesToPrimary(t *testing.T) {
	m := press(editor(), "j", "K")

	if want := []string{"apps/web", "services/api"}; !slices.Equal(m.Chosen, want) {
		t.Errorf("Chosen = %v, want %v", m.Chosen, want)
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want it to follow the moved entry", m.cursor)
	}
}

func TestReorderDemotes(t *testing.T) {
	m := press(editor(), "J")

	if want := []string{"apps/web", "services/api"}; !slices.Equal(m.Chosen, want) {
		t.Errorf("Chosen = %v, want %v", m.Chosen, want)
	}
}

func TestReorderStopsAtEdges(t *testing.T) {
	m := press(editor(), "K", "K", "K")
	if want := []string{"services/api", "apps/web"}; !slices.Equal(m.Chosen, want) {
		t.Errorf("moving up at the top changed the order: %v", m.Chosen)
	}

	m = press(editor(), "J", "J", "J")
	if want := []string{"apps/web", "services/api"}; !slices.Equal(m.Chosen, want) {
		t.Errorf("moving down past the end changed the order: %v", m.Chosen)
	}
}

func TestAddAppendsChoice(t *testing.T) {
	m := press(editor(), "a", "enter")

	if len(m.Chosen) != 3 {
		t.Fatalf("Chosen = %v, want three entries", m.Chosen)
	}
	if m.Chosen[0] != "services/api" {
		t.Errorf("adding changed the primary: %v", m.Chosen)
	}
}

// Offering a repo already in scope would let it be added twice.
func TestAddExcludesChosen(t *testing.T) {
	m := press(editor(), "a")

	for _, c := range m.candidates() {
		if slices.Contains(m.Chosen, c) {
			t.Errorf("candidate %q is already in scope: %v", c, m.candidates())
		}
	}
}

func TestAddFiltersByQuery(t *testing.T) {
	m := press(editor(), "a", "b", "i", "l")

	got := m.candidates()
	if len(got) != 1 || got[0] != "services/billing" {
		t.Errorf("candidates = %v, want just services/billing", got)
	}
}

func TestAddBackspaceWidensQuery(t *testing.T) {
	m := press(editor(), "a", "b", "i", "l", "backspace", "backspace", "backspace")

	if m.query != "" {
		t.Errorf("query = %q, want empty", m.query)
	}
	if len(m.candidates()) != 2 {
		t.Errorf("candidates = %v, want both unchosen repos", m.candidates())
	}
}

func TestAddEscapeReturnsWithoutAdding(t *testing.T) {
	m := press(editor(), "a", "b", "esc")

	if len(m.Chosen) != 2 {
		t.Errorf("Chosen = %v, want it unchanged", m.Chosen)
	}
	if m.mode != modeScope {
		t.Error("escape did not return to the scope list")
	}
	if m.query != "" {
		t.Errorf("query = %q, want it cleared", m.query)
	}
}

func TestAddWithNoMatchesAddsNothing(t *testing.T) {
	m := press(editor(), "a", "z", "z", "z", "enter")

	if len(m.Chosen) != 2 {
		t.Errorf("Chosen = %v, want it unchanged", m.Chosen)
	}
	if m.mode != modeScope {
		t.Error("enter with no matches did not return to the scope list")
	}
}

func TestEnterFinishes(t *testing.T) {
	m := press(editor(), "enter")

	if !m.Done {
		t.Fatal("enter did not finish")
	}
	if want := []string{"services/api", "apps/web"}; !slices.Equal(m.Result(), want) {
		t.Errorf("Result = %v, want %v", m.Result(), want)
	}
}

func TestEscapeCancels(t *testing.T) {
	m := press(editor(), "esc")

	if !m.Cancelled {
		t.Fatal("escape did not cancel")
	}
	if m.Result() != nil {
		t.Errorf("Result = %v, want nil", m.Result())
	}
}

// Editing must not write through to the caller's slice.
func TestDoesNotMutateInput(t *testing.T) {
	chosen := []string{"services/api", "apps/web"}
	available := []string{"apps/web", "services/api"}

	press(New(chosen, available), "j", "K", "x")

	if want := []string{"services/api", "apps/web"}; !slices.Equal(chosen, want) {
		t.Errorf("caller's slice became %v", chosen)
	}
}

func TestViewShowsScopeAndKeys(t *testing.T) {
	got := editor().View()

	for _, want := range []string{"services/api", "apps/web", "working directory", "reorder", "add"} {
		if !strings.Contains(got, want) {
			t.Errorf("view missing %q:\n%s", want, got)
		}
	}
}

func TestViewShowsCandidatesInAddMode(t *testing.T) {
	got := press(editor(), "a").View()

	if !strings.Contains(got, "services/billing") {
		t.Errorf("add view missing a candidate:\n%s", got)
	}
	if strings.Contains(got, "working directory") {
		t.Errorf("add view still showing the scope list:\n%s", got)
	}
}

// A suggestion's reason belongs beside its repo, not printed above the list.
func TestViewShowsNotes(t *testing.T) {
	m := editor()
	m.Notes = map[string]string{
		"services/api": "owns the checkout endpoint",
		"apps/web":     "calls it from the cart",
	}

	got := m.View()
	for _, want := range []string{"owns the checkout endpoint", "calls it from the cart"} {
		if !strings.Contains(got, want) {
			t.Errorf("view dropped %q:\n%s", want, got)
		}
	}
}

func TestViewAlignsNotes(t *testing.T) {
	m := New([]string{"a", "much-longer-name"}, nil)
	m.Notes = map[string]string{"a": "short", "much-longer-name": "long"}

	lines := strings.Split(m.View(), "\n")
	var cols []int
	for _, line := range lines {
		if i := strings.Index(line, "short"); i > 0 {
			cols = append(cols, i)
		}
		if i := strings.Index(line, "long"); i > 0 && !strings.Contains(line, "longer") {
			cols = append(cols, i)
		}
	}
	if len(cols) == 2 && cols[0] != cols[1] {
		t.Errorf("notes start at columns %v, want them aligned", cols)
	}
}

func TestViewShowsHeader(t *testing.T) {
	m := editor()
	m.Header = "suggested for: trace the checkout call"

	if !strings.Contains(m.View(), "trace the checkout call") {
		t.Errorf("view dropped the header:\n%s", m.View())
	}
}

// A repo added by hand has no note, and must still render.
func TestViewWithoutNotes(t *testing.T) {
	got := editor().View()

	if !strings.Contains(got, "services/api") || !strings.Contains(got, "working directory") {
		t.Errorf("view broke without notes:\n%s", got)
	}
}

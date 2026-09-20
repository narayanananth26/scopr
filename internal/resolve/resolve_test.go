package resolve_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scopr/internal/registry"
	"scopr/internal/resolve"
	"scopr/internal/scopefile"
)

func isolate(t *testing.T) {
	t.Helper()

	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("HOME", cfg)
}

func ws(t *testing.T, root string, parts ...string) string {
	t.Helper()

	dir := filepath.Join(append([]string{root}, parts...)...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := registry.Add(dir); err != nil {
		t.Fatalf("Add %s: %v", dir, err)
	}
	return dir
}

func unregistered(t *testing.T, root string, parts ...string) string {
	t.Helper()

	dir := filepath.Join(append([]string{root}, parts...)...)
	if err := os.MkdirAll(filepath.Join(dir, ".scopr"), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	return dir
}

func tempRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving temp dir: %v", err)
	}
	return root
}

func save(t *testing.T, root, name string, repos ...string) {
	t.Helper()

	if err := scopefile.Save(root, name, repos); err != nil {
		t.Fatalf("Save %s: %v", name, err)
	}
}

func TestOnePrefersTheFlag(t *testing.T) {
	isolate(t)
	root := tempRoot(t)
	a := ws(t, root, "Ananth", "Goodlife")
	b := ws(t, root, "work", "Other")

	got, err := resolve.One(resolve.Options{Workspace: "Other", Cwd: a})
	if err != nil {
		t.Fatalf("One: %v", err)
	}
	if got != b {
		t.Errorf("One = %q, want the flagged workspace %q", got, b)
	}
}

func TestOneUsesTheWorkspaceYouAreIn(t *testing.T) {
	isolate(t)
	root := tempRoot(t)
	a := ws(t, root, "Ananth", "Goodlife")
	ws(t, root, "work", "Other")

	inside := filepath.Join(a, "nested")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := resolve.One(resolve.Options{Cwd: inside})
	if err != nil {
		t.Fatalf("One: %v", err)
	}
	if got != a {
		t.Errorf("One = %q, want %q", got, a)
	}
}

func TestOneWorksInAnUnregisteredWorkspace(t *testing.T) {
	isolate(t)
	root := tempRoot(t)
	dir := unregistered(t, root, "Loose")

	got, err := resolve.One(resolve.Options{Cwd: dir})
	if err != nil {
		t.Fatalf("One: %v", err)
	}
	if got != dir {
		t.Errorf("One = %q, want %q", got, dir)
	}
}

func TestOneFallsBackToTheOnlyRegistered(t *testing.T) {
	isolate(t)
	root := tempRoot(t)
	only := ws(t, root, "Goodlife")

	outside := tempRoot(t)

	got, err := resolve.One(resolve.Options{Cwd: outside})
	if err != nil {
		t.Fatalf("One: %v", err)
	}
	if got != only {
		t.Errorf("One = %q, want %q", got, only)
	}
}

func TestOneRefusesToGuessBetweenMany(t *testing.T) {
	isolate(t)
	root := tempRoot(t)
	ws(t, root, "A")
	ws(t, root, "B")

	_, err := resolve.One(resolve.Options{Cwd: tempRoot(t)})
	if !errors.Is(err, resolve.ErrNoWorkspace) {
		t.Fatalf("One error = %v, want ErrNoWorkspace", err)
	}
}

func TestOneWithNothingRegisteredSaysHow(t *testing.T) {
	isolate(t)

	_, err := resolve.One(resolve.Options{Cwd: tempRoot(t)})
	if !errors.Is(err, resolve.ErrNoWorkspace) {
		t.Fatalf("One error = %v, want ErrNoWorkspace", err)
	}
	if got := err.Error(); !strings.Contains(got, "workspace add") {
		t.Errorf("error %q does not say how to register one", got)
	}
}

func TestOneAmbiguousFlagErrors(t *testing.T) {
	isolate(t)
	root := tempRoot(t)
	ws(t, root, "Ananth", "Goodlife")
	ws(t, root, "work", "Goodlife")

	_, err := resolve.One(resolve.Options{Workspace: "Goodlife", Cwd: tempRoot(t)})
	if !errors.Is(err, resolve.ErrAmbiguous) {
		t.Fatalf("One error = %v, want ErrAmbiguous", err)
	}
}

func TestScopeFindsItWhereYouAreStanding(t *testing.T) {
	isolate(t)
	root := tempRoot(t)
	a := ws(t, root, "Ananth", "Goodlife")
	b := ws(t, root, "work", "Other")

	save(t, a, "surfaces", "one")
	save(t, b, "surfaces", "two")

	hits, err := resolve.Scope(resolve.Options{Cwd: a}, "surfaces")
	if err != nil {
		t.Fatalf("Scope: %v", err)
	}
	if len(hits) != 1 || hits[0].Workspace.Path != a {
		t.Errorf("Scope = %v, want only the workspace we are standing in", hits)
	}
}

func TestScopeSearchesEveryWorkspace(t *testing.T) {
	isolate(t)
	root := tempRoot(t)
	a := ws(t, root, "Ananth", "Goodlife")
	b := ws(t, root, "work", "Other")

	save(t, a, "surfaces", "one")
	save(t, b, "surfaces", "two")

	hits, err := resolve.Scope(resolve.Options{Cwd: tempRoot(t)}, "surfaces")
	if err != nil {
		t.Fatalf("Scope: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("Scope = %v, want both workspaces", hits)
	}
}

func TestScopeFallsBackWhenLocalWorkspaceLacksIt(t *testing.T) {
	isolate(t)
	root := tempRoot(t)
	here := ws(t, root, "Here")
	there := ws(t, root, "There")

	save(t, there, "surfaces", "one")

	hits, err := resolve.Scope(resolve.Options{Cwd: here}, "surfaces")
	if err != nil {
		t.Fatalf("Scope: %v", err)
	}
	if len(hits) != 1 || hits[0].Workspace.Path != there {
		t.Errorf("Scope = %v, want the other workspace", hits)
	}
}

func TestScopeAcceptsTheAtPrefix(t *testing.T) {
	isolate(t)
	root := tempRoot(t)
	a := ws(t, root, "Goodlife")
	save(t, a, "surfaces", "one")

	hits, err := resolve.Scope(resolve.Options{Cwd: a}, "@surfaces")
	if err != nil {
		t.Fatalf("Scope: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("Scope = %v, want one hit", hits)
	}
}

func TestScopeUnknown(t *testing.T) {
	isolate(t)
	root := tempRoot(t)
	ws(t, root, "Goodlife")

	_, err := resolve.Scope(resolve.Options{Cwd: tempRoot(t)}, "nope")
	if !errors.Is(err, scopefile.ErrNoSuchScope) {
		t.Fatalf("Scope error = %v, want ErrNoSuchScope", err)
	}
}

func TestScopeWithFlagDoesNotSearchElsewhere(t *testing.T) {
	isolate(t)
	root := tempRoot(t)
	ws(t, root, "Empty")
	other := ws(t, root, "Other")
	save(t, other, "surfaces", "one")

	_, err := resolve.Scope(resolve.Options{Workspace: "Empty", Cwd: tempRoot(t)}, "surfaces")
	if !errors.Is(err, scopefile.ErrNoSuchScope) {
		t.Fatalf("Scope error = %v, want ErrNoSuchScope", err)
	}
}

func TestAllListsEveryScope(t *testing.T) {
	isolate(t)
	root := tempRoot(t)
	a := ws(t, root, "Ananth", "Goodlife")
	b := ws(t, root, "work", "Other")

	save(t, a, "surfaces", "one")
	save(t, a, "native", "two")
	save(t, b, "billing", "three")

	hits, err := resolve.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(hits) != 3 {
		t.Errorf("All = %v, want three scopes", hits)
	}
}

func TestAllSkipsUnregistered(t *testing.T) {
	isolate(t)
	root := tempRoot(t)
	loose := unregistered(t, root, "Loose")
	save(t, loose, "hidden", "one")

	hits, err := resolve.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("All = %v, want nothing", hits)
	}
}

package scopefile_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"scopr/internal/scopefile"
)

func root(t *testing.T) string {
	t.Helper()

	r, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving temp dir: %v", err)
	}
	return r
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	r := root(t)
	want := []string{"gl-panel", "gl-extension", "gl-webapp"}

	if err := scopefile.Save(r, "surfaces", want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := scopefile.Load(r, "surfaces")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("Load = %v, want %v", got, want)
	}
}

func TestLoadIgnoresCommentsAndBlanks(t *testing.T) {
	r := root(t)
	path := scopefile.Path(r, "seam")

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "# the checkout seam\n\n  web  \n\n# trailing note\napi\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := scopefile.Load(r, "seam")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := []string{"web", "api"}; !slices.Equal(got, want) {
		t.Errorf("Load = %v, want %v", got, want)
	}
}

func TestLoadMissingScope(t *testing.T) {
	if _, err := scopefile.Load(root(t), "nope"); !errors.Is(err, scopefile.ErrNoSuchScope) {
		t.Fatalf("Load error = %v, want ErrNoSuchScope", err)
	}
}

// A scope naming nothing must not read as a valid empty scope.
func TestLoadEmptyScopeErrors(t *testing.T) {
	r := root(t)
	path := scopefile.Path(r, "hollow")

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("# nothing here\n\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := scopefile.Load(r, "hollow"); !errors.Is(err, scopefile.ErrEmptyScope) {
		t.Fatalf("Load error = %v, want ErrEmptyScope", err)
	}
}

func TestSaveRefusesExisting(t *testing.T) {
	r := root(t)
	original := []string{"web"}

	if err := scopefile.Save(r, "seam", original); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := scopefile.Save(r, "seam", []string{"api"}); !errors.Is(err, scopefile.ErrScopeExists) {
		t.Fatalf("second Save error = %v, want ErrScopeExists", err)
	}

	got, err := scopefile.Load(r, "seam")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !slices.Equal(got, original) {
		t.Errorf("refused Save changed the scope: %v", got)
	}
}

func TestSaveCreatesScopesDir(t *testing.T) {
	r := root(t)

	if err := scopefile.Save(r, "seam", []string{"web"}); err != nil {
		t.Fatalf("Save into a fresh workspace: %v", err)
	}
	if _, err := os.Stat(scopefile.Path(r, "seam")); err != nil {
		t.Errorf("scope file missing: %v", err)
	}
}

func TestSaveRefusesEmpty(t *testing.T) {
	if err := scopefile.Save(root(t), "seam", nil); !errors.Is(err, scopefile.ErrEmptyScope) {
		t.Fatalf("Save error = %v, want ErrEmptyScope", err)
	}
}

func TestRejectsPathSeparatorInName(t *testing.T) {
	r := root(t)

	if err := scopefile.Save(r, filepath.Join("a", "b"), []string{"web"}); !errors.Is(err, scopefile.ErrInvalidName) {
		t.Fatalf("Save error = %v, want ErrInvalidName", err)
	}
	if _, err := scopefile.Load(r, filepath.Join("a", "b")); !errors.Is(err, scopefile.ErrInvalidName) {
		t.Fatalf("Load error = %v, want ErrInvalidName", err)
	}
}

// The error is not enough: assert nothing was written outside the workspace.
func TestRejectsDotDotName(t *testing.T) {
	parent := root(t)
	r := filepath.Join(parent, "workspace")
	if err := os.MkdirAll(r, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	escape := filepath.Join("..", "..", "escaped")
	if err := scopefile.Save(r, escape, []string{"web"}); !errors.Is(err, scopefile.ErrInvalidName) {
		t.Fatalf("Save error = %v, want ErrInvalidName", err)
	}

	for _, candidate := range []string{
		filepath.Join(parent, "escaped"),
		filepath.Join(parent, "..", "escaped"),
		filepath.Join(r, "escaped"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			t.Errorf("Save wrote outside the scopes directory: %s", candidate)
		}
	}
}

func TestRejectsDottedName(t *testing.T) {
	if err := scopefile.Save(root(t), ".hidden", []string{"web"}); !errors.Is(err, scopefile.ErrInvalidName) {
		t.Fatalf("Save error = %v, want ErrInvalidName", err)
	}
}

func TestListSorted(t *testing.T) {
	r := root(t)

	for _, name := range []string{"native", "surfaces", "onboarding"} {
		if err := scopefile.Save(r, name, []string{"web"}); err != nil {
			t.Fatalf("Save %s: %v", name, err)
		}
	}

	got, err := scopefile.List(r)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if want := []string{"native", "onboarding", "surfaces"}; !slices.Equal(got, want) {
		t.Errorf("List = %v, want %v", got, want)
	}
}

func TestListMissingDirIsEmpty(t *testing.T) {
	got, err := scopefile.List(root(t))
	if err != nil {
		t.Fatalf("List on a fresh workspace: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List = %v, want none", got)
	}
}

// Save's temp files are dotted so a crash mid-write leaves nothing listable.
func TestListSkipsDotfiles(t *testing.T) {
	r := root(t)

	if err := scopefile.Save(r, "seam", []string{"web"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	stray := filepath.Join(filepath.Dir(scopefile.Path(r, "seam")), ".seam.tmp123")
	if err := os.WriteFile(stray, []byte("web\n"), 0o644); err != nil {
		t.Fatalf("write stray: %v", err)
	}

	got, err := scopefile.List(r)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if want := []string{"seam"}; !slices.Equal(got, want) {
		t.Errorf("List = %v, want %v", got, want)
	}
}

func TestRenameMovesScope(t *testing.T) {
	r := root(t)
	want := []string{"web", "api"}

	if err := scopefile.Save(r, "old", want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := scopefile.Rename(r, "old", "new"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	if _, err := scopefile.Load(r, "old"); !errors.Is(err, scopefile.ErrNoSuchScope) {
		t.Errorf("old scope still loads: %v", err)
	}
	got, err := scopefile.Load(r, "new")
	if err != nil {
		t.Fatalf("Load new: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("renamed scope = %v, want %v", got, want)
	}
}

func TestRenameMissingSource(t *testing.T) {
	if err := scopefile.Rename(root(t), "nope", "new"); !errors.Is(err, scopefile.ErrNoSuchScope) {
		t.Fatalf("Rename error = %v, want ErrNoSuchScope", err)
	}
}

func TestRenameRefusesExistingTarget(t *testing.T) {
	r := root(t)

	if err := scopefile.Save(r, "from", []string{"web"}); err != nil {
		t.Fatalf("Save from: %v", err)
	}
	if err := scopefile.Save(r, "to", []string{"api"}); err != nil {
		t.Fatalf("Save to: %v", err)
	}

	if err := scopefile.Rename(r, "from", "to"); !errors.Is(err, scopefile.ErrScopeExists) {
		t.Fatalf("Rename error = %v, want ErrScopeExists", err)
	}

	for name, want := range map[string][]string{"from": {"web"}, "to": {"api"}} {
		got, err := scopefile.Load(r, name)
		if err != nil {
			t.Fatalf("Load %s: %v", name, err)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
}

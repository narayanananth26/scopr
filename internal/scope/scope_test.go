package scope_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scopr/internal/repo"
	"scopr/internal/scope"
)

// fixture mirrors internal/repo's: web appears in two containers so it is
// ambiguous, api is unique.
//
//	apps/web/.git/      repo
//	apps/shared/.git/   repo
//	services/api/.git/  repo
//	worktrees/web/.git/ repo
func fixture(t *testing.T) string {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving temp dir: %v", err)
	}

	for _, d := range []string{
		"apps/web/.git",
		"apps/shared/.git",
		"services/api/.git",
		"worktrees/web/.git",
	} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	return root
}

func TestResolvesSingleRepo(t *testing.T) {
	root := fixture(t)

	got, err := scope.Resolve(root, []string{"api"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := filepath.Join(root, "services/api"); got.Primary().Path != want {
		t.Errorf("Primary = %q, want %q", got.Primary().Path, want)
	}
	if len(got.Others()) != 0 {
		t.Errorf("Others = %v, want empty", got.Others())
	}
	if got.Root != root {
		t.Errorf("Root = %q, want %q", got.Root, root)
	}
}

// Order is not cosmetic: the first repo becomes cwd and decides whose config
// loads.
func TestPreservesArgumentOrder(t *testing.T) {
	root := fixture(t)

	first, err := scope.Resolve(root, []string{"api", "shared"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	second, err := scope.Resolve(root, []string{"shared", "api"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if first.Primary().Path == second.Primary().Path {
		t.Fatalf("both orders gave primary %q", first.Primary().Path)
	}
	if got, want := filepath.Base(first.Primary().Path), "api"; got != want {
		t.Errorf("first primary = %q, want %q", got, want)
	}
	if got, want := filepath.Base(second.Primary().Path), "shared"; got != want {
		t.Errorf("second primary = %q, want %q", got, want)
	}
}

func TestEmptyNamesErrors(t *testing.T) {
	root := fixture(t)

	got, err := scope.Resolve(root, nil)
	if !errors.Is(err, scope.ErrEmpty) {
		t.Fatalf("Resolve error = %v, want ErrEmpty", err)
	}
	if len(got.Repos) != 0 {
		t.Errorf("Repos = %v, want none", got.Repos)
	}
}

func TestUnknownNameErrors(t *testing.T) {
	root := fixture(t)

	if _, err := scope.Resolve(root, []string{"nope"}); !errors.Is(err, repo.ErrNoSuchRepo) {
		t.Fatalf("Resolve error = %v, want ErrNoSuchRepo", err)
	}
}

// One run reports every bad name. Failing on the first means fixing typos one
// rerun at a time.
func TestReportsAllBadNames(t *testing.T) {
	root := fixture(t)

	_, err := scope.Resolve(root, []string{"nope", "alsonope"})
	if err == nil {
		t.Fatal("Resolve returned no error")
	}
	for _, want := range []string{"nope", "alsonope"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestAmbiguousNameErrors(t *testing.T) {
	root := fixture(t)

	if _, err := scope.Resolve(root, []string{"web"}); !errors.Is(err, repo.ErrAmbiguous) {
		t.Fatalf("Resolve error = %v, want ErrAmbiguous", err)
	}
}

func TestDuplicateNameErrors(t *testing.T) {
	root := fixture(t)

	if _, err := scope.Resolve(root, []string{"api", "api"}); !errors.Is(err, scope.ErrDuplicate) {
		t.Fatalf("Resolve error = %v, want ErrDuplicate", err)
	}
}

// The same repo reached by two spellings is still one repo.
func TestDuplicateByDifferentSpellingErrors(t *testing.T) {
	root := fixture(t)

	_, err := scope.Resolve(root, []string{"api", filepath.Join("services", "api")})
	if !errors.Is(err, scope.ErrDuplicate) {
		t.Fatalf("Resolve error = %v, want ErrDuplicate", err)
	}
}

// A partial scope is never returned. Silently dropping the bad name would give
// a session scoped to less than was asked for, which surfaces as the agent
// failing to find code rather than as a scoping error.
func TestMixedGoodAndBadErrors(t *testing.T) {
	root := fixture(t)

	got, err := scope.Resolve(root, []string{"api", "nope"})
	if err == nil {
		t.Fatal("Resolve returned no error")
	}
	if len(got.Repos) != 0 {
		t.Errorf("Repos = %v, want none on partial failure", got.Repos)
	}
}

func TestDisambiguatesByRelativePath(t *testing.T) {
	root := fixture(t)

	got, err := scope.Resolve(root, []string{filepath.Join("apps", "web")})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := filepath.Join(root, "apps/web"); got.Primary().Path != want {
		t.Errorf("Primary = %q, want %q", got.Primary().Path, want)
	}
}

// Name keeps repo.List's meaning, the base name, whatever spelling was typed.
func TestNameIsTheBaseName(t *testing.T) {
	root := fixture(t)

	got, err := scope.Resolve(root, []string{filepath.Join("apps", "web")})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := "web"; got.Primary().Name != want {
		t.Errorf("Name = %q, want %q", got.Primary().Name, want)
	}
}

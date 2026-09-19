package scope_test

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"scopr/internal/repo"
	"scopr/internal/scope"
	"scopr/internal/scopefile"
)

// saved writes a scope into the fixture workspace.
func saved(t *testing.T, root, name string, repos ...string) {
	t.Helper()

	if err := scopefile.Save(root, name, repos); err != nil {
		t.Fatalf("Save %s: %v", name, err)
	}
}

// bases returns the resolved repos by base name, for readable assertions.
func bases(s scope.Scope) []string {
	out := make([]string, 0, len(s.Repos))
	for _, r := range s.Repos {
		out = append(out, r.Name)
	}
	return out
}

func TestExpandsScopeInPlace(t *testing.T) {
	root := fixture(t)
	saved(t, root, "seam", "api", "shared")

	got, err := scope.ResolveArgs(root, []string{"@seam"})
	if err != nil {
		t.Fatalf("ResolveArgs: %v", err)
	}
	if want := []string{"api", "shared"}; !slices.Equal(bases(got), want) {
		t.Errorf("repos = %v, want %v", bases(got), want)
	}
}

// An expanded scope keeps its position, so the extra repo lands after it.
func TestMixesScopesAndRepos(t *testing.T) {
	root := fixture(t)
	saved(t, root, "seam", "api", "shared")

	got, err := scope.ResolveArgs(root, []string{"@seam", filepath.Join("apps", "web")})
	if err != nil {
		t.Fatalf("ResolveArgs: %v", err)
	}
	if want := []string{"api", "shared", "web"}; !slices.Equal(bases(got), want) {
		t.Errorf("repos = %v, want %v", bases(got), want)
	}
}

func TestPlainReposUnaffected(t *testing.T) {
	root := fixture(t)

	got, err := scope.ResolveArgs(root, []string{"api", "shared"})
	if err != nil {
		t.Fatalf("ResolveArgs: %v", err)
	}
	if want := []string{"api", "shared"}; !slices.Equal(bases(got), want) {
		t.Errorf("repos = %v, want %v", bases(got), want)
	}
}

func TestExpandsMultipleScopes(t *testing.T) {
	root := fixture(t)
	saved(t, root, "one", "api")
	saved(t, root, "two", "shared")

	got, err := scope.ResolveArgs(root, []string{"@one", "@two"})
	if err != nil {
		t.Fatalf("ResolveArgs: %v", err)
	}
	if want := []string{"api", "shared"}; !slices.Equal(bases(got), want) {
		t.Errorf("repos = %v, want %v", bases(got), want)
	}
}

func TestUnknownScopeErrors(t *testing.T) {
	root := fixture(t)

	if _, err := scope.ResolveArgs(root, []string{"@nope"}); !errors.Is(err, scopefile.ErrNoSuchScope) {
		t.Fatalf("ResolveArgs error = %v, want ErrNoSuchScope", err)
	}
}

func TestReportsAllBadScopes(t *testing.T) {
	root := fixture(t)

	_, err := scope.ResolveArgs(root, []string{"@nope", "@alsonope"})
	if err == nil {
		t.Fatal("ResolveArgs returned no error")
	}
	for _, want := range []string{"nope", "alsonope"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// Two scopes sharing a repo collide rather than merging: a silent union would
// make the primary depend on which scope was listed first.
func TestOverlappingScopesError(t *testing.T) {
	root := fixture(t)
	saved(t, root, "one", "api", "shared")
	saved(t, root, "two", "shared")

	if _, err := scope.ResolveArgs(root, []string{"@one", "@two"}); !errors.Is(err, scope.ErrDuplicate) {
		t.Fatalf("ResolveArgs error = %v, want ErrDuplicate", err)
	}
}

// A scope naming a repo that no longer exists must say which scope is stale,
// not just which repo is missing.
func TestStaleScopeNamesTheScope(t *testing.T) {
	root := fixture(t)
	saved(t, root, "stale", "api", "deleted-repo")

	_, err := scope.ResolveArgs(root, []string{"@stale"})
	if !errors.Is(err, repo.ErrNoSuchRepo) {
		t.Fatalf("ResolveArgs error = %v, want ErrNoSuchRepo", err)
	}
	if !strings.Contains(err.Error(), "@stale") {
		t.Errorf("error %q does not name the stale scope", err)
	}
}

// A repo typed directly is not attributed to any scope.
func TestDirectNameNotAttributed(t *testing.T) {
	root := fixture(t)

	_, err := scope.ResolveArgs(root, []string{"nope"})
	if err == nil {
		t.Fatal("ResolveArgs returned no error")
	}
	if strings.Contains(err.Error(), scope.Prefix) {
		t.Errorf("error %q attributes a directly typed name to a scope", err)
	}
}

func TestEmptyArgsErrors(t *testing.T) {
	if _, err := scope.ResolveArgs(fixture(t), nil); !errors.Is(err, scope.ErrEmpty) {
		t.Fatalf("ResolveArgs error = %v, want ErrEmpty", err)
	}
}

// Two scopes claiming one repo must name both scopes; "x and x are both /p"
// reads as a bug rather than a collision.
func TestOverlapNamesBothScopes(t *testing.T) {
	root := fixture(t)
	saved(t, root, "one", "api", "shared")
	saved(t, root, "two", "shared")

	_, err := scope.ResolveArgs(root, []string{"@one", "@two"})
	if err == nil {
		t.Fatal("ResolveArgs returned no error")
	}
	for _, want := range []string{"@one", "@two", "shared"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestOverlapWithDirectNameNamesTheScope(t *testing.T) {
	root := fixture(t)
	saved(t, root, "one", "api")

	_, err := scope.ResolveArgs(root, []string{"@one", "api"})
	if err == nil {
		t.Fatal("ResolveArgs returned no error")
	}
	if !strings.Contains(err.Error(), "@one") {
		t.Errorf("error %q does not name the scope holding it", err)
	}
}

func TestScopeNamingSameRepoTwice(t *testing.T) {
	root := fixture(t)
	saved(t, root, "dup", "api", "api")

	_, err := scope.ResolveArgs(root, []string{"@dup"})
	if err == nil {
		t.Fatal("ResolveArgs returned no error")
	}
	if !strings.Contains(err.Error(), "@dup") || !strings.Contains(err.Error(), "twice") {
		t.Errorf("error %q should say @dup names it twice", err)
	}
}

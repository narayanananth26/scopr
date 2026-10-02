package repo_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"scopr/internal/repo"
)

func fixture(t *testing.T) string {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving temp dir: %v", err)
	}

	dirs := []string{
		"apps/web/.git",
		"apps/web/node_modules/pkg",
		"apps/shared/.git",
		"services/api/.git",
		"worktrees/web/src",
		"docs/a/b/c/d/e",
		"Some Folder",
		".scopr",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	gitFile := filepath.Join(root, "worktrees/web/.git")
	if err := os.WriteFile(gitFile, []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", gitFile, err)
	}

	return root
}

func rels(t *testing.T, root string, repos []repo.Repo) []string {
	t.Helper()

	out := make([]string, 0, len(repos))
	for _, r := range repos {
		rel, err := filepath.Rel(root, r.Path)
		if err != nil {
			t.Fatalf("rel %s: %v", r.Path, err)
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}

func listRels(t *testing.T, root string) []string {
	t.Helper()

	repos, err := repo.List(root, nil)
	if err != nil {
		t.Fatalf("List(%q): %v", root, err)
	}
	return rels(t, root, repos)
}

func TestListsReposAtBothLevels(t *testing.T) {
	root := fixture(t)
	got := listRels(t, root)

	for _, want := range []string{"apps/web", "services/api"} {
		if !slices.Contains(got, want) {
			t.Errorf("List missing %q; got %v", want, got)
		}
	}
}

func TestListsContainerDirs(t *testing.T) {
	root := fixture(t)
	got := listRels(t, root)

	for _, want := range []string{"apps", "services"} {
		if !slices.Contains(got, want) {
			t.Errorf("List missing container %q; got %v", want, got)
		}
	}
}

func TestSkipsDotDirectories(t *testing.T) {
	root := fixture(t)

	for _, path := range listRels(t, root) {
		for _, seg := range strings.Split(path, "/") {
			if strings.HasPrefix(seg, ".") {
				t.Errorf("List returned dotted entry %q", path)
			}
		}
	}
}

func TestDoesNotDescendIntoRepos(t *testing.T) {
	root := fixture(t)

	for _, path := range listRels(t, root) {
		if path == "apps/web/node_modules" {
			t.Fatalf("List descended into a repo: found %q", path)
		}
	}
}

func TestTreatsGitFileAsRepo(t *testing.T) {
	root := fixture(t)
	got := listRels(t, root)

	if !slices.Contains(got, "worktrees/web") {
		t.Fatalf("List missing worktree repo; got %v", got)
	}
	for _, path := range got {
		if strings.HasPrefix(path, "worktrees/web/") {
			t.Errorf("List descended past a .git file: %q", path)
		}
	}
}

func TestRespectsDepthCap(t *testing.T) {
	root := fixture(t)
	got := listRels(t, root)

	if !slices.Contains(got, "docs/a/b/c") {
		t.Errorf("depth 4 should be listed; got %v", got)
	}
	if slices.Contains(got, "docs/a/b/c/d") {
		t.Errorf("depth 5 should not be listed; got %v", got)
	}
}

func TestSortedByPath(t *testing.T) {
	root := fixture(t)

	first := listRels(t, root)
	second := listRels(t, root)

	if !slices.Equal(first, second) {
		t.Errorf("List order unstable:\n first: %v\nsecond: %v", first, second)
	}
	if !slices.IsSorted(first) {
		t.Errorf("List not sorted by path: %v", first)
	}
}

func TestResolvesUniqueBaseName(t *testing.T) {
	root := fixture(t)

	got, err := repo.Resolve(root, nil, "api")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := filepath.Join(root, "services/api"); got != want {
		t.Errorf("Resolve = %q, want %q", got, want)
	}
}

func TestResolveAmbiguousErrors(t *testing.T) {
	root := fixture(t)

	got, err := repo.Resolve(root, nil, "web")
	if !errors.Is(err, repo.ErrAmbiguous) {
		t.Fatalf("Resolve error = %v, want ErrAmbiguous", err)
	}
	if got != "" {
		t.Errorf("Resolve = %q, want empty path on ambiguity", got)
	}

	var ambig *repo.AmbiguousError
	if !errors.As(err, &ambig) {
		t.Fatalf("error is not *AmbiguousError: %v", err)
	}
	if len(ambig.Matches) != 2 {
		t.Errorf("Matches = %v, want both candidates", rels(t, root, ambig.Matches))
	}
}

func TestResolveByRelativePath(t *testing.T) {
	root := fixture(t)

	got, err := repo.Resolve(root, nil, filepath.Join("apps", "web"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := filepath.Join(root, "apps/web"); got != want {
		t.Errorf("Resolve = %q, want %q", got, want)
	}
}

func TestResolveUnknownName(t *testing.T) {
	root := fixture(t)

	if _, err := repo.Resolve(root, nil, "nope"); !errors.Is(err, repo.ErrNoSuchRepo) {
		t.Fatalf("Resolve error = %v, want ErrNoSuchRepo", err)
	}
}

func TestResolveInUsesGivenListing(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "nonexistent-root")
	repos := []repo.Repo{
		{Name: "panel", Path: filepath.Join(root, "apps", "panel")},
	}

	got, err := repo.ResolveIn(root, repos, "panel")
	if err != nil {
		t.Fatalf("ResolveIn: %v", err)
	}
	if want := filepath.Join(root, "apps", "panel"); got != want {
		t.Errorf("ResolveIn = %q, want %q", got, want)
	}
}

func nested(t *testing.T) (string, []string) {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving temp dir: %v", err)
	}

	dirs := []string{
		"work/.git",
		"work/api/.git",
		"deep/a/b/c/x/y/z/w/v/u",
		"mono/.git",
		"mono/ws/svc/.git",
		"mono/other",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	workspaces := []string{
		filepath.Join(root, "work"),
		filepath.Join(root, "deep/a/b/c/x"),
		filepath.Join(root, "mono/ws"),
		filepath.Join(filepath.Dir(root), "elsewhere"),
	}
	return root, workspaces
}

func listNested(t *testing.T) []string {
	t.Helper()

	root, workspaces := nested(t)
	repos, err := repo.List(root, workspaces)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return rels(t, root, repos)
}

func TestSkipsRegisteredWorkspaceRoots(t *testing.T) {
	got := listNested(t)

	for _, ws := range []string{"work", "deep/a/b/c/x", "mono/ws"} {
		if slices.Contains(got, ws) {
			t.Errorf("List returned workspace root %q; got %v", ws, got)
		}
	}
}

func TestListsReposInsideChildWorkspaces(t *testing.T) {
	got := listNested(t)

	if !slices.Contains(got, "work/api") {
		t.Errorf("List missing repo inside a child workspace; got %v", got)
	}
}

func TestDepthRestartsAtChildWorkspace(t *testing.T) {
	got := listNested(t)

	if !slices.Contains(got, "deep/a/b/c/x/y/z/w/v") {
		t.Errorf("depth 4 below a child workspace should be listed; got %v", got)
	}
	if slices.Contains(got, "deep/a/b/c/x/y/z/w/v/u") {
		t.Errorf("depth 5 below a child workspace should not be listed; got %v", got)
	}
}

func TestReachesChildWorkspaceInsideRepo(t *testing.T) {
	got := listNested(t)

	for _, want := range []string{"mono", "mono/ws/svc"} {
		if !slices.Contains(got, want) {
			t.Errorf("List missing %q; got %v", want, got)
		}
	}
	if slices.Contains(got, "mono/other") {
		t.Errorf("List descended into a repo beyond the path to a workspace; got %v", got)
	}
}

func lock(t *testing.T, path string, mode os.FileMode) {
	t.Helper()

	if os.Geteuid() == 0 {
		t.Skip("permissions do not apply to root")
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o755) })
}

func TestSkipsPermissionDeniedDirectories(t *testing.T) {
	for name, tc := range map[string]struct {
		mode   os.FileMode
		listed bool
	}{
		"cannot inspect": {mode: 0o000, listed: false},
		"cannot read":    {mode: 0o100, listed: true},
	} {
		t.Run(name, func(t *testing.T) {
			root := fixture(t)
			locked := filepath.Join(root, "locked")
			if err := os.Mkdir(locked, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			lock(t, locked, tc.mode)

			got := listRels(t, root)

			if slices.Contains(got, "locked") != tc.listed {
				t.Errorf("List listed locked = %v, want %v; got %v", !tc.listed, tc.listed, got)
			}
			if !slices.Contains(got, "apps/web") {
				t.Errorf("List dropped readable repos; got %v", got)
			}
		})
	}
}

func TestFailsWhenRootIsUnreadable(t *testing.T) {
	root := fixture(t)
	lock(t, root, 0o000)

	if _, err := repo.List(root, nil); !errors.Is(err, os.ErrPermission) {
		t.Errorf("List error = %v, want permission denied", err)
	}
}

func TestWalksHiddenWorkspaces(t *testing.T) {
	root := fixture(t)
	for _, d := range []string{".config/zsh", ".config/nvim/.git", ".config/.claude/agents"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	repos, err := repo.List(root, []string{filepath.Join(root, ".config")})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := rels(t, root, repos)

	for _, want := range []string{".config/zsh", ".config/nvim"} {
		if !slices.Contains(got, want) {
			t.Errorf("List missing %q; got %v", want, got)
		}
	}
	for _, unwanted := range []string{".config", ".config/.claude", ".config/.claude/agents"} {
		if slices.Contains(got, unwanted) {
			t.Errorf("List contains %q; got %v", unwanted, got)
		}
	}
}

func TestResolveRejectsUnregisteredHiddenDirectories(t *testing.T) {
	root := fixture(t)
	if err := os.MkdirAll(filepath.Join(root, ".config", "zsh"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	for _, name := range []string{".config", filepath.Join(".config", "zsh"), "zsh"} {
		if _, err := repo.Resolve(root, nil, name); !errors.Is(err, repo.ErrNoSuchRepo) {
			t.Errorf("Resolve(%q) error = %v, want ErrNoSuchRepo", name, err)
		}
	}
}

func TestResolvesInsideHiddenWorkspace(t *testing.T) {
	root := fixture(t)
	if err := os.MkdirAll(filepath.Join(root, ".config", "zsh"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	workspaces := []string{filepath.Join(root, ".config")}

	for _, name := range []string{"zsh", filepath.Join(".config", "zsh")} {
		got, err := repo.Resolve(root, workspaces, name)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", name, err)
		}
		if want := filepath.Join(root, ".config", "zsh"); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestResolveRejectsFilesAndEscapes(t *testing.T) {
	root := fixture(t)
	if err := os.WriteFile(filepath.Join(root, ".zshrc"), nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	for _, name := range []string{".zshrc", "..", filepath.Join("..", "elsewhere")} {
		if _, err := repo.Resolve(root, nil, name); !errors.Is(err, repo.ErrNoSuchRepo) {
			t.Errorf("Resolve(%q) error = %v, want ErrNoSuchRepo", name, err)
		}
	}
}

package registry

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func isolate(t *testing.T) string {
	t.Helper()

	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("HOME", cfg)

	return cfg
}

func workspaceDir(t *testing.T, parts ...string) string {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving temp dir: %v", err)
	}

	dir := filepath.Join(append([]string{root}, parts...)...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	return dir
}

func paths(in []Workspace) []string {
	out := make([]string, 0, len(in))
	for _, w := range in {
		out = append(out, w.Path)
	}
	return out
}

func names(in []Workspace) []string {
	out := make([]string, 0, len(in))
	for _, w := range in {
		out = append(out, w.Name)
	}
	return out
}

func TestAddCreatesMarkerDirectory(t *testing.T) {
	isolate(t)
	dir := workspaceDir(t, "Goodlife")

	if err := Add(dir); err != nil {
		t.Fatalf("Add: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, marker))
	if err != nil {
		t.Fatalf("marker missing: %v", err)
	}
	if !info.IsDir() {
		t.Error("marker is not a directory")
	}
}

func TestAddIsIdempotent(t *testing.T) {
	isolate(t)
	dir := workspaceDir(t, "Goodlife")

	for range 3 {
		if err := Add(dir); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	got, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("List = %v, want one entry", paths(got))
	}
}

func TestAddRejectsNonDirectory(t *testing.T) {
	isolate(t)
	dir := workspaceDir(t, "Goodlife")

	file := filepath.Join(dir, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := Add(file); !errors.Is(err, ErrNotDirectory) {
		t.Fatalf("Add error = %v, want ErrNotDirectory", err)
	}

	got, _ := List()
	if len(got) != 0 {
		t.Errorf("List = %v, want nothing registered", paths(got))
	}
}

func TestRemoveDropsOnlyThatEntry(t *testing.T) {
	isolate(t)
	a, b := workspaceDir(t, "A"), workspaceDir(t, "B")

	for _, d := range []string{a, b} {
		if err := Add(d); err != nil {
			t.Fatalf("Add %s: %v", d, err)
		}
	}
	if err := Remove(a); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	got, _ := List()
	if want := []string{b}; !slices.Equal(paths(got), want) {
		t.Errorf("List = %v, want %v", paths(got), want)
	}
}

func TestRemoveUnknownErrors(t *testing.T) {
	isolate(t)

	if err := Remove("/nowhere"); !errors.Is(err, ErrNoSuchEntry) {
		t.Fatalf("Remove error = %v, want ErrNoSuchEntry", err)
	}
}

func TestRemoveKeepsTheMarker(t *testing.T) {
	isolate(t)
	dir := workspaceDir(t, "Goodlife")

	if err := Add(dir); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := Remove(dir); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, marker)); err != nil {
		t.Errorf("Remove deleted the marker: %v", err)
	}
}

func TestListMarksMissingPathsStale(t *testing.T) {
	isolate(t)
	a, b := workspaceDir(t, "A"), workspaceDir(t, "B")

	for _, d := range []string{a, b} {
		if err := Add(d); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	if err := os.RemoveAll(a); err != nil {
		t.Fatalf("remove %s: %v", a, err)
	}

	all, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List = %v, want both entries", paths(all))
	}

	var staleCount int
	for _, w := range all {
		if w.Stale {
			staleCount++
			if w.Path != a {
				t.Errorf("wrong entry marked stale: %s", w.Path)
			}
		}
	}
	if staleCount != 1 {
		t.Errorf("%d stale entries, want 1", staleCount)
	}

	live, err := Live()
	if err != nil {
		t.Fatalf("Live: %v", err)
	}
	if want := []string{b}; !slices.Equal(paths(live), want) {
		t.Errorf("Live = %v, want %v", paths(live), want)
	}
}

func TestListMarksMarkerlessStale(t *testing.T) {
	isolate(t)
	dir := workspaceDir(t, "Goodlife")

	if err := Add(dir); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(dir, marker)); err != nil {
		t.Fatalf("remove marker: %v", err)
	}

	all, _ := List()
	if len(all) != 1 || !all[0].Stale {
		t.Errorf("List = %+v, want it marked stale", all)
	}
}

func TestMissingRegistryIsEmpty(t *testing.T) {
	isolate(t)

	got, err := List()
	if err != nil {
		t.Fatalf("List on a fresh machine: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List = %v, want nothing", paths(got))
	}
}

func TestNamesAreBasenamesWhenUnique(t *testing.T) {
	got := Names([]string{"/Users/h/Desktop/Ananth/Goodlife", "/Users/h/Desktop/Ananth/scopr"})

	if want := []string{"Goodlife", "scopr"}; !slices.Equal(got, want) {
		t.Errorf("Names = %v, want %v", got, want)
	}
}

func TestCollidingNamesGrowByOneSegment(t *testing.T) {
	got := Names([]string{"/Users/h/Desktop/Ananth/Goodlife", "/Users/h/work/Goodlife"})

	if want := []string{"Ananth/Goodlife", "work/Goodlife"}; !slices.Equal(got, want) {
		t.Errorf("Names = %v, want %v", got, want)
	}
}

func TestUninvolvedNamesDoNotGrow(t *testing.T) {
	got := Names([]string{
		"/Users/h/Desktop/Ananth/Goodlife",
		"/Users/h/work/Goodlife",
		"/Users/h/Desktop/Ananth/scopr",
	})

	if got[2] != "scopr" {
		t.Errorf("Names = %v, want scopr unchanged", got)
	}
}

func TestCollisionResolvesAtDepthTwo(t *testing.T) {
	got := Names([]string{"/a/x/shared/repo", "/a/y/shared/repo"})

	if want := []string{"x/shared/repo", "y/shared/repo"}; !slices.Equal(got, want) {
		t.Errorf("Names = %v, want %v", got, want)
	}
}

func TestNamesHandlesShortPaths(t *testing.T) {
	got := Names([]string{"/repo", "/a/repo"})

	if len(got) != 2 || got[0] == got[1] {
		t.Errorf("Names = %v, want distinct names", got)
	}
}

func TestLookupByName(t *testing.T) {
	isolate(t)
	dir := workspaceDir(t, "Goodlife")

	if err := Add(dir); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := Lookup("Goodlife")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(got) != 1 || got[0].Path != dir {
		t.Errorf("Lookup = %v, want %s", paths(got), dir)
	}
}

func TestLookupByPathSuffix(t *testing.T) {
	isolate(t)
	dir := workspaceDir(t, "nested", "Goodlife")

	if err := Add(dir); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := Lookup("nested/Goodlife")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("Lookup = %v, want one match", paths(got))
	}
}

func TestLookupByFullPath(t *testing.T) {
	isolate(t)
	dir := workspaceDir(t, "Goodlife")

	if err := Add(dir); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := Lookup(dir)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("Lookup = %v, want one match", paths(got))
	}
}

func TestLookupRejectsPartialSegment(t *testing.T) {
	isolate(t)
	dir := workspaceDir(t, "Goodlife")

	if err := Add(dir); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if _, err := Lookup("life"); !errors.Is(err, ErrNoSuchEntry) {
		t.Fatalf("Lookup error = %v, want ErrNoSuchEntry", err)
	}
}

func TestLookupAmbiguousReturnsAll(t *testing.T) {
	isolate(t)
	a := workspaceDir(t, "Ananth", "Goodlife")
	b := workspaceDir(t, "work", "Goodlife")

	for _, d := range []string{a, b} {
		if err := Add(d); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	got, err := Lookup("Goodlife")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("Lookup = %v, want both", paths(got))
	}

	all, _ := List()
	for _, n := range names(all) {
		if n == "Goodlife" {
			t.Errorf("names = %v, want both disambiguated", names(all))
		}
	}
}

func TestLookupUnknown(t *testing.T) {
	isolate(t)

	if _, err := Lookup("nope"); !errors.Is(err, ErrNoSuchEntry) {
		t.Fatalf("Lookup error = %v, want ErrNoSuchEntry", err)
	}
}

func TestLookupSkipsStale(t *testing.T) {
	isolate(t)
	dir := workspaceDir(t, "Goodlife")

	if err := Add(dir); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if _, err := Lookup("Goodlife"); !errors.Is(err, ErrNoSuchEntry) {
		t.Fatalf("Lookup error = %v, want ErrNoSuchEntry", err)
	}
}

func TestRegistryHonoursXDGConfigHome(t *testing.T) {
	cfg := isolate(t)

	got, err := file()
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if want := filepath.Join(cfg, "scopr", "workspaces"); got != want {
		t.Errorf("registry at %q, want %q", got, want)
	}
}

func TestRegistryFallsBackToDotConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)

	got, err := file()
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if want := filepath.Join(home, ".config", "scopr", "workspaces"); got != want {
		t.Errorf("registry at %q, want %q", got, want)
	}
}

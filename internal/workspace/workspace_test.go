package workspace_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"scopr/internal/workspace"
)

func tempRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving temp dir: %v", err)
	}
	return root
}

func mkdir(t *testing.T, path string) string {
	t.Helper()

	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	return path
}

func markDir(t *testing.T, dir string) string {
	t.Helper()

	mkdir(t, filepath.Join(dir, ".scopr"))
	return dir
}

func writeFile(t *testing.T, path string) string {
	t.Helper()

	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestFindsMarkerInStartDir(t *testing.T) {
	root := markDir(t, tempRoot(t))

	got, err := workspace.Find(root)
	if err != nil {
		t.Fatalf("Find(%q) returned error: %v", root, err)
	}
	if got != root {
		t.Errorf("Find(%q) = %q, want %q", root, got, root)
	}
}

func TestFindsMarkerInParent(t *testing.T) {
	root := markDir(t, tempRoot(t))
	child := mkdir(t, filepath.Join(root, "child"))

	got, err := workspace.Find(child)
	if err != nil {
		t.Fatalf("Find(%q) returned error: %v", child, err)
	}
	if got != root {
		t.Errorf("Find(%q) = %q, want the parent %q", child, got, root)
	}
}

func TestFindsMarkerSeveralLevelsUp(t *testing.T) {
	root := markDir(t, tempRoot(t))
	deep := mkdir(t, filepath.Join(root, "a", "b", "c", "d"))

	got, err := workspace.Find(deep)
	if err != nil {
		t.Fatalf("Find(%q) returned error: %v", deep, err)
	}
	if got != root {
		t.Errorf("Find(%q) = %q, want %q", deep, got, root)
	}
}

func TestReturnsNearestMarker(t *testing.T) {
	outer := markDir(t, tempRoot(t))
	inner := markDir(t, mkdir(t, filepath.Join(outer, "a", "b")))
	start := mkdir(t, filepath.Join(inner, "c", "d"))

	got, err := workspace.Find(start)
	if err != nil {
		t.Fatalf("Find(%q) returned error: %v", start, err)
	}
	if got != inner {
		t.Errorf("Find(%q) = %q, want the nearest marker %q (outer was %q)", start, got, inner, outer)
	}
}

func TestNoMarkerAnywhere(t *testing.T) {
	start := mkdir(t, filepath.Join(tempRoot(t), "a", "b"))

	got, err := workspace.Find(start)
	if !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("Find(%q) error = %v, want ErrNotFound", start, err)
	}
	if got != "" {
		t.Errorf("Find(%q) = %q, want empty path alongside ErrNotFound", start, got)
	}
}

func TestMarkerIsAFile(t *testing.T) {
	root := tempRoot(t)
	writeFile(t, filepath.Join(root, ".scopr"))
	start := mkdir(t, filepath.Join(root, "child"))

	got, err := workspace.Find(start)
	if err == nil {
		t.Fatalf("Find(%q) = %q, want an error", start, got)
	}
	if errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("Find(%q) returned ErrNotFound; a malformed marker must not read as an absent one: %v", start, err)
	}
}

func TestStartDirDoesNotExist(t *testing.T) {
	start := filepath.Join(tempRoot(t), "nope")

	_, err := workspace.Find(start)
	if err == nil {
		t.Fatalf("Find(%q) returned no error", start)
	}
	if errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("Find(%q) returned ErrNotFound; a bad start path is not an absent workspace: %v", start, err)
	}
}

func TestStartDirIsAFile(t *testing.T) {
	root := markDir(t, tempRoot(t))
	file := writeFile(t, filepath.Join(root, "file.txt"))

	_, err := workspace.Find(file)
	if err == nil {
		t.Fatal("Find on a regular file returned no error")
	}
	if errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("Find(%q) returned ErrNotFound, want a distinct error: %v", file, err)
	}
}

func TestEmptyStartDir(t *testing.T) {
	if _, err := workspace.Find(""); err == nil {
		t.Fatal(`Find("") returned no error`)
	}
}

func TestResolvesSymlinkedStartDir(t *testing.T) {
	root := tempRoot(t)
	real := markDir(t, mkdir(t, filepath.Join(root, "real")))
	sub := mkdir(t, filepath.Join(real, "sub"))

	link := filepath.Join(root, "link")
	if err := os.Symlink(sub, link); err != nil {
		t.Fatalf("symlink %s -> %s: %v", link, sub, err)
	}

	got, err := workspace.Find(link)
	if err != nil {
		t.Fatalf("Find(%q) returned error: %v", link, err)
	}
	if got != real {
		t.Errorf("Find(%q) = %q, want the real root %q", link, got, real)
	}
}

func TestReturnsAbsolutePath(t *testing.T) {
	root := markDir(t, tempRoot(t))
	child := mkdir(t, filepath.Join(root, "child"))

	t.Chdir(child)

	got, err := workspace.Find(".")
	if err != nil {
		t.Fatalf(`Find(".") returned error: %v`, err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf(`Find(".") = %q, want an absolute path`, got)
	}
	if got != root {
		t.Errorf(`Find(".") = %q, want %q`, got, root)
	}
}

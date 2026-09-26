package repo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const maxDepth = 4

var (
	ErrNoSuchRepo = errors.New("no such repository")
	ErrAmbiguous  = errors.New("ambiguous repository name")
)

type Repo struct {
	Name string
	Path string
}

type AmbiguousError struct {
	Name    string
	Matches []Repo
}

func (e *AmbiguousError) Error() string {
	var b strings.Builder

	fmt.Fprintf(&b, "ambiguous repository %q; found %d matches:", e.Name, len(e.Matches))
	for _, r := range e.Matches {
		fmt.Fprintf(&b, "\n  %s", r.Path)
	}
	b.WriteString("\nuse the relative path to disambiguate")

	return b.String()
}

func (e *AmbiguousError) Unwrap() error { return ErrAmbiguous }

// An unreadable directory fails the call rather than silently narrowing the scope.
// Registered workspace roots under root are walked through but never listed,
// and the depth budget restarts at each of them.
func List(root string, workspaces []string) ([]Repo, error) {
	stops := within(root, workspaces)
	transit := make(map[string]bool)

	var repos []Repo

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("scan %q: %w", path, err)
		}

		if path == root {
			return nil
		}

		if !d.IsDir() {
			return nil
		}

		if strings.HasPrefix(d.Name(), ".") {
			return fs.SkipDir
		}

		if slices.Contains(stops, path) {
			return nil
		}

		leads := leadsTo(path, stops)
		if transit[filepath.Dir(path)] {
			if !leads {
				return fs.SkipDir
			}
			transit[path] = true
			return nil
		}

		depth, err := depthFrom(nearest(root, stops, path), path)
		if err != nil {
			return err
		}

		repos = append(repos, Repo{Name: d.Name(), Path: path})

		isRepo, err := hasGitDir(path)
		if err != nil {
			return err
		}
		if isRepo || depth >= maxDepth {
			if leads {
				transit[path] = true
				return nil
			}
			return fs.SkipDir
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.SortFunc(repos, func(a, b Repo) int {
		return strings.Compare(a.Path, b.Path)
	})

	return repos, nil
}

func Resolve(root string, workspaces []string, name string) (string, error) {
	repos, err := List(root, workspaces)
	if err != nil {
		return "", err
	}
	return ResolveIn(root, repos, name)
}

func ResolveIn(root string, repos []Repo, name string) (string, error) {
	matches, err := match(root, repos, name)
	if err != nil {
		return "", err
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("%w: %q", ErrNoSuchRepo, name)
	case 1:
		return matches[0].Path, nil
	default:
		return "", &AmbiguousError{Name: name, Matches: matches}
	}
}

func match(root string, repos []Repo, name string) ([]Repo, error) {
	byPath := strings.ContainsRune(name, filepath.Separator)
	want := name
	if byPath {
		want = filepath.Clean(name)
	}

	var matches []Repo
	for _, r := range repos {
		got := r.Name

		if byPath {
			rel, err := filepath.Rel(root, r.Path)
			if err != nil {
				return nil, fmt.Errorf("relative path for %q: %w", r.Path, err)
			}
			got = rel
		}

		if got == want {
			matches = append(matches, r)
		}
	}

	return matches, nil
}

func within(root string, workspaces []string) []string {
	var out []string
	for _, w := range workspaces {
		if under(w, root) {
			out = append(out, w)
		}
	}
	return out
}

func leadsTo(dir string, stops []string) bool {
	return slices.ContainsFunc(stops, func(s string) bool { return under(s, dir) })
}

func nearest(root string, stops []string, path string) string {
	base := root
	for _, s := range stops {
		if under(path, s) && len(s) > len(base) {
			base = s
		}
	}
	return base
}

func under(path, root string) bool {
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

func depthFrom(root, path string) (int, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return 0, fmt.Errorf("relative path for %q: %w", path, err)
	}
	return len(strings.Split(rel, string(filepath.Separator))), nil
}

func hasGitDir(dir string) (bool, error) {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("inspect %q: %w", filepath.Join(dir, ".git"), err)
}

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

// AmbiguousError carries the candidates so callers need not parse the message.
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

// List returns every directory under root, sorted by path.
// An unreadable directory fails the call. Skipping it would silently narrow
// the scope.
func List(root string) ([]Repo, error) {
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

		depth, err := depthFrom(root, path)
		if err != nil {
			return err
		}

		repos = append(repos, Repo{Name: d.Name(), Path: path})

		isRepo, err := hasGitDir(path)
		if err != nil {
			return err
		}
		if isRepo || depth >= maxDepth {
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

// Resolve scans root on every call. To resolve several names, List once and
// use ResolveIn.
func Resolve(root, name string) (string, error) {
	repos, err := List(root)
	if err != nil {
		return "", err
	}
	return ResolveIn(root, repos, name)
}

// ResolveIn maps a name to an absolute path against an existing listing. A
// name containing a separator matches the path relative to root.
//
// Two matches is always an error.
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

// depthFrom counts separators below root. Root is depth 0.
func depthFrom(root, path string) (int, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return 0, fmt.Errorf("relative path for %q: %w", path, err)
	}
	return len(strings.Split(rel, string(filepath.Separator))), nil
}

// hasGitDir reports whether dir is a repository root.
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

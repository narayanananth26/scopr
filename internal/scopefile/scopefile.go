package scopefile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const dir = ".scopr/scopes"

var (
	ErrNoSuchScope = errors.New("no such scope")
	ErrScopeExists = errors.New("scope already exists")
	ErrInvalidName = errors.New("invalid scope name")
	ErrEmptyScope  = errors.New("scope names no repositories")
	ErrAmbiguous   = errors.New("ambiguous scope name")
)

// A name becomes a filename, so this is all that stands between it and an
// arbitrary write.
func ValidName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("%w: empty", ErrInvalidName)
	case strings.ContainsRune(name, filepath.Separator), strings.ContainsRune(name, '/'):
		return fmt.Errorf("%w: %q contains a path separator", ErrInvalidName, name)
	case name == "." || name == "..":
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	case strings.HasPrefix(name, "."):
		return fmt.Errorf("%w: %q starts with a dot", ErrInvalidName, name)
	}
	return nil
}

type AmbiguousError struct {
	Query   string
	Matches []string
}

func (e *AmbiguousError) Error() string {
	var b strings.Builder

	fmt.Fprintf(&b, "ambiguous scope %q; found %d matches:", e.Query, len(e.Matches))
	for _, m := range e.Matches {
		fmt.Fprintf(&b, "\n  %s", m)
	}
	b.WriteString("\nuse the full name to disambiguate")

	return b.String()
}

func (e *AmbiguousError) Unwrap() error { return ErrAmbiguous }

func Lookup(root, query string) ([]string, error) {
	if query == "" {
		return nil, fmt.Errorf("%w: empty", ErrNoSuchScope)
	}

	names, err := List(root)
	if err != nil {
		return nil, err
	}

	if slices.Contains(names, query) {
		return []string{query}, nil
	}

	var matches []string
	for _, n := range names {
		if strings.HasPrefix(n, query) {
			matches = append(matches, n)
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrNoSuchScope, query)
	}
	return matches, nil
}

func One(root, query string) (string, error) {
	matches, err := Lookup(root, query)
	if err != nil {
		return "", err
	}
	if len(matches) > 1 {
		return "", &AmbiguousError{Query: query, Matches: matches}
	}
	return matches[0], nil
}

func Path(root, name string) string {
	return filepath.Join(root, dir, name)
}

func Load(root, name string) ([]string, error) {
	if err := ValidName(name); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(Path(root, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %q", ErrNoSuchScope, name)
	}
	if err != nil {
		return nil, fmt.Errorf("read scope %q: %w", name, err)
	}

	var repos []string
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		repos = append(repos, line)
	}

	if len(repos) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrEmptyScope, name)
	}
	return repos, nil
}

func Save(root, name string, repos []string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	if len(repos) == 0 {
		return fmt.Errorf("%w: %q", ErrEmptyScope, name)
	}

	path := Path(root, name)

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%w: %q", ErrScopeExists, name)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat scope %q: %w", name, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create scopes directory: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+name+".*")
	if err != nil {
		return fmt.Errorf("create temp file for scope %q: %w", name, err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(strings.Join(repos, "\n") + "\n"); err != nil {
		tmp.Close()
		return fmt.Errorf("write scope %q: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close scope %q: %w", name, err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("install scope %q: %w", name, err)
	}
	return nil
}

func List(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read scopes directory: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		names = append(names, e.Name())
	}

	slices.Sort(names)
	return names, nil
}

func Rename(root, from, to string) error {
	if err := ValidName(from); err != nil {
		return err
	}
	if err := ValidName(to); err != nil {
		return err
	}

	src, dst := Path(root, from), Path(root, to)

	if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %q", ErrNoSuchScope, from)
	} else if err != nil {
		return fmt.Errorf("stat scope %q: %w", from, err)
	}

	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("%w: %q", ErrScopeExists, to)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat scope %q: %w", to, err)
	}

	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("rename scope %q to %q: %w", from, to, err)
	}
	return nil
}

func Delete(root, name string) error {
	if err := ValidName(name); err != nil {
		return err
	}

	err := os.Remove(Path(root, name))
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %q", ErrNoSuchScope, name)
	}
	if err != nil {
		return fmt.Errorf("delete scope %q: %w", name, err)
	}
	return nil
}

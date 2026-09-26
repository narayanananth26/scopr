package scope

import (
	"errors"
	"fmt"
	"path/filepath"

	"scopr/internal/registry"
	"scopr/internal/repo"
)

var (
	ErrEmpty     = errors.New("no repositories named")
	ErrDuplicate = errors.New("repository named twice")
)

type Scope struct {
	Root  string
	Repos []repo.Repo
}

func (s Scope) Primary() repo.Repo { return s.Repos[0] }

func (s Scope) Others() []repo.Repo { return s.Repos[1:] }

func (s Scope) Paths() ([]string, error) {
	out := make([]string, 0, len(s.Repos))
	for _, r := range s.Repos {
		rel, err := filepath.Rel(s.Root, r.Path)
		if err != nil {
			return nil, fmt.Errorf("relative path for %q: %w", r.Path, err)
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out, nil
}

type named struct {
	name string
	from string
}

func (n named) attribute(err error) error {
	if n.from == "" {
		return err
	}
	return fmt.Errorf("%s%s: %w", Prefix, n.from, err)
}

func Resolve(root string, names []string) (Scope, error) {
	ns := make([]named, len(names))
	for i, name := range names {
		ns[i] = named{name: name}
	}
	return resolveNamed(root, ns)
}

func resolveNamed(root string, names []named) (Scope, error) {
	if len(names) == 0 {
		return Scope{}, ErrEmpty
	}

	workspaces, err := registry.LivePaths()
	if err != nil {
		return Scope{}, err
	}

	repos, err := repo.List(root, workspaces)
	if err != nil {
		return Scope{}, err
	}

	byPath := make(map[string]repo.Repo, len(repos))
	byRel := make(map[string]string, len(repos))
	for _, r := range repos {
		byPath[r.Path] = r
		if rel, err := filepath.Rel(root, r.Path); err == nil {
			byRel[filepath.ToSlash(rel)] = r.Path
		}
	}

	var (
		resolved []repo.Repo
		problems []error
		seen     = make(map[string]named, len(names))
	)

	for _, n := range names {
		path, exact := byRel[n.name]
		if n.from == "" || !exact {
			path, err = repo.ResolveIn(root, repos, n.name)
			if err != nil {
				problems = append(problems, n.attribute(err))
				continue
			}
		}

		if first, dup := seen[path]; dup {
			problems = append(problems, duplicateError(first, n, path))
			continue
		}
		seen[path] = n

		resolved = append(resolved, byPath[path])
	}

	if len(problems) > 0 {
		return Scope{}, errors.Join(problems...)
	}

	return Scope{Root: root, Repos: resolved}, nil
}

func duplicateError(first, second named, path string) error {
	switch {
	case first.from != "" && second.from != "" && first.from == second.from:
		return fmt.Errorf("%w: %s%s names %q twice", ErrDuplicate, Prefix, first.from, second.name)

	case first.from != "" && second.from != "":
		return fmt.Errorf("%w: %s%s and %s%s both name %q", ErrDuplicate, Prefix, first.from, Prefix, second.from, second.name)

	case first.from != "":
		return fmt.Errorf("%w: %q is already in %s%s", ErrDuplicate, second.name, Prefix, first.from)

	case second.from != "":
		return fmt.Errorf("%w: %s%s names %q, which was already given", ErrDuplicate, Prefix, second.from, second.name)

	case first.name == second.name:
		return fmt.Errorf("%w: %q", ErrDuplicate, second.name)

	default:
		return fmt.Errorf("%w: %q and %q are both %s", ErrDuplicate, first.name, second.name, path)
	}
}

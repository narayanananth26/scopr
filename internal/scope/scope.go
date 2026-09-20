package scope

import (
	"errors"
	"fmt"

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

	repos, err := repo.List(root)
	if err != nil {
		return Scope{}, err
	}

	byPath := make(map[string]repo.Repo, len(repos))
	for _, r := range repos {
		byPath[r.Path] = r
	}

	var (
		resolved []repo.Repo
		problems []error
		seen     = make(map[string]named, len(names))
	)

	for _, n := range names {
		path, err := repo.ResolveIn(root, repos, n.name)
		if err != nil {
			problems = append(problems, n.attribute(err))
			continue
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

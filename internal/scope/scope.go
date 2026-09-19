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

// Scope is an ordered set of repositories. Repos[0] is the primary and becomes
// the session's working directory.
type Scope struct {
	Root  string
	Repos []repo.Repo
}

func (s Scope) Primary() repo.Repo { return s.Repos[0] }

func (s Scope) Others() []repo.Repo { return s.Repos[1:] }

// named is a repository name plus the scope it was expanded from, empty when
// it was typed directly.
type named struct {
	name string
	from string
}

// attribute prefixes err with the scope a name came from.
func (n named) attribute(err error) error {
	if n.from == "" {
		return err
	}
	return fmt.Errorf("%s%s: %w", Prefix, n.from, err)
}

// Resolve maps names to repositories under root, keeping the given order.
// Duplicates are matched by resolved path, so two spellings of one repo
// collide. Reports every bad name at once; returns no scope on any failure.
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
		seen     = make(map[string]string, len(names))
	)

	for _, n := range names {
		path, err := repo.ResolveIn(root, repos, n.name)
		if err != nil {
			problems = append(problems, n.attribute(err))
			continue
		}

		if first, dup := seen[path]; dup {
			problems = append(problems, n.attribute(fmt.Errorf("%w: %q and %q are both %s", ErrDuplicate, first, n.name, path)))
			continue
		}
		seen[path] = n.name

		resolved = append(resolved, byPath[path])
	}

	if len(problems) > 0 {
		return Scope{}, errors.Join(problems...)
	}

	return Scope{Root: root, Repos: resolved}, nil
}

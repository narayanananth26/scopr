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

// Resolve maps names to repositories under root, keeping the given order.
// Duplicates are matched by resolved path, so two spellings of one repo
// collide. Reports every bad name at once; returns no scope on any failure.
func Resolve(root string, names []string) (Scope, error) {
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

	for _, name := range names {
		path, err := repo.ResolveIn(root, repos, name)
		if err != nil {
			problems = append(problems, err)
			continue
		}

		if first, dup := seen[path]; dup {
			problems = append(problems, fmt.Errorf("%w: %q and %q are both %s", ErrDuplicate, first, name, path))
			continue
		}
		seen[path] = name

		resolved = append(resolved, byPath[path])
	}

	if len(problems) > 0 {
		return Scope{}, errors.Join(problems...)
	}

	return Scope{Root: root, Repos: resolved}, nil
}

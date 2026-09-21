package resolve

import (
	"errors"
	"fmt"
	"strings"

	"scopr/internal/registry"
	"scopr/internal/scopefile"
	"scopr/internal/workspace"
)

var (
	ErrNoWorkspace = errors.New("not inside a workspace")

	ErrAmbiguous = errors.New("more than one match")
)

type Options struct {
	Workspace string

	Cwd string
}

type Hit struct {
	Workspace registry.Workspace
	Scope     string
}

func One(opt Options) (string, error) {
	if opt.Workspace != "" {
		matches, err := registry.Lookup(opt.Workspace)
		if err != nil {
			return "", err
		}
		if len(matches) > 1 {
			return "", ambiguous(opt.Workspace, matches)
		}
		return matches[0].Path, nil
	}

	root, err := workspace.Find(opt.Cwd)
	if err == nil {
		return root, nil
	}
	if !errors.Is(err, workspace.ErrNotFound) {
		return "", err
	}

	live, err := registry.Live()
	if err != nil {
		return "", err
	}
	if len(live) == 1 {
		return live[0].Path, nil
	}

	if len(live) == 0 {
		return "", fmt.Errorf("%w, and none are registered; add one with: scopr workspace add", ErrNoWorkspace)
	}
	return "", fmt.Errorf("%w; name one with --workspace, or run from inside it", ErrNoWorkspace)
}

func Scope(opt Options, name string) ([]Hit, error) {
	name = strings.TrimPrefix(name, "@")

	if opt.Workspace != "" {
		matches, err := registry.Lookup(opt.Workspace)
		if err != nil {
			return nil, err
		}
		if len(matches) > 1 {
			return nil, ambiguous(opt.Workspace, matches)
		}
		resolved, err := scopefile.One(matches[0].Path, name)
		if err != nil {
			return nil, fmt.Errorf("%w in %s", err, matches[0].Name)
		}
		return []Hit{{Workspace: matches[0], Scope: resolved}}, nil
	}

	if root, err := workspace.Find(opt.Cwd); err == nil {
		resolved, err := scopefile.One(root, name)
		if err == nil {
			return []Hit{{Workspace: registry.Workspace{Path: root, Name: root}, Scope: resolved}}, nil
		}
		if errors.Is(err, scopefile.ErrAmbiguous) {
			return nil, err
		}
	}

	live, err := registry.Live()
	if err != nil {
		return nil, err
	}

	var hits []Hit
	for _, w := range live {
		if resolved, ok := holds(w.Path, name); ok {
			hits = append(hits, Hit{Workspace: w, Scope: resolved})
		}
	}

	if len(hits) == 0 {
		return nil, fmt.Errorf("%w: %q", scopefile.ErrNoSuchScope, name)
	}
	return hits, nil
}

func All() ([]Hit, error) {
	live, err := registry.Live()
	if err != nil {
		return nil, err
	}

	var hits []Hit
	for _, w := range live {
		names, err := scopefile.List(w.Path)
		if err != nil {
			continue
		}
		for _, n := range names {
			hits = append(hits, Hit{Workspace: w, Scope: n})
		}
	}
	return hits, nil
}

func holds(root, name string) (string, bool) {
	resolved, err := scopefile.One(root, name)
	return resolved, err == nil
}

func ambiguous(query string, matches []registry.Workspace) error {
	var b strings.Builder

	fmt.Fprintf(&b, "%v: %q matches", ErrAmbiguous, query)
	for _, w := range matches {
		fmt.Fprintf(&b, "\n  %s", w.Path)
	}
	return fmt.Errorf("%w%s", ErrAmbiguous, strings.TrimPrefix(b.String(), ErrAmbiguous.Error()))
}

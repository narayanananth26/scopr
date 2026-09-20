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
	// ErrNoWorkspace reports that nothing said which workspace to act on.
	ErrNoWorkspace = errors.New("not inside a workspace")

	// ErrAmbiguous reports more than one candidate. Guessing would act on the
	// wrong workspace, which is the failure this tool exists to prevent.
	ErrAmbiguous = errors.New("more than one match")
)

// Options is what the caller knows before resolving.
type Options struct {
	// Workspace is the --workspace value, empty when not given.
	Workspace string

	// Cwd is where the command was run.
	Cwd string
}

// Hit is a scope found in a workspace.
type Hit struct {
	Workspace registry.Workspace
	Scope     string
}

// One returns the workspace a command should act on, for the commands that
// need exactly one: saving, renaming, surveying.
//
// Order is --workspace, then the workspace the command was run in, then the
// only registered one if there is only one. Anything less certain is an error
// rather than a guess.
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

// Scope finds which workspaces hold a named scope.
//
// The workspace the command was run in wins outright when it has the scope, so
// standing somewhere is a stronger signal than being registered. Otherwise
// every registered workspace is searched, and every hit is returned for the
// caller to choose between.
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
		if !holds(matches[0].Path, name) {
			return nil, fmt.Errorf("%w: %q in %s", scopefile.ErrNoSuchScope, name, matches[0].Name)
		}
		return []Hit{{Workspace: matches[0], Scope: name}}, nil
	}

	if root, err := workspace.Find(opt.Cwd); err == nil && holds(root, name) {
		return []Hit{{Workspace: registry.Workspace{Path: root, Name: root}, Scope: name}}, nil
	}

	live, err := registry.Live()
	if err != nil {
		return nil, err
	}

	var hits []Hit
	for _, w := range live {
		if holds(w.Path, name) {
			hits = append(hits, Hit{Workspace: w, Scope: name})
		}
	}

	if len(hits) == 0 {
		return nil, fmt.Errorf("%w: %q", scopefile.ErrNoSuchScope, name)
	}
	return hits, nil
}

// All returns every scope in every registered workspace, for offering a
// choice rather than resolving a name.
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

func holds(root, name string) bool {
	_, err := scopefile.Load(root, name)
	return err == nil
}

func ambiguous(query string, matches []registry.Workspace) error {
	var b strings.Builder

	fmt.Fprintf(&b, "%v: %q matches", ErrAmbiguous, query)
	for _, w := range matches {
		fmt.Fprintf(&b, "\n  %s", w.Path)
	}
	return fmt.Errorf("%w%s", ErrAmbiguous, strings.TrimPrefix(b.String(), ErrAmbiguous.Error()))
}

package picker

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"scopr/internal/repo"
	"scopr/internal/scope"
	"scopr/internal/scopefile"
	"scopr/internal/ui"
)

// ErrCancelled reports that nothing was chosen.
var ErrCancelled = ui.ErrCancelled

// Editor shows a scope and returns the accepted one. Injected so the logic
// around it is testable without a terminal.
type Editor func(chosen, available []string) ([]string, error)

// Pick opens the editor with nothing chosen.
func Pick(root string) ([]string, error) {
	return pick(root, nil, ui.Run)
}

// Edit opens the editor on an existing scope, for trimming, reordering or
// adding to a set of suggestions.
func Edit(root string, chosen []string) ([]string, error) {
	return pick(root, chosen, ui.Run)
}

func pick(root string, chosen []string, edit Editor) ([]string, error) {
	available, err := names(root)
	if err != nil {
		return nil, err
	}
	if len(available) == 0 {
		return nil, errors.New("no repositories in this workspace")
	}

	// A scope name is not a repository, so it is expanded before editing
	// rather than offered as an entry.
	expanded, err := expand(root, chosen)
	if err != nil {
		return nil, err
	}

	return edit(expanded, available)
}

// names is every repository in the workspace, as it must be typed back.
func names(root string) ([]string, error) {
	repos, err := repo.List(root)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(repos))
	for _, r := range repos {
		rel, err := filepath.Rel(root, r.Path)
		if err != nil {
			continue
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out, nil
}

// expand replaces any @name with the repositories it holds.
func expand(root string, chosen []string) ([]string, error) {
	var out []string

	for _, c := range chosen {
		name, ok := strings.CutPrefix(c, scope.Prefix)
		if !ok {
			out = append(out, c)
			continue
		}

		members, err := scopefile.Load(root, name)
		if err != nil {
			return nil, err
		}
		out = append(out, members...)
	}
	return out, nil
}

// Available lists what could have been picked, for when the editor cannot run.
func Available(root string, w *os.File) {
	if scopes, err := scopefile.List(root); err == nil {
		for _, name := range scopes {
			members, err := scopefile.Load(root, name)
			if err != nil {
				continue
			}
			_, _ = w.WriteString("  " + scope.Prefix + name + "  " + strings.Join(members, " ") + "\n")
		}
	}

	if repos, err := names(root); err == nil {
		for _, name := range repos {
			_, _ = w.WriteString("  " + name + "\n")
		}
	}
}

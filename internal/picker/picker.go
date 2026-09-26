package picker

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"scopr/internal/registry"
	"scopr/internal/repo"
	"scopr/internal/scope"
	"scopr/internal/scopefile"
	"scopr/internal/ui"
)

var ErrCancelled = ui.ErrCancelled

type Editor func(ui.Model) ([]string, error)

func Pick(root string) ([]string, error) {
	return pick(root, Scope{}, ui.Run)
}

type Scope struct {
	Names []string

	Notes map[string]string

	Header string
}

func Edit(root string, s Scope) ([]string, error) {
	return pick(root, s, ui.Run)
}

func pick(root string, s Scope, edit Editor) ([]string, error) {
	available, err := Names(root)
	if err != nil {
		return nil, err
	}
	if len(available) == 0 {
		return nil, errors.New("no repositories in this workspace")
	}

	expanded, err := expand(root, s.Names)
	if err != nil {
		return nil, err
	}

	m := ui.New(expanded, available)
	m.Notes = s.Notes
	m.Header = s.Header

	return edit(m)
}

func Names(root string) ([]string, error) {
	workspaces, err := registry.LivePaths()
	if err != nil {
		return nil, err
	}

	repos, err := repo.List(root, workspaces)
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

	if repos, err := Names(root); err == nil {
		for _, name := range repos {
			_, _ = w.WriteString("  " + name + "\n")
		}
	}
}

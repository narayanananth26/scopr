package picker

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"scopr/internal/repo"
	"scopr/internal/scope"
	"scopr/internal/scopefile"
)

var (
	// ErrUnavailable reports that fzf is not installed. Callers fall back to
	// printing what could have been picked.
	ErrUnavailable = errors.New("fzf not found on PATH")

	// ErrCancelled reports that nothing was chosen. Starting nothing is the
	// correct outcome, not a failure.
	ErrCancelled = errors.New("selection cancelled")
)

// Chooser presents items and returns those selected. Injected so the
// selection logic is testable without a terminal.
type Chooser func(prompt string, items []string, multi bool) ([]string, error)

// Pick asks for a scope or a primary repository, then for any additional
// repositories, and returns arguments in the form ResolveArgs accepts.
//
// Choosing the primary separately is what makes the order meaningful: fzf
// returns multi-selections in list order, not selection order, so a single
// multi-select could not say which repository becomes the working directory.
func Pick(root string) ([]string, error) {
	return pick(root, fzf)
}

func pick(root string, choose Chooser) ([]string, error) {
	scopes, err := scopefile.List(root)
	if err != nil {
		return nil, err
	}

	repos, err := repo.List(root)
	if err != nil {
		return nil, err
	}

	first, err := chooseFirst(root, choose, scopes, repos)
	if err != nil {
		return nil, err
	}

	// A saved scope carries its own order; nothing more to ask.
	if strings.HasPrefix(first, scope.Prefix) {
		return []string{first}, nil
	}

	rest, err := chooseRest(root, choose, repos, first)
	if err != nil {
		return nil, err
	}

	return append([]string{first}, rest...), nil
}

func chooseFirst(root string, choose Chooser, scopes []string, repos []repo.Repo) (string, error) {
	items := make([]string, 0, len(scopes)+len(repos))

	for _, name := range scopes {
		members, err := scopefile.Load(root, name)
		if err != nil {
			items = append(items, scope.Prefix+name)
			continue
		}
		items = append(items, fmt.Sprintf("%s%s\t%s", scope.Prefix, name, strings.Join(members, " ")))
	}

	for _, r := range repos {
		items = append(items, relative(root, r.Path))
	}

	if len(items) == 0 {
		return "", errors.New("no scopes or repositories in this workspace")
	}

	picked, err := choose("scope or primary repo> ", items, false)
	if err != nil {
		return "", err
	}
	if len(picked) == 0 {
		return "", ErrCancelled
	}

	return field(picked[0]), nil
}

func chooseRest(root string, choose Chooser, repos []repo.Repo, exclude string) ([]string, error) {
	var items []string
	for _, r := range repos {
		if rel := relative(root, r.Path); rel != exclude {
			items = append(items, rel)
		}
	}
	if len(items) == 0 {
		return nil, nil
	}

	picked, err := choose("also in scope (optional)> ", items, true)
	// Choosing nothing here is a single-repo scope, not a cancellation.
	if errors.Is(err, ErrCancelled) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(picked))
	for _, p := range picked {
		out = append(out, field(p))
	}
	return out, nil
}

// relative renders a repo path the way it must be typed back.
func relative(root, path string) string {
	rel := strings.TrimPrefix(strings.TrimPrefix(path, root), string(os.PathSeparator))
	if rel == "" {
		return path
	}
	return rel
}

// field returns the selectable token, dropping any tab-separated description.
func field(line string) string {
	name, _, _ := strings.Cut(line, "\t")
	return strings.TrimSpace(name)
}

// fzf runs the real picker. It draws on the terminal and writes the selection
// to stdout, so only stdout is captured.
func fzf(prompt string, items []string, multi bool) ([]string, error) {
	bin, err := exec.LookPath("fzf")
	if err != nil {
		return nil, ErrUnavailable
	}

	args := []string{
		"--prompt", prompt,
		"--height", "40%",
		"--layout", "reverse",
		"--delimiter", "\t",
		"--with-nth", "1,2",
	}
	if multi {
		args = append(args, "--multi", "--header", "tab to select, enter to confirm, esc for none")
	}

	cmd := exec.Command(bin, args...)
	cmd.Stdin = strings.NewReader(strings.Join(items, "\n"))
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// 1 is no match, 130 is interrupted. Both mean nothing was chosen.
		if code := exitErr.ExitCode(); code == 1 || code == 130 {
			return nil, ErrCancelled
		}
		return nil, fmt.Errorf("fzf: %w", err)
	}
	if err != nil {
		return nil, fmt.Errorf("run fzf: %w", err)
	}

	var picked []string
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		if line != "" {
			picked = append(picked, line)
		}
	}
	if len(picked) == 0 {
		return nil, ErrCancelled
	}
	return picked, nil
}

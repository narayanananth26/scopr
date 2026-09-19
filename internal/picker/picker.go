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

// Prompt is one question put to the person.
type Prompt struct {
	Label string
	Items []string

	// Multi allows more than one answer.
	Multi bool

	// Preselect marks every item to begin with, so answering means unmarking
	// what should go rather than marking what should stay.
	Preselect bool
}

// Chooser presents a Prompt and returns what was selected. Injected so the
// selection logic is testable without a terminal.
type Chooser func(Prompt) ([]string, error)

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

// Trim shows the given names all marked and returns those kept, in order.
// Unmarking everything is a cancellation.
//
// It can only remove. Adding a repository the survey missed, or changing which
// one is primary, means declining and naming them instead.
func Trim(names []string) ([]string, error) {
	return trim(names, fzf)
}

func trim(names []string, choose Chooser) ([]string, error) {
	if len(names) == 0 {
		return nil, ErrCancelled
	}

	picked, err := choose(Prompt{
		Label:     "keep (shift-tab to drop)> ",
		Items:     names,
		Multi:     true,
		Preselect: true,
	})
	if err != nil {
		return nil, err
	}
	if len(picked) == 0 {
		return nil, ErrCancelled
	}

	kept := make([]string, 0, len(picked))
	for _, p := range picked {
		kept = append(kept, field(p))
	}
	return kept, nil
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

	picked, err := choose(Prompt{Label: "scope or primary repo> ", Items: items})
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

	picked, err := choose(Prompt{Label: "also in scope (tab to mark)> ", Items: items, Multi: true})
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

// fzfArgs translates a Prompt into fzf flags.
func fzfArgs(p Prompt) []string {
	args := []string{
		"--prompt", p.Label,
		"--height", "40%",
		"--layout", "reverse",
		"--delimiter", "\t",
		"--with-nth", "1,2",
		"--header-first",
	}
	if p.Multi {
		args = append(args, "--multi", "--header", "tab to mark, shift-tab to unmark, enter to confirm, esc for none")
	}
	if p.Preselect {
		args = append(args, "--bind", "start:select-all")
	}
	return args
}

// fzf runs the real picker. It draws on the terminal and writes the selection
// to stdout, so only stdout is captured.
func fzf(p Prompt) ([]string, error) {
	bin, err := exec.LookPath("fzf")
	if err != nil {
		return nil, ErrUnavailable
	}

	cmd := exec.Command(bin, fzfArgs(p)...)
	cmd.Stdin = strings.NewReader(strings.Join(p.Items, "\n"))
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

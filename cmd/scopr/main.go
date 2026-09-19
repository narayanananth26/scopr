package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"scopr/internal/dispatch"
	"scopr/internal/launch"
	"scopr/internal/picker"
	"scopr/internal/repo"
	"scopr/internal/scope"
	"scopr/internal/scopefile"
	"scopr/internal/workspace"
)

// findRoot locates the workspace, reporting a missing one as advice rather
// than as a stat error.
func findRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("couldn't resolve current working directory: %w", err)
	}

	root, err := workspace.Find(cwd)
	if errors.Is(err, workspace.ErrNotFound) {
		return "", errors.New("not inside a .scopr workspace; run this command from within a workspace")
	}
	if err != nil {
		return "", err
	}
	return root, nil
}

// runWhere prints the workspace root. The path goes to stdout alone.
func runWhere() int {
	root, err := findRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fmt.Fprintln(os.Stdout, root)
	return 0
}

func runList() int {
	root, err := findRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	names, err := scopefile.List(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(names) == 0 {
		fmt.Fprintln(os.Stderr, "no saved scopes; create one with --save")
		return 0
	}

	for _, name := range names {
		repos, err := scopefile.Load(root, name)
		if err != nil {
			fmt.Fprintf(os.Stdout, "%s%s\n", scope.Prefix, name)
			continue
		}
		fmt.Fprintf(os.Stdout, "%s%-16s %s\n", scope.Prefix, name, strings.Join(repos, " "))
	}
	return 0
}

// runSave stores a scope after checking every repository resolves, so a saved
// scope is one that can actually launch.
func runSave(name string, args []string) int {
	root, err := findRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if _, err := scope.ResolveArgs(root, args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if err := scopefile.Save(root, name, args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "saved %s%s: %s\n", scope.Prefix, name, strings.Join(args, " "))
	return 0
}

func runDelete(name string) int {
	root, err := findRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	name = strings.TrimPrefix(name, scope.Prefix)

	if err := scopefile.Delete(root, name); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "deleted %s%s\n", scope.Prefix, name)
	return 0
}

func runRename(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: scopr --rename <old> <new>")
		return 1
	}

	root, err := findRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	from := strings.TrimPrefix(args[0], scope.Prefix)
	to := strings.TrimPrefix(args[1], scope.Prefix)

	if err := scopefile.Rename(root, from, to); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "renamed %s%s to %s%s\n", scope.Prefix, from, scope.Prefix, to)
	return 0
}

// choose runs the picker, falling back to a printed listing when fzf is
// missing so a bare scopr still says what could have been picked.
func choose(root string) ([]string, int) {
	args, err := picker.Pick(root)

	switch {
	case err == nil:
		return args, 0

	case errors.Is(err, picker.ErrCancelled):
		return nil, 0

	case errors.Is(err, picker.ErrUnavailable):
		fmt.Fprintln(os.Stderr, "fzf is not installed, so there is nothing to pick with.")
		fmt.Fprintln(os.Stderr, "Name a repository or scope, or install fzf. Available:")
		fmt.Fprintln(os.Stderr)
		printChoices(root)
		return nil, 1

	default:
		fmt.Fprintln(os.Stderr, err)
		return nil, 1
	}
}

func printChoices(root string) {
	if names, err := scopefile.List(root); err == nil {
		for _, name := range names {
			repos, err := scopefile.Load(root, name)
			if err != nil {
				continue
			}
			fmt.Fprintf(os.Stderr, "  %s%-16s %s\n", scope.Prefix, name, strings.Join(repos, " "))
		}
	}

	repos, err := repo.List(root)
	if err != nil {
		return
	}
	for _, r := range repos {
		rel, err := filepath.Rel(root, r.Path)
		if err != nil {
			continue
		}
		fmt.Fprintf(os.Stderr, "  %s\n", rel)
	}
}

// surveyTimeout is generous: the survey is a 15-30 second call with an
// observed 21-second tail.
const surveyTimeout = 90 * time.Second

// confirm asks whether to use the suggested scope. Inference suggests; the
// person decides, because a wrongly narrow scope fails by the agent not
// finding code rather than by saying so.
func confirm() (approved, edit bool) {
	fmt.Fprint(os.Stderr, "\nstart here? [Y]es / [e]dit / [n]o: ")

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false, false
	}

	switch strings.ToLower(strings.TrimSpace(line)) {
	case "", "y", "yes":
		return true, false
	case "e", "edit":
		return false, true
	default:
		return false, false
	}
}

// runInfer surveys the workspace for a task, shows what it found, and starts a
// session once approved.
func runInfer(task, prompt string, verbose bool) int {
	root, err := findRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), surveyTimeout)
	defer cancel()

	fmt.Fprintln(os.Stderr, "surveying the workspace...")

	cfg := dispatch.Config{Root: root, Task: task}
	if verbose {
		cfg.Trace = os.Stderr
	}

	suggestions, err := dispatch.Infer(ctx, cfg)

	var args []string

	switch {
	case err == nil:
		fmt.Fprintln(os.Stderr)
		for _, s := range suggestions {
			fmt.Fprintf(os.Stderr, "  %-28s %s\n", s.Name, s.Reason)
			args = append(args, s.Name)
		}

		approved, edit := confirm()
		switch {
		case approved:

		case edit:
			// Trim can only remove. Adding a missed repo, or changing which
			// is primary, means declining and naming them.
			kept, err := picker.Trim(args)
			if errors.Is(err, picker.ErrCancelled) {
				return 0
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			args = kept

		default:
			return 0
		}

	case errors.Is(err, dispatch.ErrDeclined):
		// Nothing found is not a failure; fall through to picking by hand.
		fmt.Fprintf(os.Stderr, "%v\n\npick instead:\n", err)

	default:
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if prompt == "" {
		prompt = task
	}
	return runLaunch(args, prompt)
}

// runLaunch resolves names to a scope and starts a session in the primary
// repo, returning claude's exit code. With no names it asks.
func runLaunch(args []string, prompt string) int {
	root, err := findRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if len(args) == 0 {
		picked, code := choose(root)
		if len(picked) == 0 {
			return code
		}
		args = picked
	}

	s, err := scope.ResolveArgs(root, args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	code, err := launch.Run(launch.Config{Scope: s, Prompt: prompt})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return code
}

func usage() {
	fmt.Fprint(os.Stderr, `scopr launches Claude Code scoped to chosen repositories.

usage:
  scopr                            pick a scope or repos interactively
  scopr [flags] <repo|@scope>...   start a session; the first repo becomes the working directory
  scopr --infer <task>             suggest a scope for the task, then start
  scopr --save <name> <repo>...    save a scope under that name
  scopr --rename <old> <new>       rename a saved scope
  scopr --delete <name>            delete a saved scope
  scopr --list                     list saved scopes
  scopr --where                    print the workspace root

flags:
  -p <text>                        prompt to submit on start
  --verbose                        with --infer, show the survey's tool calls

An @name argument expands to the repositories that scope names, so scopes and
plain repositories can be mixed. Flags must precede everything else.
`)
}

func main() {
	flag.Usage = usage

	where := flag.Bool("where", false, "print the workspace root and exit")
	list := flag.Bool("list", false, "list saved scopes")
	rename := flag.Bool("rename", false, "rename a saved scope")
	save := flag.String("save", "", "save the given repositories under this scope name")
	del := flag.String("delete", "", "delete the named scope")
	prompt := flag.String("p", "", "prompt to submit on start")
	infer := flag.String("infer", "", "suggest a scope for this task")
	verbose := flag.Bool("verbose", false, "show the survey's tool calls and reasoning")
	flag.Parse()

	args := flag.Args()

	switch {
	case *where:
		os.Exit(runWhere())
	case *list:
		os.Exit(runList())
	case *rename:
		os.Exit(runRename(args))
	case *del != "":
		os.Exit(runDelete(*del))
	case *infer != "":
		os.Exit(runInfer(*infer, *prompt, *verbose))
	case *save != "":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "usage: scopr --save <name> <repo>...")
			os.Exit(1)
		}
		os.Exit(runSave(strings.TrimPrefix(*save, scope.Prefix), args))
	default:
		os.Exit(runLaunch(args, *prompt))
	}
}

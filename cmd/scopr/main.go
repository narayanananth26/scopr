package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"scopr/internal/dispatch"
	"scopr/internal/files"
	"scopr/internal/launch"
	"scopr/internal/picker"
	"scopr/internal/scope"
	"scopr/internal/scopefile"
	"scopr/internal/ui"
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
		fmt.Fprintln(os.Stderr, "no saved scopes; create one with: scopr save <name> <repo>...")
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

// surveyTimeout is generous: the survey runs 15-30 seconds with an observed
// 21-second tail.
const surveyTimeout = 90 * time.Second

// runWizard walks name, scope and prompt, then starts the session. A new name
// saves the scope once it is known to resolve.
//
// This is what bare scopr does. The name step is one Enter to skip, and it
// buys a label and a prompt that a bare picker had nowhere to put.
func runWizard() int {
	root, err := findRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	saved, err := scopefile.List(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	available, err := picker.Names(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(available) == 0 {
		fmt.Fprintln(os.Stderr, "no repositories in this workspace")
		return 1
	}

	load := func(name string) ([]string, error) { return scopefile.Load(root, name) }

	wiz := ui.NewWizard(saved, available, load)
	wiz.LoadFiles = func(names []string) []files.File {
		s, err := scope.Resolve(root, names)
		if err != nil {
			return nil
		}

		paths := make([]string, 0, len(s.Repos))
		for _, r := range s.Repos {
			paths = append(paths, r.Path)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		out, err := files.List(ctx, s.Primary().Path, paths)
		if err != nil {
			return nil
		}
		return out
	}

	w, err := ui.RunWizard(wiz)
	if errors.Is(err, ui.ErrCancelled) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	repos := w.Repos()

	// Save only after the scope is known to resolve, so a saved scope is one
	// that can launch.
	if name := w.Name(); name != "" {
		if _, err := scope.Resolve(root, repos); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := scopefile.Save(root, name, repos); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "saved %s%s\n", scope.Prefix, name)
	}

	return runLaunch(repos, w.Prompt(), w.Label())
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

	var (
		suggestions []dispatch.Suggestion
		cfg         = dispatch.Config{Root: root, Task: task}
	)

	err = ui.RunSurvey(ctx, task, func(ctx context.Context, trace io.Writer) error {
		if verbose {
			cfg.Trace = trace
		}
		var err error
		suggestions, err = dispatch.Infer(ctx, cfg)
		return err
	})

	scoped := picker.Scope{Header: "suggested for: " + task}

	switch {
	case errors.Is(err, ui.ErrCancelled):
		return 0

	case err == nil:
		scoped.Notes = make(map[string]string, len(suggestions))
		for _, s := range suggestions {
			scoped.Names = append(scoped.Names, s.Name)
			scoped.Notes[s.Name] = s.Reason
		}

	case errors.Is(err, dispatch.ErrDeclined):
		// Nothing found is not a failure; open the editor empty.
		scoped.Header = "nothing suggested - " + err.Error()

	default:
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	args, err := picker.Edit(root, scoped)
	if errors.Is(err, picker.ErrCancelled) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if prompt == "" {
		prompt = task
	}
	return runLaunch(args, prompt, task)
}

// runLaunch resolves names to a scope and starts a session in the primary
// repo, returning claude's exit code.
func runLaunch(args []string, prompt, name string) int {
	root, err := findRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	s, err := scope.ResolveArgs(root, args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	code, err := launch.Run(launch.Config{Scope: s, Prompt: prompt, Name: name})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return code
}

// label names a session from its arguments: a saved scope names itself, a
// list of repositories does not.
func label(args []string) string {
	if len(args) == 1 && strings.HasPrefix(args[0], scope.Prefix) {
		return strings.TrimPrefix(args[0], scope.Prefix)
	}
	return ""
}

// verbs are reserved: a repository sharing one of these names must be given
// as a path, such as frontend/list.
var verbs = []string{"infer", "save", "list", "rename", "delete", "where", "workspace"}

func usage() {
	fmt.Fprint(os.Stderr, `scopr launches Claude Code scoped to chosen repositories.

usage:
  scopr                             name, scope and prompt, step by step
  scopr [flags] <repo|@scope>...    start a session; the first repo becomes the working directory

  scopr infer <task>                suggest a scope for the task, then start
  scopr save <name> <repo>...       save a scope under that name
  scopr list                        list saved scopes
  scopr rename <old> <new>          rename a saved scope
  scopr delete <name>               delete a saved scope
  scopr where                       print the workspace root

  scopr workspace add [path]        register a workspace
  scopr workspace list              list registered workspaces
  scopr workspace remove <name>     forget a workspace

flags, before the verb:
  --workspace <name>                which workspace to resolve against
  -p <text>                         prompt to submit on start
  --verbose                         with infer, show the survey's tool calls

An @name argument expands to the repositories that scope names, so scopes and
plain repositories can be mixed.
`)
}

// globals are the flags every verb shares. Parsing them separately is what
// lets a verb take its own flags after its arguments.
type globals struct {
	workspace string
	prompt    string
	verbose   bool
}

// parseGlobals reads the flags before the verb and returns what is left.
func parseGlobals(argv []string) (globals, []string, error) {
	fs := flag.NewFlagSet("scopr", flag.ContinueOnError)
	fs.Usage = usage

	var g globals
	fs.StringVar(&g.workspace, "workspace", "", "which workspace to resolve against")
	fs.StringVar(&g.prompt, "p", "", "prompt to submit on start")
	fs.BoolVar(&g.verbose, "verbose", false, "show the survey's tool calls")

	if err := fs.Parse(argv); err != nil {
		return g, nil, err
	}
	return g, fs.Args(), nil
}

// verbFlags parses flags appearing after a verb's arguments, so
// scopr save surfaces gl-panel -p x works.
func verbFlags(name string, args []string, g *globals) ([]string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = usage
	fs.StringVar(&g.prompt, "p", g.prompt, "prompt to submit on start")
	fs.BoolVar(&g.verbose, "verbose", g.verbose, "show the survey's tool calls")

	// Positionals first, then any flags: flag stops at the first non-flag
	// argument, so the two are separated before parsing.
	var positional, flags []string
	for i, a := range args {
		if strings.HasPrefix(a, "-") {
			flags = args[i:]
			break
		}
		positional = append(positional, a)
	}

	if err := fs.Parse(flags); err != nil {
		return nil, err
	}
	return append(positional, fs.Args()...), nil
}

func run(argv []string) int {
	g, args, err := parseGlobals(argv)
	if err != nil {
		return 2
	}

	if len(args) == 0 {
		return runWizard()
	}

	verb := args[0]

	// Launching is not a verb, but it takes flags after its arguments too:
	// scopr gl-panel -p "..." should work.
	if !slices.Contains(verbs, verb) {
		names, err := verbFlags("scopr", args, &g)
		if err != nil {
			return 2
		}
		return runLaunch(names, g.prompt, label(names))
	}

	rest, err := verbFlags(verb, args[1:], &g)
	if err != nil {
		return 2
	}

	switch verb {
	case "where":
		return runWhere()

	case "list":
		return runList()

	case "rename":
		return runRename(rest)

	case "delete":
		if len(rest) != 1 {
			fmt.Fprintln(os.Stderr, "usage: scopr delete <name>")
			return 1
		}
		return runDelete(rest[0])

	case "infer":
		if len(rest) == 0 {
			fmt.Fprintln(os.Stderr, "usage: scopr infer <task>")
			return 1
		}
		return runInfer(strings.Join(rest, " "), g.prompt, g.verbose)

	case "save":
		if len(rest) < 2 {
			fmt.Fprintln(os.Stderr, "usage: scopr save <name> <repo>...")
			return 1
		}
		return runSave(strings.TrimPrefix(rest[0], scope.Prefix), rest[1:])

	case "workspace":
		fmt.Fprintln(os.Stderr, "workspace management is not built yet")
		return 1
	}

	return runLaunch(args, g.prompt, label(args))
}

func main() { os.Exit(run(os.Args[1:])) }

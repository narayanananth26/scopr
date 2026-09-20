package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"scopr/internal/dispatch"
	"scopr/internal/files"
	"scopr/internal/launch"
	"scopr/internal/picker"
	"scopr/internal/registry"
	"scopr/internal/resolve"
	"scopr/internal/scope"
	"scopr/internal/scopefile"
	"scopr/internal/ui"
)

// findRoot returns the workspace a command should act on: --workspace, then
// the one it was run in, then the only registered one.
func findRoot(g globals) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("couldn't resolve current working directory: %w", err)
	}
	return resolve.One(resolve.Options{Workspace: g.workspace, Cwd: cwd})
}

// findScopeRoot returns the workspace holding a named scope. Standing in a
// workspace that has it wins; otherwise every registered one is searched, and
// more than one hit is reported rather than guessed between.
func findScopeRoot(g globals, name string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("couldn't resolve current working directory: %w", err)
	}

	hits, err := resolve.Scope(resolve.Options{Workspace: g.workspace, Cwd: cwd}, name)
	if err != nil {
		return "", err
	}
	if len(hits) > 1 {
		var b strings.Builder
		fmt.Fprintf(&b, "%s%s is in more than one workspace:", scope.Prefix, name)
		for _, h := range hits {
			fmt.Fprintf(&b, "\n  %s  %s", h.Workspace.Name, h.Workspace.Path)
		}
		b.WriteString("\nname one with --workspace")
		return "", errors.New(b.String())
	}
	return hits[0].Workspace.Path, nil
}

// runWhere prints the workspace root. The path goes to stdout alone.
func runWhere(g globals) int {
	root, err := findRoot(g)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fmt.Fprintln(os.Stdout, root)
	return 0
}

func runList(g globals) int {
	root, err := findRoot(g)
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
func runSave(g globals, name string, args []string) int {
	root, err := findRoot(g)
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

func runDelete(g globals, name string) int {
	root, err := findRoot(g)
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

func runRename(g globals, args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: scopr --rename <old> <new>")
		return 1
	}

	root, err := findRoot(g)
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
func runWizard(g globals) int {
	root, err := findRoot(g)
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

	g.prompt = w.Prompt()
	return runLaunch(g, repos, w.Label())
}

// runInfer surveys the workspace for a task, shows what it found, and starts a
// session once approved.
func runInfer(g globals, task string) int {
	root, err := findRoot(g)
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
		if g.verbose {
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

	prompt := g.prompt
	if prompt == "" {
		prompt = task
	}
	g.prompt = prompt
	return runLaunch(g, args, task)
}

// runLaunch resolves names to a scope and starts a session in the primary
// repo, returning claude's exit code.
func runLaunch(g globals, args []string, name string) int {
	// A named scope says which workspace to use, so scopr @surfaces works
	// from anywhere. Plain repository names do not, and resolve as usual.
	root, err := launchRoot(g, args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	s, err := scope.ResolveArgs(root, args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	code, err := launch.Run(launch.Config{Scope: s, Prompt: g.prompt, Name: name})
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

// runWorkspace handles the workspace verbs.
func runWorkspace(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: scopr workspace add|list|remove")
		return 1
	}

	switch args[0] {
	case "add":
		path := "."
		if len(args) > 1 {
			path = args[1]
		}
		return runWorkspaceAdd(path)

	case "list":
		return runWorkspaceList()

	case "remove":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "usage: scopr workspace remove <name>")
			return 1
		}
		return runWorkspaceRemove(args[1])

	default:
		fmt.Fprintf(os.Stderr, "unknown workspace command %q; want add, list or remove\n", args[0])
		return 1
	}
}

func runWorkspaceAdd(path string) int {
	if err := registry.Add(path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	abs, err := filepath.Abs(path)
	if err == nil {
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
	}
	fmt.Fprintf(os.Stderr, "registered %s\n", abs)
	return 0
}

// runWorkspaceList prints name and path. Stale entries are shown rather than
// hidden, so a workspace that moved is visible instead of quietly absent.
func runWorkspaceList() int {
	all, err := registry.List()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(all) == 0 {
		fmt.Fprintln(os.Stderr, "no workspaces registered; add one with: scopr workspace add")
		return 0
	}

	width := 0
	for _, w := range all {
		width = max(width, len(w.Name))
	}

	for _, w := range all {
		note := ""
		if w.Stale {
			note = "  (missing)"
		}
		fmt.Fprintf(os.Stdout, "%-*s  %s%s\n", width, w.Name, w.Path, note)
	}
	return 0
}

// runWorkspaceRemove forgets a workspace. Its scopes stay on disk.
func runWorkspaceRemove(query string) int {
	matches, err := registry.Lookup(query)

	// A stale entry cannot be looked up, so fall back to removing by path.
	if err != nil {
		if rmErr := registry.Remove(query); rmErr == nil {
			fmt.Fprintf(os.Stderr, "forgot %s\n", query)
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if len(matches) > 1 {
		fmt.Fprintf(os.Stderr, "%q matches more than one workspace:\n", query)
		for _, w := range matches {
			fmt.Fprintf(os.Stderr, "  %s\n", w.Path)
		}
		return 1
	}

	if err := registry.Remove(matches[0].Path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "forgot %s\n", matches[0].Path)
	return 0
}

// launchRoot picks the workspace to launch in. The first @name among the
// arguments locates it; otherwise the usual resolution applies.
func launchRoot(g globals, args []string) (string, error) {
	for _, a := range args {
		if name, ok := strings.CutPrefix(a, scope.Prefix); ok {
			return findScopeRoot(g, name)
		}
	}
	return findRoot(g)
}

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
		return runWizard(g)
	}

	verb := args[0]

	// Launching is not a verb, but it takes flags after its arguments too:
	// scopr gl-panel -p "..." should work.
	if !slices.Contains(verbs, verb) {
		names, err := verbFlags("scopr", args, &g)
		if err != nil {
			return 2
		}
		return runLaunch(g, names, label(names))
	}

	rest, err := verbFlags(verb, args[1:], &g)
	if err != nil {
		return 2
	}

	switch verb {
	case "where":
		return runWhere(g)

	case "list":
		return runList(g)

	case "rename":
		return runRename(g, rest)

	case "delete":
		if len(rest) != 1 {
			fmt.Fprintln(os.Stderr, "usage: scopr delete <name>")
			return 1
		}
		return runDelete(g, rest[0])

	case "infer":
		if len(rest) == 0 {
			fmt.Fprintln(os.Stderr, "usage: scopr infer <task>")
			return 1
		}
		return runInfer(g, strings.Join(rest, " "))

	case "save":
		if len(rest) < 2 {
			fmt.Fprintln(os.Stderr, "usage: scopr save <name> <repo>...")
			return 1
		}
		return runSave(g, strings.TrimPrefix(rest[0], scope.Prefix), rest[1:])

	case "workspace":
		return runWorkspace(rest)
	}

	return runLaunch(g, args, label(args))
}

func main() { os.Exit(run(os.Args[1:])) }

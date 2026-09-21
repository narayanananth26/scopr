package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"scopr/internal/cli"
	"scopr/internal/dispatch"
	"scopr/internal/files"
	"scopr/internal/launch"
	"scopr/internal/picker"
	"scopr/internal/registry"
	"scopr/internal/repo"
	"scopr/internal/resolve"
	"scopr/internal/scope"
	"scopr/internal/scopefile"
	"scopr/internal/ui"
)

var version = "dev"

const surveyTimeout = 90 * time.Second

func findRoot(a cli.Args) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("couldn't resolve current working directory: %w", err)
	}
	return resolve.One(resolve.Options{Workspace: a.Str("workspace"), Cwd: cwd})
}

func findScopeRoot(a cli.Args, name string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("couldn't resolve current working directory: %w", err)
	}

	hits, err := resolve.Scope(resolve.Options{Workspace: a.Str("workspace"), Cwd: cwd}, name)
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
		return "", fmt.Errorf("%w: %s", errAmbiguousWorkspace, b.String())
	}
	return hits[0].Workspace.Path, nil
}

func emit(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runWhere(a cli.Args) int {
	root, err := findRoot(a)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if a.Bool("json") {
		return emit(struct {
			Root string `json:"root"`
		}{root})
	}

	fmt.Fprintln(os.Stdout, root)
	return 0
}

func runList(a cli.Args) int {
	root, err := findRoot(a)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	names, err := scopefile.List(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	type entry struct {
		Name  string   `json:"name"`
		Repos []string `json:"repos"`
	}

	entries := make([]entry, 0, len(names))
	for _, name := range names {
		repos, err := scopefile.Load(root, name)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		entries = append(entries, entry{name, repos})
	}

	if a.Bool("json") {
		return emit(entries)
	}

	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "no saved scopes; create one with: scopr save @name <repo>...")
		return 0
	}

	width := 0
	for _, e := range entries {
		width = max(width, len(e.Name))
	}

	for _, e := range entries {
		fmt.Fprintf(os.Stdout, "%s%-*s  %s\n", scope.Prefix, width, e.Name, strings.Join(e.Repos, " "))
	}
	return 0
}

var errAmbiguousWorkspace = errors.New("scope is in more than one workspace")

func reportScope(query string, err error) int {
	var ambiguous *scopefile.AmbiguousError
	if errors.As(err, &ambiguous) {
		var b strings.Builder
		fmt.Fprintf(&b, "%s%s matches more than one scope:", scope.Prefix, query)
		for _, m := range ambiguous.Matches {
			fmt.Fprintf(&b, "\n  %s%s", scope.Prefix, m)
		}
		fmt.Fprintln(os.Stderr, b.String())
		return 2
	}

	fmt.Fprintln(os.Stderr, err)
	if errors.Is(err, errAmbiguousWorkspace) {
		return 2
	}
	return 1
}

func echoScopes(root string, operands []string) ([]string, int) {
	out := slices.Clone(operands)

	for i, o := range out {
		name, ok := strings.CutPrefix(o, scope.Prefix)
		if !ok {
			continue
		}

		resolved, err := scopefile.One(root, name)
		if err != nil {
			return nil, reportScope(name, err)
		}
		if resolved != name {
			fmt.Fprintf(os.Stderr, "using %s%s\n", scope.Prefix, resolved)
		}
		out[i] = scope.Prefix + resolved
	}

	return out, 0
}

func oneScope(root, query string) (string, int) {
	name, err := scopefile.One(root, query)
	if err != nil {
		return "", reportScope(query, err)
	}
	return name, 0
}

func runShow(a cli.Args) int {
	root, err := findRoot(a)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	name, code := oneScope(root, strings.TrimPrefix(a.Operands[0], scope.Prefix))
	if code != 0 {
		return code
	}

	repos, err := scopefile.Load(root, name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if a.Bool("json") {
		return emit(repos)
	}

	for _, r := range repos {
		fmt.Fprintln(os.Stdout, r)
	}
	return 0
}

func runSave(a cli.Args) int {
	root, err := findRoot(a)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	name, repos := strings.TrimPrefix(a.Operands[0], scope.Prefix), a.Operands[1:]

	if _, err := scope.ResolveArgs(root, repos); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if err := scopefile.Save(root, name, repos); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "saved %s%s: %s\n", scope.Prefix, name, strings.Join(repos, " "))
	return 0
}

func runDelete(a cli.Args) int {
	root, err := findRoot(a)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	typed := strings.TrimPrefix(a.Operands[0], scope.Prefix)

	name, code := oneScope(root, typed)
	if code != 0 {
		return code
	}

	if name != typed {
		ok, err := ui.Confirm(fmt.Sprintf("delete %s%s?", scope.Prefix, name))
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s%s matched %s%s; name it in full to delete it without a terminal\n",
				scope.Prefix, typed, scope.Prefix, name)
			return 2
		}
		if !ok {
			fmt.Fprintln(os.Stderr, "cancelled")
			return 0
		}
	}

	if err := scopefile.Delete(root, name); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "deleted %s%s\n", scope.Prefix, name)
	return 0
}

func runRename(a cli.Args) int {
	root, err := findRoot(a)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	from, code := oneScope(root, strings.TrimPrefix(a.Operands[0], scope.Prefix))
	if code != 0 {
		return code
	}

	to := strings.TrimPrefix(a.Operands[1], scope.Prefix)

	if err := scopefile.Rename(root, from, to); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "renamed %s%s to %s%s\n", scope.Prefix, from, scope.Prefix, to)
	return 0
}

func noTTY(what string) int {
	fmt.Fprintf(os.Stderr, "%s needs a terminal; name the repositories instead: scopr <@scope|repo>...\n", what)
	return 2
}

func runWizard(a cli.Args) int {
	spaces, entries, err := wizardEntries(a)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(spaces) == 0 {
		fmt.Fprintln(os.Stderr, "no workspaces to start from; add one with: scopr workspace add")
		return 1
	}

	wiz := ui.NewWizard(spaces, entries,
		func(root, name string) ([]string, error) { return scopefile.Load(root, name) },
		func(root string) []string {
			repos, err := picker.Names(root)
			if err != nil {
				return nil
			}
			return repos
		},
	)

	wiz.LoadFiles = taggableFiles
	wiz = wiz.WithPrompt(a.Str("prompt"))

	w, err := ui.RunWizard(wiz)
	if errors.Is(err, ui.ErrNoTTY) {
		return noTTY("scopr")
	}
	if errors.Is(err, ui.ErrCancelled) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	root := w.Root()
	repos := w.Repos()

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

	return launchIn(a, root, repos, label(a, repos, w.Label()), w.Prompt())
}

func wizardEntries(a cli.Args) ([]ui.Space, []ui.Entry, error) {
	var roots []string

	if local, err := findRoot(a); err == nil {
		roots = append(roots, local)
	}

	live, err := registry.Live()
	if err != nil {
		return nil, nil, err
	}

	names := map[string]string{}
	for _, w := range live {
		names[w.Path] = w.Name
		if !slices.Contains(roots, w.Path) {
			roots = append(roots, w.Path)
		}
	}

	var (
		spaces  []ui.Space
		entries []ui.Entry
	)

	for _, root := range roots {
		name, ok := names[root]
		if !ok {
			name = filepath.Base(root)
		}
		spaces = append(spaces, ui.Space{Name: name, Root: root})

		scopes, err := scopefile.List(root)
		if err != nil {
			continue
		}
		for _, sc := range scopes {
			repos, err := scopefile.Load(root, sc)
			if err != nil {
				continue
			}
			entries = append(entries, ui.Entry{Root: root, Scope: sc, Repos: repos})
		}
	}
	return spaces, entries, nil
}

func taggableFiles(root string, names []string) []files.File {
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

func launchIn(a cli.Args, root string, repos []string, name, prompt string) int {
	s, err := scope.ResolveArgs(root, repos)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, repo.ErrNoSuchRepo) {
			if hint := a.CommandHint(); hint != "" {
				fmt.Fprintln(os.Stderr, hint)
			}
		}
		return 1
	}

	code, err := launch.Run(launch.Config{Scope: s, Prompt: prompt, Name: name})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return code
}

func launchRoot(a cli.Args) (string, string, error) {
	for _, o := range a.Operands {
		if name, ok := strings.CutPrefix(o, scope.Prefix); ok {
			root, err := findScopeRoot(a, name)
			return root, name, err
		}
	}
	root, err := findRoot(a)
	return root, "", err
}

func label(a cli.Args, repos []string, fallback string) string {
	if l := a.Str("label"); l != "" {
		return l
	}
	if len(repos) == 1 && strings.HasPrefix(repos[0], scope.Prefix) {
		return strings.TrimPrefix(repos[0], scope.Prefix)
	}
	return fallback
}

func runLaunch(a cli.Args) int {
	root, query, err := launchRoot(a)
	if err != nil {
		return reportScope(query, err)
	}

	repos, code := echoScopes(root, a.Operands)
	if code != 0 {
		return code
	}

	return launchIn(a, root, repos, label(a, repos, ""), a.Str("prompt"))
}

func runInfer(a cli.Args) int {
	task := a.Operands[0]

	root, err := findRoot(a)
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
		if a.Bool("verbose") {
			cfg.Trace = trace
		}
		var err error
		suggestions, err = dispatch.Infer(ctx, cfg)
		return err
	})

	scoped := picker.Scope{Header: "suggested for: " + task}

	switch {
	case errors.Is(err, ui.ErrNoTTY):
		return noTTY("scopr infer")

	case errors.Is(err, ui.ErrCancelled):
		return 0

	case err == nil:
		scoped.Notes = make(map[string]string, len(suggestions))
		for _, sg := range suggestions {
			scoped.Names = append(scoped.Names, sg.Name)
			scoped.Notes[sg.Name] = sg.Reason
		}

	case errors.Is(err, dispatch.ErrDeclined):
		scoped.Header = "nothing suggested - " + err.Error()

	default:
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	repos, err := picker.Edit(root, scoped)
	if errors.Is(err, ui.ErrNoTTY) {
		return noTTY("scopr infer")
	}
	if errors.Is(err, picker.ErrCancelled) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	prompt := a.Str("prompt")
	if prompt == "" {
		prompt = task
	}

	return launchIn(a, root, repos, label(a, repos, task), prompt)
}

func runWorkspaceAdd(a cli.Args) int {
	path := "."
	if len(a.Operands) > 0 {
		path = a.Operands[0]
	}

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
	fmt.Fprintf(os.Stderr, "added %s\n", abs)
	return 0
}

func runWorkspaceList(a cli.Args) int {
	all, err := registry.List()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if a.Bool("json") {
		type entry struct {
			Name  string `json:"name"`
			Path  string `json:"path"`
			Stale bool   `json:"stale"`
		}
		entries := make([]entry, 0, len(all))
		for _, w := range all {
			entries = append(entries, entry{w.Name, w.Path, w.Stale})
		}
		return emit(entries)
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

func runWorkspaceRemove(a cli.Args) int {
	query := a.Operands[0]

	matches, err := registry.Lookup(query)
	if err != nil {
		abs, absErr := filepath.Abs(query)
		if absErr == nil {
			if rmErr := registry.Remove(abs); rmErr == nil {
				fmt.Fprintf(os.Stderr, "removed %s\n", abs)
				return 0
			}
		}
		if rmErr := registry.Remove(query); rmErr == nil {
			fmt.Fprintf(os.Stderr, "removed %s\n", query)
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
		return 2
	}

	if err := registry.Remove(matches[0].Path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "removed %s\n", matches[0].Path)
	return 0
}

func line(path []string, c *cli.Command) string {
	parts := append([]string{"scopr"}, path...)
	if c.Use != "" {
		parts = append(parts, c.Use)
	}
	return strings.Join(parts, " ")
}

func lines(path []string, c *cli.Command, out *[][2]string) {
	if c.Help != "" {
		*out = append(*out, [2]string{line(path, c), c.Help})
	}
	for _, k := range c.Children {
		lines(slices.Concat(path, []string{k.Name}), k, out)
	}
}

func flagLine(f cli.Flag) string {
	var b strings.Builder

	if f.Short != "" {
		fmt.Fprintf(&b, "-%s, ", f.Short)
	} else {
		b.WriteString("    ")
	}
	fmt.Fprintf(&b, "--%s", f.Name)
	if f.Arg != "" {
		fmt.Fprintf(&b, " %s", f.Arg)
	}
	return b.String()
}

func usage(path []string, c *cli.Command) {
	var rows [][2]string
	lines(path, c, &rows)

	width := 0
	for _, r := range rows {
		width = max(width, len(r[0]))
	}

	w := os.Stderr
	root := c == cli.Commands

	if root {
		fmt.Fprint(w, "scopr launches Claude Code scoped to chosen repositories.\n\nusage:\n")
		fmt.Fprintf(w, "  %-*s  %s\n", width, "scopr", "name, scope and prompt, step by step")
	} else {
		fmt.Fprint(w, "usage:\n")
	}

	for _, r := range rows {
		fmt.Fprintf(w, "  %-*s  %s\n", width, r[0], r[1])
	}

	fmt.Fprint(w, "\nflags:\n")
	for f := range cli.Flags.All() {
		if !root && !accepts(c, f.Name) {
			continue
		}
		fmt.Fprintf(w, "  %-20s  %s\n", flagLine(f), f.Help)
	}

	if root {
		fmt.Fprint(w, `
Flags may appear anywhere, and -- ends them so a repository whose name begins
with a dash can still be named. A word that is also a command reaches its
repository through scopr run.

An @name argument expands to the repositories that scope names, so scopes and
plain repositories can be mixed.
`)
	}
}

func accepts(c *cli.Command, name string) bool {
	if name == "help" {
		return true
	}
	return slices.ContainsFunc(c.Accepts, func(f *cli.Flag) bool { return f.Name == name })
}

func runHelp(a cli.Args) int {
	if len(a.Operands) == 0 {
		usage(nil, cli.Commands)
		return 0
	}

	for _, c := range cli.Commands.Children {
		if c.Name == a.Operands[0] {
			usage([]string{c.Name}, c)
			return 0
		}
	}

	fmt.Fprintf(os.Stderr, "unknown command %q\n", a.Operands[0])
	return 2
}

func dispatchArgs(a cli.Args) int {
	switch strings.Join(a.Path, " ") {
	case "":
		if len(a.Operands) == 0 {
			return runWizard(a)
		}
		return runLaunch(a)

	case "run":
		return runLaunch(a)

	case "infer":
		return runInfer(a)

	case "list":
		return runList(a)

	case "show":
		return runShow(a)

	case "save":
		return runSave(a)

	case "delete":
		return runDelete(a)

	case "rename":
		return runRename(a)

	case "where":
		return runWhere(a)

	case "workspace list":
		return runWorkspaceList(a)

	case "workspace add":
		return runWorkspaceAdd(a)

	case "workspace remove":
		return runWorkspaceRemove(a)

	case "help":
		return runHelp(a)

	case "version":
		fmt.Fprintln(os.Stdout, version)
		return 0
	}

	usage(nil, cli.Commands)
	return 2
}

func run(argv []string) int {
	a, err := cli.Parse(argv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	if a.Help {
		usage(a.Path, a.Command)
		return 0
	}

	return dispatchArgs(a)
}

func main() { os.Exit(run(os.Args[1:])) }

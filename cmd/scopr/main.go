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

	"scopr/completions"
	"scopr/internal/cli"
	"scopr/internal/complete"
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
		fmt.Fprintf(&b, "%s%s is in %d workspaces; pick one with -w:", scope.Prefix, name, len(hits))
		for _, h := range hits {
			fmt.Fprintf(&b, "\n  %s", a.Retry(h.Workspace.Name))
		}
		return "", &usageError{b.String()}
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

func whereIs(a cli.Args) (string, bool) {
	if a.Has("workspace") {
		root, err := findRoot(a)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return "", false
		}
		return root, true
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return "", false
	}

	w, ok := registry.Containing(cwd)
	if !ok {
		fmt.Fprintln(os.Stderr, "not inside a scopr workspace")
		fmt.Fprintln(os.Stderr, "  scopr workspace add .   register this directory")
		fmt.Fprintln(os.Stderr, "  scopr workspace list    see the ones you have")
		return "", false
	}
	return w.Path, true
}

func runWhere(a cli.Args) int {
	root, ok := whereIs(a)
	if !ok {
		return 1
	}

	name := registry.NameOf(root)

	if a.Bool("json") {
		return emit(struct {
			Name string `json:"name"`
			Root string `json:"root"`
		}{name, root})
	}

	fmt.Fprintf(os.Stdout, "%s  %s\n", name, root)
	return 0
}

type scopeEntry struct {
	Name  string   `json:"name"`
	Repos []string `json:"repos"`
}

func scopesIn(root string) ([]scopeEntry, error) {
	names, err := scopefile.List(root)
	if err != nil {
		return nil, err
	}

	out := make([]scopeEntry, 0, len(names))
	for _, name := range names {
		repos, err := scopefile.Load(root, name)
		if err != nil {
			return nil, err
		}
		out = append(out, scopeEntry{name, repos})
	}
	return out, nil
}

func widest(entries []scopeEntry) int {
	w := 0
	for _, e := range entries {
		w = max(w, len(e.Name))
	}
	return w
}

func printScopes(entries []scopeEntry, width, indent int) {
	for _, e := range entries {
		fmt.Fprintf(os.Stdout, "%*s%s%-*s  %s\n",
			indent, "", scope.Prefix, width, e.Name, strings.Join(e.Repos, " "))
	}
}

func runList(a cli.Args) int {
	if !a.Has("workspace") {
		return runListAll(a)
	}

	root, err := findRoot(a)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	entries, err := scopesIn(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if a.Bool("json") {
		return emit(entries)
	}

	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "no saved scopes; create one with: scopr save @name <repo>...")
		return 0
	}

	printScopes(entries, widest(entries), 0)
	return 0
}

func runListAll(a cli.Args) int {
	live, err := registry.Live()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	type group struct {
		Workspace string       `json:"workspace"`
		Path      string       `json:"path"`
		Scopes    []scopeEntry `json:"scopes"`
	}

	var (
		groups []group
		width  int
	)

	for _, w := range live {
		entries, err := scopesIn(w.Path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if len(entries) == 0 {
			continue
		}
		groups = append(groups, group{w.Name, w.Path, entries})
		width = max(width, widest(entries))
	}

	if a.Bool("json") {
		return emit(groups)
	}

	if len(groups) == 0 {
		fmt.Fprintln(os.Stderr, "no saved scopes in any workspace; create one with: scopr save @name <repo>...")
		return 0
	}

	for i, g := range groups {
		if i > 0 {
			fmt.Fprintln(os.Stdout)
		}
		fmt.Fprintln(os.Stdout, g.Workspace)
		printScopes(g.Scopes, width, 2)
	}
	return 0
}

type usageError struct{ text string }

func (e *usageError) Error() string { return e.text }

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
	var misuse *usageError
	if errors.As(err, &misuse) {
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

func localScope(a cli.Args, root, query string) (string, int) {
	name, err := scopefile.One(root, query)
	if err == nil {
		return name, 0
	}
	if !errors.Is(err, scopefile.ErrNoSuchScope) {
		return "", reportScope(query, err)
	}

	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		return "", reportScope(query, err)
	}

	var others []registry.Workspace
	if hits, lookErr := resolve.Scope(resolve.Options{Cwd: cwd}, query); lookErr == nil {
		for _, h := range hits {
			if h.Workspace.Path != root {
				others = append(others, h.Workspace)
			}
		}
	}
	if len(others) == 0 {
		return "", reportScope(query, err)
	}

	here := registry.NameOf(root)
	if len(others) == 1 {
		fmt.Fprintf(os.Stderr, "no %s%s in %s; it is in %s\n", scope.Prefix, query, here, others[0].Name)
	} else {
		fmt.Fprintf(os.Stderr, "no %s%s in %s; it is in %d other workspaces:\n", scope.Prefix, query, here, len(others))
	}
	for _, w := range others {
		fmt.Fprintf(os.Stderr, "  %s\n", a.Retry(w.Name))
	}
	return "", 2
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

	s, err := scope.ResolveArgs(root, repos)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	paths, err := s.Paths()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if err := scopefile.Save(root, name, paths); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "saved %s%s in %s: %s\n", scope.Prefix, name, registry.NameOf(root), strings.Join(paths, " "))
	return 0
}

func runDelete(a cli.Args) int {
	root, err := findRoot(a)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	typed := strings.TrimPrefix(a.Operands[0], scope.Prefix)

	name, code := localScope(a, root, typed)
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

	fmt.Fprintf(os.Stderr, "deleted %s%s in %s\n", scope.Prefix, name, registry.NameOf(root))
	return 0
}

func runRename(a cli.Args) int {
	root, err := findRoot(a)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	from, code := localScope(a, root, strings.TrimPrefix(a.Operands[0], scope.Prefix))
	if code != 0 {
		return code
	}

	to := strings.TrimPrefix(a.Operands[1], scope.Prefix)

	if err := scopefile.Rename(root, from, to); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "renamed %s%s to %s%s in %s\n", scope.Prefix, from, scope.Prefix, to, registry.NameOf(root))
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
		s, err := scope.Resolve(root, repos)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		paths, err := s.Paths()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := scopefile.Save(root, name, paths); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "saved %s%s in %s\n", scope.Prefix, name, registry.NameOf(root))
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
	var root, first string

	for _, o := range a.Operands {
		name, ok := strings.CutPrefix(o, scope.Prefix)
		if !ok {
			continue
		}

		at, err := findScopeRoot(a, name)
		if err != nil {
			return "", name, err
		}

		if root == "" {
			root, first = at, name
			continue
		}
		if at != root {
			return "", name, &usageError{fmt.Sprintf("%s%s is in %s and %s%s is in %s; a launch uses one workspace",
				scope.Prefix, first, registry.NameOf(root), scope.Prefix, name, registry.NameOf(at))}
		}
	}

	if root != "" {
		return root, first, nil
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

	w := matches[0]

	scopes, err := scopefile.List(w.Path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if len(scopes) > 0 && !a.Bool("force") {
		ok, err := ui.Confirm(fmt.Sprintf("remove %s and delete %s?", w.Name, count(len(scopes), "scope")))
		if errors.Is(err, ui.ErrNoTTY) {
			fmt.Fprintf(os.Stderr, "%s has %s; remove it with --force to delete them\n", w.Name, count(len(scopes), "scope"))
			return 2
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if !ok {
			fmt.Fprintln(os.Stderr, "cancelled")
			return 0
		}
	}

	if err := registry.Remove(w.Path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.RemoveAll(filepath.Join(w.Path, ".scopr")); err != nil {
		fmt.Fprintf(os.Stderr, "removed %s, but its scopes are still there: %v\n", w.Name, err)
		return 1
	}

	if len(scopes) == 0 {
		fmt.Fprintf(os.Stderr, "removed %s\n", w.Name)
	} else {
		fmt.Fprintf(os.Stderr, "removed %s, deleted %s\n", w.Name, count(len(scopes), "scope"))
	}
	return 0
}

func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
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
	shown := documented()
	for f := range cli.Flags.All() {
		if root && !shown[f.Name] {
			continue
		}
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

// A flag no documented command takes belongs to a hidden one.
func documented() map[string]bool {
	out := map[string]bool{"help": true}

	var walk func(*cli.Command)
	walk = func(c *cli.Command) {
		if c.Help != "" {
			for _, f := range c.Accepts {
				out[f.Name] = true
			}
		}
		for _, k := range c.Children {
			walk(k)
		}
	}
	walk(cli.Commands)

	return out
}

func runCompletion(a cli.Args) int {
	switch a.Operands[0] {
	case "zsh":
		fmt.Fprint(os.Stdout, completions.Zsh)
		return 0
	}

	fmt.Fprintf(os.Stderr, "no completion script for %q; scopr has one for zsh\n", a.Operands[0])
	return 2
}

const completeProtocol = "2"

func completeEnv() complete.Env {
	return complete.Env{
		Root: func(workspace string) string {
			cwd, err := os.Getwd()
			if err != nil {
				return ""
			}
			root, err := resolve.One(resolve.Options{Workspace: workspace, Cwd: cwd})
			if err != nil {
				return ""
			}
			return root
		},

		Scopes: func(workspace string, local bool) []complete.Candidate {
			spaces, err := registry.Live()
			if workspace != "" {
				spaces, err = registry.Lookup(workspace)
			}
			if err != nil || workspace != "" && len(spaces) > 1 {
				return nil
			}

			cwd, _ := os.Getwd()
			here, _ := resolve.One(resolve.Options{Cwd: cwd})

			wins := func(w registry.Workspace, name string) bool {
				if workspace != "" {
					return true
				}
				if local {
					return w.Path == here
				}
				hits, err := resolve.Scope(resolve.Options{Cwd: cwd}, name)
				return err == nil && len(hits) == 1 && hits[0].Workspace.Path == w.Path && hits[0].Scope == name
			}

			var out []complete.Candidate
			for _, w := range spaces {
				entries, err := scopesIn(w.Path)
				if err != nil {
					continue
				}
				for _, e := range entries {
					c := complete.Candidate{
						Value: e.Name,
						Desc:  "(" + w.Name + ") " + strings.Join(e.Repos, " "),
					}
					if !wins(w, e.Name) {
						c.Pin = w.Name
					}
					out = append(out, c)
				}
			}
			return out
		},

		Repos: func(root string) []complete.Candidate {
			if root == "" {
				return nil
			}
			names, err := picker.Names(root)
			if err != nil {
				return nil
			}
			out := make([]complete.Candidate, 0, len(names))
			for _, n := range names {
				out = append(out, complete.Candidate{Value: n})
			}
			return out
		},

		Workspaces: func() []complete.Candidate {
			all, err := registry.Live()
			if err != nil {
				return nil
			}
			out := make([]complete.Candidate, 0, len(all))
			for _, w := range all {
				out = append(out, complete.Candidate{Value: w.Name, Desc: w.Path})
			}
			return out
		},
	}
}

// Always exits 0 and never writes to stderr: a completion function must not
// put anything in front of the prompt.
func runComplete(a cli.Args) int {
	if p := a.Str("protocol"); p != "" && p != completeProtocol {
		return 0
	}

	r := complete.Complete(completeEnv(), a.Operands)

	var b strings.Builder
	for _, c := range r.Candidates {
		raw := ""
		if c.Pin != "" {
			raw = "raw"
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", c.Insert(), c.Desc, c.Group, raw)
	}
	if r.Files {
		b.WriteString(":1\n")
	} else {
		b.WriteString(":0\n")
	}

	os.Stdout.WriteString(b.String())
	return 0
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

	case "completion":
		return runCompletion(a)

	case "__complete":
		return runComplete(a)
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

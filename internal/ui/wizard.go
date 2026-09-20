package ui

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"scopr/internal/files"
	"scopr/internal/scopefile"

	tea "github.com/charmbracelet/bubbletea"
)

// scopePrefix marks a saved scope in display only; scope owns the real one.
const scopePrefix = "@"

// step is which screen the wizard is on.
type step int

const (
	stepWorkspace step = iota
	stepName
	stepScope
	stepPrompt
	stepDone
)

// Space is a workspace to work in.
type Space struct {
	Name string
	Root string
}

// Entry is a saved scope the name step can start from.
type Entry struct {
	// Root is the workspace holding it.
	Root string

	// Scope is the saved scope's name.
	Scope string

	// Repos is what it holds, for showing beside it.
	Repos []string
}

// Wizard walks name, then scope, then prompt.
//
// The name step is where the workspace is chosen: every entry names one, so
// picking a scope picks its workspace too. Typing a name that matches nothing
// offers to create it, once per workspace, so the ambiguity is visible rather
// than guessed at.
type Wizard struct {
	// Spaces are the workspaces to choose between, the one you are standing
	// in first.
	Spaces []Space

	// Entries are the saved scopes across all of them.
	Entries []Entry

	// Load returns the repositories a saved scope holds.
	Load func(root, name string) ([]string, error)

	// ReposIn returns every repository in a workspace.
	ReposIn func(root string) []string

	// LoadFiles reads the taggable files for a scope. It runs once the scope
	// is settled, so a large workspace is read while the prompt is being
	// typed rather than before it appears.
	LoadFiles func([]string) []files.File

	// Files are taggable paths. Nil until loaded, which the tag list reports
	// rather than pretending to be empty.
	Files []files.File

	step   step
	name   string
	nameAt int

	wsAt int

	// space is the workspace chosen at the first step; chosen is the scope
	// chosen at the second, empty when starting fresh.
	space  Space
	chosen Entry

	prompt    string
	scope     Model
	tagging   bool
	query     string
	tagAt     int
	tagCursor int

	// Err is set when a saved scope could not be loaded.
	Err error

	Cancelled bool
}

func NewWizard(spaces []Space, entries []Entry, load func(root, name string) ([]string, error), repos func(root string) []string) Wizard {
	w := Wizard{
		Spaces:  slices.Clone(spaces),
		Entries: slices.Clone(entries),
		Load:    load,
		ReposIn: repos,
	}

	// One workspace is not a choice.
	if len(spaces) == 1 {
		w.space = spaces[0]
		w.step = stepName
	}
	return w
}

// matchesName are the chosen workspace's scopes the typed name selects, plus a
// create entry when the name matches none of them.
func (w Wizard) matchesName() []Entry {
	q := strings.ToLower(strings.TrimSpace(w.name))

	var hits []Entry
	for _, e := range w.Entries {
		if e.Root != w.space.Root {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(e.Scope), q) {
			hits = append(hits, e)
		}
	}

	// An unnamed session is still a session: without this, a blank name and
	// enter would load the first saved scope instead of starting fresh.
	if q == "" {
		return append(hits, Entry{Root: w.space.Root})
	}

	// A name that cannot be saved must not be offered: the wizard would run
	// to the end and fail at the write.
	if scopefile.ValidName(strings.TrimSpace(w.name)) != nil {
		return hits
	}

	if !slices.ContainsFunc(hits, func(e Entry) bool { return strings.EqualFold(e.Scope, q) }) {
		hits = append(hits, Entry{Root: w.space.Root, Scope: strings.TrimSpace(w.name)})
	}
	return hits
}

// Name is the scope name to save under, empty when nothing should be written:
// an existing scope needs no saving, and a workspace entry names nothing.
func (w Wizard) Name() string {
	if w.chosen.Scope == "" || w.existingScope() {
		return ""
	}
	return w.chosen.Scope
}

// Root is the workspace the session belongs to.
func (w Wizard) Root() string { return w.space.Root }

// Label is what the session was called, whether or not it is saved. A name is
// useful for the terminal tab even when nothing is written.
func (w Wizard) Label() string { return w.chosen.Scope }

// Repos is the chosen scope.
func (w Wizard) Repos() []string { return w.scope.Result() }

// Prompt is the task to submit on start, empty when none was given.
func (w Wizard) Prompt() string { return strings.TrimSpace(w.prompt) }

// Done reports that the wizard finished and a session should start.
func (w Wizard) Done() bool { return w.step == stepDone && !w.Cancelled }

// existingScope reports whether the chosen entry is a scope that already
// exists, rather than one about to be created.
func (w Wizard) existingScope() bool {
	for _, e := range w.Entries {
		if e.Root == w.space.Root && e.Scope != "" && e.Scope == w.chosen.Scope {
			return true
		}
	}
	return false
}

// Key applies one keystroke.
func (w Wizard) Key(k string) Wizard {
	switch w.step {
	case stepWorkspace:
		return w.keyWorkspace(k)
	case stepName:
		return w.keyName(k)
	case stepScope:
		return w.keyScope(k)
	case stepPrompt:
		return w.keyPrompt(k)
	}
	return w
}

func (w Wizard) keyWorkspace(k string) Wizard {
	switch k {
	case "ctrl+c", "esc", "q":
		w.Cancelled = true

	case "up", "k", "ctrl+p":
		if w.wsAt > 0 {
			w.wsAt--
		}

	case "down", "j", "ctrl+n":
		if w.wsAt < len(w.Spaces)-1 {
			w.wsAt++
		}

	case "enter":
		if len(w.Spaces) == 0 {
			return w
		}
		w.space = w.Spaces[min(w.wsAt, len(w.Spaces)-1)]
		w.step = stepName
	}

	return w
}

func (w Wizard) keyName(k string) Wizard {
	switch k {
	case "ctrl+c":
		w.Cancelled = true

	case "esc":
		if len(w.Spaces) > 1 {
			w.step = stepWorkspace
			w.Err = nil
			return w
		}
		w.Cancelled = true

	case "up", "ctrl+p":
		if w.nameAt > 0 {
			w.nameAt--
		}

	case "down", "ctrl+n":
		if w.nameAt < len(w.matchesName())-1 {
			w.nameAt++
		}

	case "enter":
		hits := w.matchesName()
		if len(hits) == 0 {
			return w
		}
		w.chosen = hits[min(w.nameAt, len(hits)-1)]

		var repos []string
		if w.existingScope() {
			loaded, err := w.Load(w.space.Root, w.chosen.Scope)
			if err != nil {
				w.Err = err
				return w
			}
			repos = loaded
		}

		w.scope = New(repos, w.ReposIn(w.space.Root))
		w.step = stepScope

	case "backspace":
		if w.name != "" {
			w.name = w.name[:len(w.name)-1]
			w.nameAt = 0
		}

	default:
		if len([]rune(k)) == 1 {
			w.name += k
			// Typing reorders the list, so a held cursor would point at a
			// different entry than the one under it a moment ago.
			w.nameAt = 0
		}
	}

	return w
}

func (w Wizard) keyScope(k string) Wizard {
	// Escaping the scope step goes back to the name rather than out, so a
	// mistyped name is one keystroke to fix.
	if k == "esc" && w.scope.mode == modeScope {
		w.step = stepName
		return w
	}

	w.scope = w.scope.key(k)

	switch {
	case w.scope.Cancelled:
		w.Cancelled = true
	case w.scope.Done:
		w.step = stepPrompt
	}

	return w
}

// fileLimit is how many matches the tag list shows. More than a screenful is
// a reason to type another character, not to scroll.
const fileLimit = 10

// matches are the files the current tag query selects.
func (w Wizard) matches() []files.File {
	return files.Match(w.Files, w.query, fileLimit)
}

func (w Wizard) keyTag(k string) Wizard {
	switch k {
	case "ctrl+c":
		w.Cancelled = true

	case "esc":
		// Drop the @ too, so escaping leaves no half-typed tag.
		w.prompt = w.prompt[:w.tagAt]
		w.tagging = false
		w.query = ""

	case "up", "ctrl+p":
		if w.tagCursor > 0 {
			w.tagCursor--
		}

	case "down", "ctrl+n":
		if w.tagCursor < len(w.matches())-1 {
			w.tagCursor++
		}

	case "enter", "tab":
		if m := w.matches(); w.tagCursor < len(m) {
			w.prompt = w.prompt[:w.tagAt] + "@" + m[w.tagCursor].Rel + " "
		}
		w.tagging = false
		w.query = ""

	case "backspace":
		if w.query == "" {
			w.prompt = w.prompt[:w.tagAt]
			w.tagging = false
			return w
		}
		w.query = w.query[:len(w.query)-1]
		w.prompt = w.prompt[:w.tagAt] + "@" + w.query
		// A narrower query reorders the list, so a held cursor would point
		// at a different file than the one under it a moment ago.
		w.tagCursor = 0

	case " ":
		// A space ends a tag nobody completed.
		w.tagging = false
		w.query = ""
		w.prompt += " "

	default:
		if len([]rune(k)) == 1 {
			w.query += k
			w.prompt = w.prompt[:w.tagAt] + "@" + w.query
			w.tagCursor = 0
		}
	}

	return w
}

func (w Wizard) keyPrompt(k string) Wizard {
	if w.tagging {
		return w.keyTag(k)
	}

	if k == "@" {
		w.tagging = true
		w.tagAt = len(w.prompt)
		w.query = ""
		w.tagCursor = 0
		w.prompt += "@"
		return w
	}

	switch k {
	case "ctrl+c":
		w.Cancelled = true

	case "esc":
		w.step = stepScope
		w.scope.Done = false

	case "enter":
		w.step = stepDone

	case "backspace":
		if w.prompt != "" {
			w.prompt = w.prompt[:len(w.prompt)-1]
		}

	default:
		if len([]rune(k)) == 1 {
			w.prompt += k
		}
	}

	return w
}

func (w Wizard) view() string {
	switch w.step {
	case stepWorkspace:
		return w.viewWorkspace()
	case stepName:
		return w.viewName()
	case stepScope:
		return w.scope.View()
	case stepPrompt:
		if w.tagging {
			return w.viewTag()
		}
		return w.viewPrompt()
	}
	return ""
}

func (w Wizard) viewWorkspace() string {
	var b strings.Builder

	b.WriteString("which workspace?\n\n")

	if len(w.Spaces) == 0 {
		b.WriteString(dim.Render("  none registered - add one with: scopr workspace add") + "\n")
	}

	width := 0
	for _, sp := range w.Spaces {
		width = max(width, len(sp.Name))
	}

	for i, sp := range w.Spaces {
		cursor := "  "
		if i == w.wsAt {
			cursor = marker.Render("> ")
		}

		n := 0
		for _, e := range w.Entries {
			if e.Root == sp.Root {
				n++
			}
		}

		scopes := dim.Render("  no saved scopes")
		if n == 1 {
			scopes = dim.Render("  1 scope")
		} else if n > 1 {
			scopes = dim.Render(fmt.Sprintf("  %d scopes", n))
		}

		b.WriteString(cursor + sp.Name + strings.Repeat(" ", width-len(sp.Name)) + scopes + "\n")
	}

	b.WriteString("\n" + dim.Render("j/k move  enter continue  esc cancel") + "\n")
	return b.String()
}

func (w Wizard) viewName() string {
	var b strings.Builder

	b.WriteString("what are you working on?\n")
	b.WriteString(dim.Render("  in "+w.space.Name) + "\n\n")
	fmt.Fprintf(&b, "  %s%s%s\n\n", dim.Render(scopePrefix), w.name, marker.Render("_"))

	if w.Err != nil {
		b.WriteString(dim.Render("  "+w.Err.Error()) + "\n\n")
	}

	hits := w.matchesName()
	if len(hits) == 0 {
		if err := scopefile.ValidName(strings.TrimSpace(w.name)); err != nil && w.name != "" {
			b.WriteString(dim.Render("  "+err.Error()) + "\n")
		} else {
			b.WriteString(dim.Render("  nothing matches") + "\n")
		}
	}

	// Scopes are written @name everywhere else, so they read that way here.
	width := len("(unnamed)")
	for _, e := range hits {
		if e.Scope != "" {
			width = max(width, len(scopePrefix)+len(e.Scope))
		}
	}

	for i, e := range hits {
		if i >= 12 {
			b.WriteString(dim.Render(fmt.Sprintf("  ... %d more", len(hits)-i)) + "\n")
			break
		}

		cursor := "  "
		if i == w.nameAt {
			cursor = marker.Render("> ")
		}

		shown := scopePrefix + e.Scope
		if e.Scope == "" {
			shown = "(unnamed)"
		}
		b.WriteString(cursor + shown + strings.Repeat(" ", width-len(shown)))

		switch {
		case e.Scope == "":
			b.WriteString(dim.Render("  start fresh, unnamed"))
		case len(e.Repos) > 0:
			b.WriteString(dim.Render("  " + strings.Join(e.Repos, " ")))
		default:
			b.WriteString(dim.Render("  new"))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n" + dim.Render("type to filter  up/down move  enter continue  esc back") + "\n")
	return b.String()
}

func (w Wizard) viewPrompt() string {
	var b strings.Builder

	b.WriteString("what are you working on?\n")
	b.WriteString(dim.Render("  submitted when the session starts") + "\n\n")
	fmt.Fprintf(&b, "  %s%s\n\n", w.prompt, marker.Render("_"))

	b.WriteString(dim.Render("  "+strings.Join(w.scope.Chosen, "  ")) + "\n")
	b.WriteString("\n" + dim.Render("enter start  esc back  blank to start without one") + "\n")

	return b.String()
}

func (w Wizard) viewTag() string {
	var b strings.Builder

	b.WriteString("what are you working on?\n")
	b.WriteString(dim.Render("  tagging a file") + "\n\n")
	fmt.Fprintf(&b, "  %s%s\n\n", w.prompt, marker.Render("_"))

	switch m := w.matches(); {
	case w.Files == nil:
		b.WriteString(dim.Render("  still reading the workspace...") + "\n")
	case len(m) == 0:
		b.WriteString(dim.Render("  no files match") + "\n")
	default:
		for i, f := range m {
			if i == w.tagCursor {
				b.WriteString(marker.Render("> ") + f.Rel + "\n")
				continue
			}
			b.WriteString(dim.Render("  "+f.Rel) + "\n")
		}
	}

	b.WriteString("\n" + dim.Render("type to filter  up/down move  enter or tab to tag  esc to drop it") + "\n")
	return b.String()
}

// FilesMsg carries the taggable files once they have been read.
type FilesMsg []files.File

// loadCmd reads the taggable files off the update loop.
func (w Wizard) loadCmd() tea.Cmd {
	if w.LoadFiles == nil {
		return nil
	}
	repos := slices.Clone(w.scope.Chosen)
	return func() tea.Msg { return FilesMsg(w.LoadFiles(repos)) }
}

// Init satisfies tea.Model.
func (w Wizard) Init() tea.Cmd { return nil }

// Update satisfies tea.Model.
func (w Wizard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if loaded, ok := msg.(FilesMsg); ok {
		w.Files = loaded
		return w, nil
	}

	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return w, nil
	}

	before := w.step
	w = w.Key(k.String())

	if w.Cancelled || w.step == stepDone {
		return w, tea.Quit
	}

	// The scope is settled on entering the prompt, so that is when the file
	// list can be read.
	if before == stepScope && w.step == stepPrompt && w.Files == nil {
		return w, w.loadCmd()
	}
	return w, nil
}

func (w Wizard) View() string { return w.view() }

// RunWizard walks name, scope and prompt, and reports what was chosen.
func RunWizard(w Wizard) (Wizard, error) {
	out, err := tea.NewProgram(w, tea.WithOutput(os.Stderr)).Run()
	if err != nil {
		return Wizard{}, fmt.Errorf("run wizard: %w", err)
	}

	done, ok := out.(Wizard)
	if !ok {
		return Wizard{}, errors.New("wizard returned an unexpected model")
	}
	if !done.Done() || len(done.Repos()) == 0 {
		return Wizard{}, ErrCancelled
	}
	return done, nil
}

package ui

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"scopr/internal/files"

	tea "github.com/charmbracelet/bubbletea"
)

// scopePrefix marks a saved scope in display only; scope owns the real one.
const scopePrefix = "@"

// step is which screen the wizard is on.
type step int

const (
	stepName step = iota
	stepScope
	stepPrompt
	stepDone
)

// Wizard walks name, then scope, then prompt.
//
// The name step doubles as scope selection: an existing name loads that scope,
// a new one saves what you build, and a blank one starts an unsaved session.
type Wizard struct {
	// Saved is the existing scope names, for loading and for telling a new
	// name from an existing one.
	Saved []string

	// Load returns the repositories a saved scope holds.
	Load func(name string) ([]string, error)

	// Available is every repository in the workspace.
	Available []string

	// LoadFiles reads the taggable files for a scope. It runs once the scope
	// is settled, so a large workspace is read while the prompt is being
	// typed rather than before it appears.
	LoadFiles func([]string) []files.File

	// Files are taggable paths. Nil until loaded, which the tag list reports
	// rather than pretending to be empty.
	Files []files.File

	step      step
	name      string
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

func NewWizard(saved, available []string, load func(string) ([]string, error)) Wizard {
	return Wizard{
		Saved:     slices.Clone(saved),
		Available: slices.Clone(available),
		Load:      load,
		scope:     New(nil, available),
	}
}

// Name is the scope name to save under, empty when the session is unsaved.
// It is empty for an existing scope too, which needs no saving.
func (w Wizard) Name() string {
	if w.existing() {
		return ""
	}
	return w.name
}

// Repos is the chosen scope.
func (w Wizard) Repos() []string { return w.scope.Result() }

// Prompt is the task to submit on start, empty when none was given.
func (w Wizard) Prompt() string { return strings.TrimSpace(w.prompt) }

// Done reports that the wizard finished and a session should start.
func (w Wizard) Done() bool { return w.step == stepDone && !w.Cancelled }

// existing reports whether the typed name is already a saved scope.
func (w Wizard) existing() bool {
	return slices.Contains(w.Saved, strings.TrimSpace(w.name))
}

// Key applies one keystroke.
func (w Wizard) Key(k string) Wizard {
	switch w.step {
	case stepName:
		return w.keyName(k)
	case stepScope:
		return w.keyScope(k)
	case stepPrompt:
		return w.keyPrompt(k)
	}
	return w
}

func (w Wizard) keyName(k string) Wizard {
	switch k {
	case "ctrl+c", "esc":
		w.Cancelled = true

	case "enter":
		if w.existing() {
			repos, err := w.Load(strings.TrimSpace(w.name))
			if err != nil {
				w.Err = err
				return w
			}
			w.scope = New(repos, w.Available)
		}
		w.step = stepScope

	case "backspace":
		if w.name != "" {
			w.name = w.name[:len(w.name)-1]
		}

	default:
		if len([]rune(k)) == 1 {
			w.name += k
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

func (w Wizard) viewName() string {
	var b strings.Builder

	b.WriteString("name this session\n\n")
	fmt.Fprintf(&b, "  %s%s\n\n", w.name, marker.Render("_"))

	switch name := strings.TrimSpace(w.name); {
	case w.Err != nil:
		b.WriteString(dim.Render("  "+w.Err.Error()) + "\n")
	case name == "":
		b.WriteString(dim.Render("  unnamed - the scope will not be saved") + "\n")
	case w.existing():
		b.WriteString(dim.Render("  loads "+scopePrefix+name) + "\n")
	default:
		b.WriteString(dim.Render("  saves as "+scopePrefix+name) + "\n")
	}

	if len(w.Saved) > 0 {
		b.WriteString("\n" + dim.Render("  saved: "+scopePrefix+strings.Join(w.Saved, "  "+scopePrefix)) + "\n")
	}

	b.WriteString("\n" + dim.Render("enter continue  esc cancel") + "\n")
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

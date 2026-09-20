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

const scopePrefix = "@"

type step int

const (
	stepWorkspace step = iota
	stepName
	stepScope
	stepPrompt
	stepDone
)

type Space struct {
	Name string
	Root string
}

type Entry struct {
	Root string

	Scope string

	Repos []string
}

type Wizard struct {
	Spaces []Space

	Entries []Entry

	Load func(root, name string) ([]string, error)

	ReposIn func(root string) []string

	LoadFiles func(root string, names []string) []files.File

	Files []files.File

	// Separates "not read yet" from "read, and there are none".
	filesLoaded bool

	step   step
	name   string
	nameAt int

	wsAt  int
	width int

	space  Space
	chosen Entry

	prompt   string
	scope    Model
	promptAt int

	editing bool

	tagging   bool
	query     string
	tagHead   string
	tagTail   string
	tagCursor int

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

	if len(spaces) == 1 {
		w.space = spaces[0]
		w.step = stepName
	}
	return w
}

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

	if q == "" {
		return append(hits, Entry{Root: w.space.Root})
	}

	if scopefile.ValidName(strings.TrimSpace(w.name)) != nil {
		return hits
	}

	if !slices.ContainsFunc(hits, func(e Entry) bool { return strings.EqualFold(e.Scope, q) }) {
		hits = append(hits, Entry{Root: w.space.Root, Scope: strings.TrimSpace(w.name)})
	}
	return hits
}

func (w Wizard) Name() string {
	if w.chosen.Scope == "" || w.existingScope() {
		return ""
	}
	return w.chosen.Scope
}

func (w Wizard) Root() string { return w.space.Root }

func (w Wizard) Label() string { return w.chosen.Scope }

func (w Wizard) Repos() []string { return w.scope.Result() }

func (w Wizard) Prompt() string { return strings.TrimSpace(w.prompt) }

func (w Wizard) Done() bool { return w.step == stepDone && !w.Cancelled }

func (w Wizard) existingScope() bool {
	for _, e := range w.Entries {
		if e.Root == w.space.Root && e.Scope != "" && e.Scope == w.chosen.Scope {
			return true
		}
	}
	return false
}

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

			w.nameAt = 0
		}
	}

	return w
}

func (w Wizard) keyScope(k string) Wizard {
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

const fileLimit = 10

func (w Wizard) matches() []files.File {
	return files.Match(w.Files, w.query, fileLimit)
}

func (w Wizard) keyTag(k string) Wizard {
	switch k {
	case "ctrl+c":
		w.Cancelled = true

	case "esc":

		w = w.endTag("")

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
			w = w.endTag("@" + m[w.tagCursor].Rel + " ")
		} else {
			w = w.endTag("")
		}

	case "backspace":
		if w.query == "" {
			return w.endTag("")
		}
		w.query = w.query[:len(w.query)-1]

		w.tagCursor = 0
		w = w.redrawTag()

	case " ":

		w = w.endTag("@" + w.query + " ")

	default:
		if len([]rune(k)) == 1 {
			w.query += k
			w.tagCursor = 0
			w = w.redrawTag()
		}
	}

	return w
}

func (w Wizard) redrawTag() Wizard {
	w.prompt = w.tagHead + "@" + w.query + w.tagTail
	w.promptAt = len([]rune(w.tagHead)) + 1 + len([]rune(w.query))
	return w
}

func (w Wizard) endTag(inserted string) Wizard {
	w.prompt = w.tagHead + inserted + w.tagTail
	w.promptAt = len([]rune(w.tagHead)) + len([]rune(inserted))
	w.tagging = false
	w.query = ""
	w.tagHead, w.tagTail = "", ""
	return w
}

func (w Wizard) insertPrompt(s string) Wizard {
	r := []rune(w.prompt)
	at := min(w.promptAt, len(r))

	w.prompt = string(r[:at]) + s + string(r[at:])
	w.promptAt = at + len([]rune(s))
	return w
}

func (w Wizard) keyPrompt(k string) Wizard {
	if w.tagging {
		return w.keyTag(k)
	}

	r := []rune(w.prompt)
	at := min(w.promptAt, len(r))

	switch k {
	case "ctrl+c":
		w.Cancelled = true

	case "esc":
		w.step = stepScope
		w.scope.Done = false

	case "enter":
		w.step = stepDone

	// ctrl+e is end-of-line, so the editor gets ctrl+o.
	case "ctrl+o":
		w.editing = true

	case "@":
		w.tagging = true
		w.tagHead = string(r[:at])
		w.tagTail = string(r[at:])
		w.query = ""
		w.tagCursor = 0
		w = w.redrawTag()

	case "left", "ctrl+b":
		if at > 0 {
			w.promptAt = at - 1
		}

	case "right", "ctrl+f":
		if at < len(r) {
			w.promptAt = at + 1
		}

	case "home", "ctrl+a":
		w.promptAt = 0

	case "end", "ctrl+e":
		w.promptAt = len(r)

	case "backspace":
		if at > 0 {
			w.prompt = string(r[:at-1]) + string(r[at:])
			w.promptAt = at - 1
		}

	case "delete", "ctrl+d":
		if at < len(r) {
			w.prompt = string(r[:at]) + string(r[at+1:])
		}

	case "ctrl+u":
		w.prompt = string(r[at:])
		w.promptAt = 0

	case "ctrl+k":
		w.prompt = string(r[:at])

	default:
		if len([]rune(k)) == 1 {
			w = w.insertPrompt(k)
		}
	}

	return w
}

func (w Wizard) promptLine() string {
	r := []rune(w.prompt)
	at := min(w.promptAt, len(r))

	if at >= len(r) {
		return w.prompt + cursor.Render(" ")
	}
	return string(r[:at]) + cursor.Render(string(r[at])) + string(r[at+1:])
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

		b.WriteString(clipTo(cursor+sp.Name+strings.Repeat(" ", width-len(sp.Name))+scopes, w.width) + "\n")
	}

	b.WriteString("\n" + dim.Render(clipTo("j/k move  enter continue  esc cancel", w.width)) + "\n")
	return b.String()
}

func (w Wizard) viewName() string {
	var b strings.Builder

	b.WriteString("what are you working on?\n")
	b.WriteString(dim.Render(clipTo("  in "+w.space.Name, w.width)) + "\n\n")
	fmt.Fprintf(&b, "  %s%s%s\n\n", dim.Render(scopePrefix), w.name, cursor.Render(" "))

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
		row := cursor + shown + strings.Repeat(" ", width-len(shown))

		switch {
		case e.Scope == "":
			row += dim.Render("  start fresh, unnamed")
		case len(e.Repos) > 0:
			row += dim.Render("  " + strings.Join(e.Repos, " "))
		default:
			row += dim.Render("  new")
		}
		b.WriteString(clipTo(row, w.width) + "\n")
	}

	b.WriteString("\n" + dim.Render(clipTo("type to filter  up/down move  enter continue  esc back", w.width)) + "\n")
	return b.String()
}

func (w Wizard) viewPrompt() string {
	var b strings.Builder

	b.WriteString("what are you working on?\n")
	b.WriteString(dim.Render("  submitted when the session starts") + "\n")
	if w.Err != nil {
		b.WriteString(dim.Render("  "+w.Err.Error()) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(indent(wrapTo(w.promptLine(), w.width-2), "  ") + "\n\n")

	b.WriteString(dim.Render(clipTo("  "+strings.Join(w.scope.Chosen, "  "), w.width)) + "\n")
	b.WriteString("\n" + dim.Render(clipTo("@ tag a file  ctrl+o "+editorName()+"  enter start  esc back", w.width)) + "\n")

	return b.String()
}

func (w Wizard) viewTag() string {
	var b strings.Builder

	b.WriteString("what are you working on?\n")
	b.WriteString(dim.Render("  tagging a file") + "\n\n")
	b.WriteString(indent(wrapTo(w.promptLine(), w.width-2), "  ") + "\n\n")

	switch m := w.matches(); {
	case !w.filesLoaded:
		b.WriteString(dim.Render("  still reading the scope...") + "\n")
	case len(w.Files) == 0:
		b.WriteString(dim.Render("  no files found in this scope") + "\n")
	case len(m) == 0:
		b.WriteString(dim.Render("  no files match") + "\n")
	default:
		for i, f := range m {
			if i == w.tagCursor {
				b.WriteString(clipTo(marker.Render("> ")+f.Rel, w.width) + "\n")
				continue
			}
			b.WriteString(dim.Render(clipTo("  "+f.Rel, w.width)) + "\n")
		}
	}

	b.WriteString("\n" + dim.Render(clipTo("type to filter  up/down move  enter or tab to tag  esc to drop it", w.width)) + "\n")
	return b.String()
}

type FilesMsg []files.File

func (w Wizard) loadCmd() tea.Cmd {
	if w.LoadFiles == nil {
		return nil
	}
	root := w.space.Root
	repos := slices.Clone(w.scope.Chosen)
	return func() tea.Msg { return FilesMsg(w.LoadFiles(root, repos)) }
}

func (w Wizard) Init() tea.Cmd { return nil }

func (w Wizard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		w.width = size.Width
		w.scope.width = size.Width
		return w, nil
	}

	if loaded, ok := msg.(FilesMsg); ok {
		w.Files = loaded
		w.filesLoaded = true
		return w, nil
	}

	if edited, ok := msg.(EditedMsg); ok {
		if edited.Err != nil {
			w.Err = edited.Err
			return w, nil
		}
		w.Err = nil
		w.prompt = edited.Text
		w.promptAt = len([]rune(w.prompt))
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

	if w.editing {
		w.editing = false
		return w, editExternally(w.prompt)
	}

	if before == stepScope && w.step == stepPrompt && !w.filesLoaded {
		return w, w.loadCmd()
	}
	return w, nil
}

func (w Wizard) View() string { return w.view() }

func RunWizard(w Wizard) (Wizard, error) {
	out, err := tea.NewProgram(w, tea.WithOutput(os.Stderr), tea.WithAltScreen()).Run()
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

package ui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ErrCancelled reports that nothing was chosen. Starting nothing is the right
// outcome when nobody chose anything.
var ErrCancelled = errors.New("selection cancelled")

// renderer draws to stderr, where the screens go. lipgloss's default renderer
// detects the colour profile from stdout; when stdout is redirected it decides
// the terminal has no colour and silently strips every style, including the
// cursor.
var renderer = lipgloss.NewRenderer(os.Stderr)

var (
	dim     = renderer.NewStyle().Faint(true)
	primary = renderer.NewStyle().Bold(true)
	marker  = renderer.NewStyle().Bold(true)

	// cursor is reverse video, so it reads as a block sitting on a character.
	// Bold alone is invisible on a character that is already there.
	cursor = renderer.NewStyle().Reverse(true)
)

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update satisfies tea.Model. Keystrokes are delegated to key so the logic is
// testable without terminal messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
		return m, nil
	}

	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	m = m.key(k.String())
	if m.Done || m.Cancelled {
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) View() string {
	if m.mode == modeAdd {
		return m.viewAdd()
	}
	return m.viewScope()
}

func (m Model) viewScope() string {
	var b strings.Builder

	b.WriteString("scope\n")
	if m.Header != "" {
		b.WriteString(dim.Render(clipTo("  "+m.Header, m.width)) + "\n")
	}
	b.WriteString("\n")

	if len(m.Chosen) == 0 {
		b.WriteString(dim.Render("  nothing chosen - press a to add") + "\n")
	}

	width := 0
	for _, name := range m.Chosen {
		if len(name) > width {
			width = len(name)
		}
	}

	for i, name := range m.Chosen {
		cursor := "  "
		if i == m.cursor {
			cursor = marker.Render("> ")
		}

		shown := name
		if i == 0 {
			shown = primary.Render(name)
		}

		row := cursor + shown + strings.Repeat(" ", width-len(name))

		switch note := m.Notes[name]; {
		case i == 0 && note != "":
			row += dim.Render("  working directory - " + note)
		case i == 0:
			row += dim.Render("  working directory")
		case note != "":
			row += dim.Render("  " + note)
		}
		b.WriteString(clipTo(row, m.width) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(dim.Render(clipTo("j/k move  J/K reorder  x remove  a add  enter start  esc cancel", m.width)))
	b.WriteString("\n")

	return b.String()
}

func (m Model) viewAdd() string {
	var b strings.Builder

	fmt.Fprintf(&b, "add a repository\n\n  %s%s\n\n", m.query, marker.Render("_"))

	c := m.candidates()
	if len(c) == 0 {
		b.WriteString(dim.Render("  no matches") + "\n")
	}

	for i, name := range c {
		if i >= 12 {
			fmt.Fprintf(&b, "%s\n", dim.Render(fmt.Sprintf("  ... %d more", len(c)-i)))
			break
		}
		cursor := "  "
		if i == m.addCursor {
			cursor = marker.Render("> ")
		}
		fmt.Fprintf(&b, "%s%s\n", cursor, name)
	}

	b.WriteString("\n")
	b.WriteString(dim.Render(clipTo("type to filter  up/down move  enter add  esc back", m.width)))
	b.WriteString("\n")

	return b.String()
}

// Run shows the editor and returns the accepted scope.
func Run(m Model) ([]string, error) {
	out, err := tea.NewProgram(m, tea.WithOutput(os.Stderr), tea.WithAltScreen()).Run()
	if err != nil {
		return nil, fmt.Errorf("run editor: %w", err)
	}

	m, ok := out.(Model)
	if !ok {
		return nil, errors.New("editor returned an unexpected model")
	}

	result := m.Result()
	if len(result) == 0 {
		return nil, ErrCancelled
	}
	return result, nil
}

// defaultWidth is used until the terminal says otherwise. Eighty is the
// conventional fallback and narrow enough to be safe.
const defaultWidth = 80

// wrapTo folds text at the given width, so a long prompt stays on screen
// rather than running past the edge.
//
// ANSI-aware: the cursor and the styles are escape sequences, and counting
// them as characters would wrap several columns early.
func wrapTo(s string, width int) string {
	if width <= 0 {
		width = defaultWidth
	}
	return ansi.Wrap(s, width, "")
}

// clipTo cuts a line at the given width, for rows where wrapping would
// misalign a list rather than help.
func clipTo(s string, width int) string {
	if width <= 0 {
		width = defaultWidth
	}
	return ansi.Truncate(s, width, "...")
}

// indent puts a prefix on every line, so wrapped text lines up under the
// first.
func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

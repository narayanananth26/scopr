package ui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ErrCancelled reports that nothing was chosen. Starting nothing is the right
// outcome when nobody chose anything.
var ErrCancelled = errors.New("selection cancelled")

var (
	dim     = lipgloss.NewStyle().Faint(true)
	primary = lipgloss.NewStyle().Bold(true)
	marker  = lipgloss.NewStyle().Bold(true)
)

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update satisfies tea.Model. Keystrokes are delegated to key so the logic is
// testable without terminal messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	b.WriteString("scope\n\n")

	if len(m.Chosen) == 0 {
		b.WriteString(dim.Render("  (empty - press a to add)") + "\n")
	}

	for i, name := range m.Chosen {
		cursor := "  "
		if i == m.cursor {
			cursor = marker.Render("> ")
		}

		line := name
		if i == 0 {
			line = primary.Render(name) + dim.Render("  working directory")
		}
		fmt.Fprintf(&b, "%s%s\n", cursor, line)
	}

	b.WriteString("\n")
	b.WriteString(dim.Render("j/k move  J/K reorder  x remove  a add  enter start  esc cancel"))
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
	b.WriteString(dim.Render("type to filter  up/down move  enter add  esc back"))
	b.WriteString("\n")

	return b.String()
}

// Run shows the editor and returns the accepted scope.
func Run(chosen, available []string) ([]string, error) {
	out, err := tea.NewProgram(New(chosen, available), tea.WithOutput(os.Stderr)).Run()
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

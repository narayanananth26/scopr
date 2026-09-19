package ui

import (
	"slices"
	"strings"
)

// mode is which list the cursor is in.
type mode int

const (
	modeScope mode = iota // editing the chosen set
	modeAdd               // filtering the workspace to add one
)

// Model edits an ordered set of repository names.
//
// Order is not cosmetic for the first entry: it becomes the working directory
// and decides whose config loads. The rest become --add-dir, where order has
// no effect.
type Model struct {
	// Chosen is the scope being edited, in order.
	Chosen []string

	// Available is every name in the workspace, including those chosen.
	Available []string

	mode   mode
	cursor int

	// add-mode state
	query      string
	addCursor  int
	addMatches []string

	// Done is set when the person accepted the scope.
	Done bool

	// Cancelled is set when they backed out.
	Cancelled bool
}

func New(chosen, available []string) Model {
	m := Model{
		Chosen:    slices.Clone(chosen),
		Available: slices.Clone(available),
	}
	return m
}

// Result is the edited scope, or nil if cancelled or emptied.
func (m Model) Result() []string {
	if m.Cancelled || len(m.Chosen) == 0 {
		return nil
	}
	return m.Chosen
}

// candidates are the workspace names not already chosen, filtered by query.
func (m Model) candidates() []string {
	var out []string
	for _, name := range m.Available {
		if slices.Contains(m.Chosen, name) {
			continue
		}
		if m.query == "" || strings.Contains(strings.ToLower(name), strings.ToLower(m.query)) {
			out = append(out, name)
		}
	}
	return out
}

// key applies one keystroke. Split from Update so the logic is exercised
// without constructing terminal messages.
func (m Model) key(k string) Model {
	if m.mode == modeAdd {
		return m.keyAdd(k)
	}
	return m.keyScope(k)
}

func (m Model) keyScope(k string) Model {
	switch k {
	case "ctrl+c", "esc", "q":
		m.Cancelled = true

	case "enter":
		if len(m.Chosen) > 0 {
			m.Done = true
		}

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}

	case "down", "j":
		if m.cursor < len(m.Chosen)-1 {
			m.cursor++
		}

	// Moving an entry is how the primary is chosen: the first is the one
	// that matters.
	case "K", "shift+up":
		if m.cursor > 0 {
			m.Chosen[m.cursor-1], m.Chosen[m.cursor] = m.Chosen[m.cursor], m.Chosen[m.cursor-1]
			m.cursor--
		}

	case "J", "shift+down":
		if m.cursor < len(m.Chosen)-1 {
			m.Chosen[m.cursor+1], m.Chosen[m.cursor] = m.Chosen[m.cursor], m.Chosen[m.cursor+1]
			m.cursor++
		}

	case "x", "d", "delete", "backspace":
		if len(m.Chosen) > 0 {
			m.Chosen = slices.Delete(m.Chosen, m.cursor, m.cursor+1)
			if m.cursor >= len(m.Chosen) && m.cursor > 0 {
				m.cursor--
			}
		}

	case "a":
		m.mode = modeAdd
		m.query = ""
		m.addCursor = 0
	}

	return m
}

func (m Model) keyAdd(k string) Model {
	switch k {
	case "ctrl+c":
		m.Cancelled = true

	case "esc":
		m.mode = modeScope
		m.query = ""

	case "enter":
		c := m.candidates()
		if m.addCursor < len(c) {
			m.Chosen = append(m.Chosen, c[m.addCursor])
			m.cursor = len(m.Chosen) - 1
		}
		m.mode = modeScope
		m.query = ""

	case "up":
		if m.addCursor > 0 {
			m.addCursor--
		}

	case "down":
		if m.addCursor < len(m.candidates())-1 {
			m.addCursor++
		}

	case "backspace":
		if m.query != "" {
			m.query = m.query[:len(m.query)-1]
			m.addCursor = 0
		}

	default:
		// Single printable runes extend the filter.
		if len([]rune(k)) == 1 {
			m.query += k
			m.addCursor = 0
		}
	}

	return m
}

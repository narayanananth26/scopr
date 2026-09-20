package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// EditedMsg carries what the external editor was left holding.
type EditedMsg struct {
	Text string
	Err  error
}

// editorCommand is the editor to hand the prompt to, split into a command and
// its arguments so EDITOR="code -w" works.
//
// VISUAL before EDITOR is the old convention: VISUAL is the full-screen one,
// EDITOR may be a line editor.
func editorCommand() []string {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return strings.Fields(v)
		}
	}
	return []string{"vi"}
}

// editExternally hands the text to a real editor and reports what comes back.
//
// Emulating vim would mean reimplementing operators, text objects and word
// boundaries; running the editor means the person's own configuration applies
// and nothing is approximated.
func editExternally(text string) tea.Cmd {
	// The extension is what makes filetype rules fire, so the editor behaves
	// as it would on any other prose.
	file, err := os.CreateTemp("", "scopr-prompt-*.md")
	if err != nil {
		return func() tea.Msg { return EditedMsg{Err: fmt.Errorf("create scratch file: %w", err)} }
	}
	path := file.Name()

	if _, err := file.WriteString(text); err != nil {
		file.Close()
		os.Remove(path)
		return func() tea.Msg { return EditedMsg{Err: fmt.Errorf("write scratch file: %w", err)} }
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return func() tea.Msg { return EditedMsg{Err: fmt.Errorf("close scratch file: %w", err)} }
	}

	argv := append(editorCommand(), path)
	cmd := exec.Command(argv[0], argv[1:]...)

	// ExecProcess hands the terminal over and restores the screen after, so
	// the editor gets a terminal to itself.
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(path)

		if err != nil {
			return EditedMsg{Err: fmt.Errorf("run %s: %w", filepath.Base(argv[0]), err)}
		}

		out, err := os.ReadFile(path)
		if err != nil {
			return EditedMsg{Err: fmt.Errorf("read back: %w", err)}
		}

		// A trailing newline is what an editor leaves behind, not something
		// the person typed.
		return EditedMsg{Text: strings.TrimRight(string(out), "\n")}
	})
}

// editorName is how the editor is named in the help line, so the key says
// which program it will open.
func editorName() string {
	return filepath.Base(editorCommand()[0])
}

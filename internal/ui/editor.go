package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type EditedMsg struct {
	Text string
	Err  error
}

func editorCommand() []string {
	// VISUAL before EDITOR: VISUAL is the full-screen one.
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return strings.Fields(v)
		}
	}
	return []string{"vi"}
}

func editExternally(text string) tea.Cmd {
	// The extension is what makes the editor's filetype rules fire.
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

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(path)

		if err != nil {
			return EditedMsg{Err: fmt.Errorf("run %s: %w", filepath.Base(argv[0]), err)}
		}

		out, err := os.ReadFile(path)
		if err != nil {
			return EditedMsg{Err: fmt.Errorf("read back: %w", err)}
		}

		return EditedMsg{Text: strings.TrimRight(string(out), "\n")}
	})
}

func editorName() string {
	return filepath.Base(editorCommand()[0])
}

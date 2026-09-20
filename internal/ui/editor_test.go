package ui

import (
	"slices"
	"testing"
)

func TestEditorCommandPrefersVisual(t *testing.T) {
	t.Setenv("VISUAL", "nvim")
	t.Setenv("EDITOR", "ed")

	if got := editorCommand(); !slices.Equal(got, []string{"nvim"}) {
		t.Errorf("editorCommand = %v, want nvim", got)
	}
}

func TestEditorCommandFallsBackToEditor(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "nvim")

	if got := editorCommand(); !slices.Equal(got, []string{"nvim"}) {
		t.Errorf("editorCommand = %v, want nvim", got)
	}
}

func TestEditorCommandFallsBackToVi(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")

	if got := editorCommand(); !slices.Equal(got, []string{"vi"}) {
		t.Errorf("editorCommand = %v, want vi", got)
	}
}

func TestEditorCommandSplitsArguments(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "code -w --new-window")

	if got := editorCommand(); !slices.Equal(got, []string{"code", "-w", "--new-window"}) {
		t.Errorf("editorCommand = %v, want the flags kept", got)
	}
}

func TestEditorNameIsTheProgram(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "/opt/homebrew/bin/nvim -u NONE")

	if got := editorName(); got != "nvim" {
		t.Errorf("editorName = %q, want nvim", got)
	}
}

package ui

import (
	"errors"
	"testing"
)

func TestRunNeedsATerminal(t *testing.T) {
	if _, err := Run(New(nil, []string{"a", "b"})); !errors.Is(err, ErrNoTTY) {
		t.Fatalf("Run error = %v, want ErrNoTTY", err)
	}
}

func TestRunWizardNeedsATerminal(t *testing.T) {
	w := NewWizard(
		[]Space{{Name: "w", Root: "/tmp"}},
		nil,
		func(root, name string) ([]string, error) { return nil, nil },
		func(root string) []string { return []string{"a"} },
	)

	if _, err := RunWizard(w); !errors.Is(err, ErrNoTTY) {
		t.Fatalf("RunWizard error = %v, want ErrNoTTY", err)
	}
}

func TestConfirmNeedsATerminal(t *testing.T) {
	if _, err := Confirm("delete @web?"); !errors.Is(err, ErrNoTTY) {
		t.Fatalf("Confirm error = %v, want ErrNoTTY", err)
	}
}

func TestYesAcceptsOnlyAnExplicitYes(t *testing.T) {
	for line, want := range map[string]bool{
		"y\n":      true,
		"Y\n":      true,
		"yes\n":    true,
		"  YES \n": true,
		"n\n":      false,
		"no\n":     false,
		"\n":       false,
		"":         false,
		"nope\n":   false,
		"ye\n":     false,
		"yeah\n":   false,
	} {
		if got := yes(line); got != want {
			t.Errorf("yes(%q) = %v, want %v", line, got, want)
		}
	}
}

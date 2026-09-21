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

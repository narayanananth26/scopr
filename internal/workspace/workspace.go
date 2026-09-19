package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrNotFound = errors.New("no .scopr workspace found")

// Searches upward from startDir until it finds .scopr dir.
// Returns absolute path of scopr workspace root and error
func Find(startDir string) (string, error) {
	if startDir == "" {
		return "", fmt.Errorf("start path is empty")
	}

	absDir, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute path for %q: %w", startDir, err)
	}

	current, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve symlinks for %q: %w", absDir, err)
	}

	info, err := os.Stat(current)
	if err != nil {
		return "", fmt.Errorf("failed to describe file: %w", err)
	}

	if !info.IsDir() {
		return "", fmt.Errorf("start path %q is not a directory", current)
	}

	for {
		found, err := hasMarker(current)
		if err != nil {
			return "", fmt.Errorf("couldn't find scopr workspace for %q: %w", current, err)
		}

		if found {
			return current, nil
		}

		// check parent
		parent := filepath.Dir(current)
		if parent == current {
			break
		}

		current = parent
	}

	return "", ErrNotFound
}

func hasMarker(dir string) (bool, error) {
	marker := filepath.Join(dir, ".scopr")

	info, err := os.Stat(marker)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}

	if !info.IsDir() {
		return false, fmt.Errorf("%q exists but is not a directory", marker)
	}

	return true, nil
}

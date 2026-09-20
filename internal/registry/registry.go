package registry

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// marker is the directory that makes a path a workspace. It is the same one
// workspace.Find walks up looking for.
const marker = ".scopr"

var (
	ErrNotDirectory  = errors.New("not a directory")
	ErrNoSuchEntry   = errors.New("workspace not registered")
	ErrAmbiguousName = errors.New("ambiguous workspace")
)

// Workspace is a registered workspace.
type Workspace struct {
	// Path is absolute and is the key: two workspaces cannot share one.
	Path string

	// Name is for display and for --workspace. It is the shortest trailing
	// piece of the path that no other registered workspace shares, so it can
	// change when another workspace is registered.
	Name string

	// Stale reports that the path is gone, or is no longer a workspace.
	Stale bool
}

// file is the registry: $XDG_CONFIG_HOME/scopr/workspaces, or ~/.config if
// that is unset.
//
// Not os.UserConfigDir, which on macOS is ~/Library/Application Support and
// ignores XDG_CONFIG_HOME. Not ~/.scopr either: workspace.Find walks up
// looking for a directory of that name, so a registry there would make the
// home directory a workspace.
func file() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home directory: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "scopr", "workspaces"), nil
}

// read returns the registered paths in file order. A missing registry is an
// empty list.
func read() ([]string, error) {
	path, err := file()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read registry: %w", err)
	}

	var paths []string
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		paths = append(paths, line)
	}
	return paths, nil
}

// write replaces the registry atomically, so a crash cannot leave it holding
// fewer workspaces than it named.
func write(paths []string) error {
	path, err := file()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".workspaces.*")
	if err != nil {
		return fmt.Errorf("create temp registry: %w", err)
	}
	defer os.Remove(tmp.Name())

	body := ""
	if len(paths) > 0 {
		body = strings.Join(paths, "\n") + "\n"
	}
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return fmt.Errorf("write registry: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close registry: %w", err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("install registry: %w", err)
	}
	return nil
}

// List returns every registered workspace with its display name, sorted by
// path. Entries whose path is gone are marked stale rather than dropped: a
// dead entry should not break an unrelated launch, and should not vanish
// without being seen.
func List() ([]Workspace, error) {
	paths, err := read()
	if err != nil {
		return nil, err
	}

	slices.Sort(paths)
	paths = slices.Compact(paths)

	out := make([]Workspace, 0, len(paths))
	for _, p := range paths {
		out = append(out, Workspace{Path: p, Stale: !isWorkspace(p)})
	}

	for i, name := range Names(paths) {
		out[i].Name = name
	}
	return out, nil
}

// Live is List without the stale entries, for resolving against.
func Live() ([]Workspace, error) {
	all, err := List()
	if err != nil {
		return nil, err
	}

	out := make([]Workspace, 0, len(all))
	for _, w := range all {
		if !w.Stale {
			out = append(out, w)
		}
	}
	return out, nil
}

// Add registers a directory, creating its marker so the directory is a
// workspace by cwd detection too. Registering twice is a no-op.
func Add(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", path, err)
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", path, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("inspect %q: %w", abs, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %q", ErrNotDirectory, abs)
	}

	if err := os.MkdirAll(filepath.Join(abs, marker), 0o755); err != nil {
		return fmt.Errorf("create marker in %q: %w", abs, err)
	}

	paths, err := read()
	if err != nil {
		return err
	}
	if slices.Contains(paths, abs) {
		return nil
	}
	return write(append(paths, abs))
}

// Remove forgets a workspace. The marker directory is left alone: it holds the
// saved scopes.
func Remove(path string) error {
	paths, err := read()
	if err != nil {
		return err
	}

	i := slices.Index(paths, path)
	if i < 0 {
		return fmt.Errorf("%w: %q", ErrNoSuchEntry, path)
	}
	return write(slices.Delete(paths, i, i+1))
}

// Lookup finds a workspace by display name, by any trailing piece of its path,
// or by the whole path. More than one match is returned for the caller to
// resolve rather than guessed at.
func Lookup(query string) ([]Workspace, error) {
	all, err := Live()
	if err != nil {
		return nil, err
	}

	q := strings.Trim(filepath.ToSlash(query), "/")
	if q == "" {
		return nil, fmt.Errorf("%w: empty", ErrNoSuchEntry)
	}

	var matches []Workspace
	for _, w := range all {
		if w.Name == q || w.Path == query || hasSegmentSuffix(w.Path, q) {
			matches = append(matches, w)
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrNoSuchEntry, query)
	}
	return matches, nil
}

// Names returns a display name per path, in the given order.
//
// Each starts at its base name; while any name is shared, only the colliding
// ones take another parent segment. Paths are unique, so this terminates, and
// a name that is already unique never grows.
func Names(paths []string) []string {
	depth := make([]int, len(paths))
	for i := range depth {
		depth[i] = 1
	}

	for range maxSegments(paths) {
		names := render(paths, depth)

		counts := make(map[string]int, len(names))
		for _, n := range names {
			counts[n]++
		}

		grew := false
		for i, n := range names {
			if counts[n] > 1 && depth[i] < segments(paths[i]) {
				depth[i]++
				grew = true
			}
		}
		if !grew {
			break
		}
	}

	return render(paths, depth)
}

func render(paths []string, depth []int) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		parts := strings.Split(strings.Trim(filepath.ToSlash(p), "/"), "/")
		n := min(depth[i], len(parts))
		out[i] = strings.Join(parts[len(parts)-n:], "/")
	}
	return out
}

func segments(path string) int {
	return len(strings.Split(strings.Trim(filepath.ToSlash(path), "/"), "/"))
}

func maxSegments(paths []string) int {
	most := 0
	for _, p := range paths {
		most = max(most, segments(p))
	}
	return most
}

// hasSegmentSuffix reports whether q matches whole trailing segments of path,
// so "Ananth/Goodlife" matches but "life" does not.
func hasSegmentSuffix(path, q string) bool {
	parts := strings.Split(strings.Trim(filepath.ToSlash(path), "/"), "/")
	want := strings.Split(q, "/")

	if len(want) > len(parts) {
		return false
	}
	return slices.Equal(parts[len(parts)-len(want):], want)
}

// isWorkspace reports whether the path is still a directory holding a marker.
func isWorkspace(path string) bool {
	info, err := os.Stat(filepath.Join(path, marker))
	return err == nil && info.IsDir()
}

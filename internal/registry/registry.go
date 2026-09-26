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

const marker = ".scopr"

var (
	ErrNotDirectory  = errors.New("not a directory")
	ErrNoSuchEntry   = errors.New("workspace not registered")
	ErrAmbiguousName = errors.New("ambiguous workspace")
)

type Workspace struct {
	Path string

	Name string

	Stale bool
}

// Not os.UserConfigDir, which ignores XDG_CONFIG_HOME on macOS. Not ~/.scopr,
// which workspace.Find would then treat as a workspace.
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

func List() ([]Workspace, error) {
	paths, err := read()
	if err != nil {
		return nil, err
	}

	slices.Sort(paths)
	paths = slices.Compact(paths)

	out := make([]Workspace, 0, len(paths))
	for _, p := range paths {
		out = append(out, Workspace{Path: p, Stale: !isDir(p)})
	}

	for i, name := range Names(paths) {
		out[i].Name = name
	}
	return out, nil
}

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

func LivePaths() ([]string, error) {
	live, err := Live()
	if err != nil {
		return nil, err
	}

	out := make([]string, len(live))
	for i, w := range live {
		out[i] = w.Path
	}
	return out, nil
}

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

	paths, err := read()
	if err != nil {
		return err
	}
	if slices.Contains(paths, abs) {
		return nil
	}

	if err := os.MkdirAll(filepath.Join(abs, marker), 0o755); err != nil {
		return fmt.Errorf("create marker in %q: %w", abs, err)
	}

	return write(append(paths, abs))
}

func under(path, root string) bool {
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

// NameOf is the display name for a registered path, or its base name when
// the path is not registered.
func NameOf(path string) string {
	all, err := List()
	if err == nil {
		for _, w := range all {
			if w.Path == path {
				return w.Name
			}
		}
	}
	return filepath.Base(path)
}

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

func hasSegmentSuffix(path, q string) bool {
	parts := strings.Split(strings.Trim(filepath.ToSlash(path), "/"), "/")
	want := strings.Split(q, "/")

	if len(want) > len(parts) {
		return false
	}
	return slices.Equal(parts[len(parts)-len(want):], want)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func Containing(dir string) (Workspace, bool) {
	all := Enclosing(dir)
	if len(all) == 0 {
		return Workspace{}, false
	}
	return all[0], true
}

// Enclosing is every registered workspace holding dir, innermost first.
// Registered paths are symlink-resolved, so dir has to be too before comparing.
func Enclosing(dir string) []Workspace {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}

	all, err := Live()
	if err != nil {
		return nil
	}

	var out []Workspace
	for _, w := range all {
		if w.Path == dir || under(dir, w.Path) {
			out = append(out, w)
		}
	}

	slices.SortFunc(out, func(a, b Workspace) int { return len(b.Path) - len(a.Path) })
	return out
}

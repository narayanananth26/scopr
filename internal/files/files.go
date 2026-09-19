package files

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// walkCap bounds a non-git directory, which has no .gitignore to keep a walk
// honest.
const walkCap = 5000

// File is a taggable path.
type File struct {
	// Rel is the path as it must be typed after @: relative to the primary
	// repo, so a file elsewhere in the scope reads as ../other/src/thing.ts.
	Rel string

	// Base is the file name, for ranking.
	Base string
}

// List returns every taggable file in the scope.
//
// Tracked files come from git, which applies .gitignore for free. Without that
// a walk of a JavaScript repo returns mostly node_modules.
func List(ctx context.Context, primary string, repos []string) ([]File, error) {
	var out []File

	for _, repo := range repos {
		paths, err := list(ctx, repo)
		if err != nil {
			return nil, err
		}

		for _, p := range paths {
			rel, err := filepath.Rel(primary, p)
			if err != nil {
				continue
			}
			out = append(out, File{Rel: filepath.ToSlash(rel), Base: filepath.Base(p)})
		}
	}

	slices.SortFunc(out, func(a, b File) int { return strings.Compare(a.Rel, b.Rel) })
	return out, nil
}

func list(ctx context.Context, dir string) ([]string, error) {
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
		return tracked(ctx, dir)
	}
	return walk(dir)
}

// tracked asks git, so ignored files never appear.
func tracked(ctx context.Context, dir string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "ls-files", "-z")

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list files in %s: %w", dir, err)
	}

	var paths []string
	for _, name := range bytes.Split(out, []byte{0}) {
		if len(name) == 0 {
			continue
		}
		paths = append(paths, filepath.Join(dir, string(name)))
	}
	return paths, nil
}

// walk covers a directory git does not track. Dotted directories and
// node_modules are skipped, and the result is capped.
func walk(dir string) ([]string, error) {
	var paths []string

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if len(paths) >= walkCap {
			return filepath.SkipAll
		}

		name := d.Name()
		if d.IsDir() {
			if path != dir && (strings.HasPrefix(name, ".") || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}

		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, err)
	}
	return paths, nil
}

// Match returns files matching query as a subsequence, best first.
//
// Subsequence rather than substring so "glweb" finds gl-webapp, which is how
// people type paths they half remember.
func Match(in []File, query string, limit int) []File {
	if query == "" {
		if len(in) > limit {
			return in[:limit]
		}
		return in
	}

	q := strings.ToLower(query)

	type scored struct {
		file  File
		score int
	}

	var hits []scored
	for _, f := range in {
		s, ok := score(f, q)
		if !ok {
			continue
		}
		hits = append(hits, scored{f, s})
	}

	slices.SortStableFunc(hits, func(a, b scored) int {
		if a.score != b.score {
			return a.score - b.score
		}
		return len(a.file.Rel) - len(b.file.Rel)
	})

	out := make([]File, 0, min(limit, len(hits)))
	for i, h := range hits {
		if i >= limit {
			break
		}
		out = append(out, h.file)
	}
	return out
}

// score is lower for better matches: a hit in the file name beats one in the
// directories above it, and a tighter run of characters beats a scattered one.
func score(f File, q string) (int, bool) {
	span, ok := subsequence(strings.ToLower(f.Rel), q)
	if !ok {
		return 0, false
	}

	if baseSpan, ok := subsequence(strings.ToLower(f.Base), q); ok {
		return baseSpan, true
	}
	return span + 1000, true
}

// subsequence reports whether q appears in s in order, and how much of s the
// match spanned.
func subsequence(s, q string) (int, bool) {
	if q == "" {
		return 0, true
	}

	first, last, qi := -1, -1, 0
	for i := 0; i < len(s) && qi < len(q); i++ {
		if s[i] == q[qi] {
			if first < 0 {
				first = i
			}
			last = i
			qi++
		}
	}

	if qi < len(q) {
		return 0, false
	}
	return last - first, true
}

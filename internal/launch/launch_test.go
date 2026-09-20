package launch_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"scopr/internal/launch"
	"scopr/internal/scope"
)

func fixture(t *testing.T) string {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving temp dir: %v", err)
	}
	for _, d := range []string{"apps/web/.git", "apps/shared/.git", "services/api/.git"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	return root
}

func resolve(t *testing.T, names ...string) scope.Scope {
	t.Helper()

	s, err := scope.Resolve(fixture(t), names)
	if err != nil {
		t.Fatalf("Resolve(%v): %v", names, err)
	}
	return s
}

func args(t *testing.T, cfg launch.Config) []string {
	t.Helper()

	got, err := launch.Args(cfg)
	if err != nil {
		t.Fatalf("Args: %v", err)
	}
	return got
}

func TestPromptPrecedesVariadicFlags(t *testing.T) {
	got := args(t, launch.Config{Scope: resolve(t, "api", "shared"), Prompt: "find the retry logic"})

	prompt := slices.Index(got, "find the retry logic")
	addDir := slices.Index(got, "--add-dir")

	if prompt == -1 {
		t.Fatalf("prompt missing from %v", got)
	}
	if addDir == -1 {
		t.Fatalf("--add-dir missing from %v", got)
	}
	if prompt > addDir {
		t.Errorf("prompt at %d is after --add-dir at %d: %v", prompt, addDir, got)
	}
}

func TestOmitsPromptWhenEmpty(t *testing.T) {
	got := args(t, launch.Config{Scope: resolve(t, "api")})

	if slices.Contains(got, "") {
		t.Errorf("argv contains an empty string: %q", got)
	}
}

func TestSingleRepoHasNoAddDir(t *testing.T) {
	got := args(t, launch.Config{Scope: resolve(t, "api")})

	if slices.Contains(got, "--add-dir") {
		t.Errorf("single-repo scope emitted --add-dir: %v", got)
	}
}

func TestAddDirCoversEveryOtherRepo(t *testing.T) {
	s := resolve(t, "api", "shared", "web")
	got := args(t, launch.Config{Scope: s})

	for _, r := range s.Others() {
		if !slices.Contains(got, r.Path) {
			t.Errorf("--add-dir missing %q: %v", r.Path, got)
		}
	}
}

func TestAddDirExcludesPrimary(t *testing.T) {
	s := resolve(t, "api", "shared")
	got := args(t, launch.Config{Scope: s})

	addDir := slices.Index(got, "--add-dir")
	if addDir == -1 {
		t.Fatalf("--add-dir missing from %v", got)
	}

	for _, a := range got[addDir+1:] {
		if a == s.Primary().Path {
			t.Errorf("primary %q passed to --add-dir: %v", s.Primary().Path, got)
		}
	}
}

func TestAlwaysDisablesSnapshot(t *testing.T) {
	for _, names := range [][]string{{"api"}, {"api", "shared"}} {
		got := args(t, launch.Config{Scope: resolve(t, names...)})

		i := slices.Index(got, "--system-prompt-snapshot")
		if i == -1 || i+1 >= len(got) || got[i+1] != "off" {
			t.Errorf("scope %v: snapshot not disabled: %v", names, got)
		}
	}
}

func TestCarriesDeclarationAndAgents(t *testing.T) {
	s := resolve(t, "api", "shared")
	got := args(t, launch.Config{Scope: s})

	for _, flag := range []string{"--append-system-prompt", "--agents"} {
		i := slices.Index(got, flag)
		if i == -1 {
			t.Fatalf("%s missing from %v", flag, got)
		}
		if i+1 >= len(got) || got[i+1] == "" {
			t.Fatalf("%s has no value: %v", flag, got)
		}
	}

	decl := got[slices.Index(got, "--append-system-prompt")+1]
	for _, r := range s.Repos {
		if !strings.Contains(decl, r.Path) {
			t.Errorf("declaration missing %q", r.Path)
		}
	}

	agents := got[slices.Index(got, "--agents")+1]
	if !strings.Contains(agents, s.Primary().Path) {
		t.Errorf("agents JSON missing primary %q", s.Primary().Path)
	}
}

func TestEmptyScopeErrors(t *testing.T) {
	if _, err := launch.Args(launch.Config{}); err == nil {
		t.Fatal("Args on an empty scope returned no error")
	}
}

package files

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func repo(t *testing.T, dir string, paths ...string) {
	t.Helper()

	for _, p := range append(paths, "node_modules/pkg/index.js") {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func fixture(t *testing.T) (root, primary string) {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving temp dir: %v", err)
	}

	primary = filepath.Join(root, "apps", "web")
	repo(t, primary, "src/cart.ts", "src/checkout.ts")
	repo(t, filepath.Join(root, "services", "api"), "api/checkout.py")

	return root, primary
}

func rels(in []File) []string {
	out := make([]string, 0, len(in))
	for _, f := range in {
		out = append(out, f.Rel)
	}
	return out
}

func TestListsTrackedFiles(t *testing.T) {
	root, primary := fixture(t)

	got, err := List(context.Background(), primary,
		[]string{primary, filepath.Join(root, "services", "api")})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, want := range []string{"src/cart.ts", "src/checkout.ts"} {
		if !slices.Contains(rels(got), want) {
			t.Errorf("missing %q: %v", want, rels(got))
		}
	}
}

func TestSkipsIgnoredFiles(t *testing.T) {
	root, primary := fixture(t)

	got, err := List(context.Background(), primary,
		[]string{primary, filepath.Join(root, "services", "api")})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, r := range rels(got) {
		if strings.Contains(r, "node_modules") {
			t.Errorf("node_modules leaked into the list: %q", r)
		}
	}
}

func TestPathsAreRelativeToPrimary(t *testing.T) {
	root, primary := fixture(t)

	got, err := List(context.Background(), primary,
		[]string{primary, filepath.Join(root, "services", "api")})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	want := "../../services/api/api/checkout.py"
	if !slices.Contains(rels(got), want) {
		t.Errorf("missing %q: %v", want, rels(got))
	}
}

func TestWalksNonGitDirectories(t *testing.T) {
	root, primary := fixture(t)

	docs := filepath.Join(root, "docs")
	for _, p := range []string{"guide.md", "node_modules/junk.js", ".hidden/secret.md"} {
		full := filepath.Join(docs, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	got, err := List(context.Background(), primary, []string{docs})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if !slices.Contains(rels(got), "../../docs/guide.md") {
		t.Errorf("walk missed the real file: %v", rels(got))
	}
	for _, r := range rels(got) {
		if strings.Contains(r, "node_modules") || strings.Contains(r, ".hidden") {
			t.Errorf("walk returned %q, which should be skipped", r)
		}
	}
}

func TestSortedByPath(t *testing.T) {
	root, primary := fixture(t)
	repos := []string{primary, filepath.Join(root, "services", "api")}

	first, err := List(context.Background(), primary, repos)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	second, _ := List(context.Background(), primary, repos)

	if !slices.Equal(rels(first), rels(second)) {
		t.Errorf("order unstable:\n%v\n%v", rels(first), rels(second))
	}
}

func sample() []File {
	return []File{
		{Rel: "src/cart.ts", Base: "cart.ts"},
		{Rel: "src/checkout.ts", Base: "checkout.ts"},
		{Rel: "src/components/CheckoutButton.tsx", Base: "CheckoutButton.tsx"},
		{Rel: "../services/api/api/checkout.py", Base: "checkout.py"},
	}
}

func TestMatchEmptyQueryReturnsAll(t *testing.T) {
	if got := Match(sample(), "", 10); len(got) != 4 {
		t.Errorf("got %d, want all four", len(got))
	}
}

func TestMatchRespectsLimit(t *testing.T) {
	if got := Match(sample(), "", 2); len(got) != 2 {
		t.Errorf("got %d, want 2", len(got))
	}
	if got := Match(sample(), "checkout", 1); len(got) != 1 {
		t.Errorf("got %d, want 1", len(got))
	}
}

func TestMatchIsSubsequence(t *testing.T) {
	got := Match(sample(), "srcchk", 10)

	if len(got) == 0 {
		t.Fatal("subsequence query matched nothing")
	}
	if !strings.HasPrefix(got[0].Rel, "src/") {
		t.Errorf("best match = %q, want the tightest run under src/", got[0].Rel)
	}

	if len(Match(sample(), "cmpchkbtn", 10)) == 0 {
		t.Error("a scattered subsequence matched nothing")
	}
}

func TestMatchPrefersFileName(t *testing.T) {
	in := []File{
		{Rel: "checkout/legacy/util.ts", Base: "util.ts"},
		{Rel: "src/checkout.ts", Base: "checkout.ts"},
	}

	got := Match(in, "checkout", 10)
	if len(got) == 0 || got[0].Rel != "src/checkout.ts" {
		t.Errorf("best match = %v, want the file named checkout", rels(got))
	}
}

func TestMatchIsCaseInsensitive(t *testing.T) {
	got := Match(sample(), "checkoutbutton", 10)

	if len(got) == 0 || got[0].Base != "CheckoutButton.tsx" {
		t.Errorf("got %v, want the mixed-case file", rels(got))
	}
}

func TestMatchNoHits(t *testing.T) {
	if got := Match(sample(), "zzzzz", 10); len(got) != 0 {
		t.Errorf("got %v, want nothing", rels(got))
	}
}

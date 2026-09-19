package picker

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"scopr/internal/scopefile"
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

func saved(t *testing.T, root, name string, repos ...string) {
	t.Helper()

	if err := scopefile.Save(root, name, repos); err != nil {
		t.Fatalf("Save %s: %v", name, err)
	}
}

// script answers each prompt in turn and records what it was offered.
type script struct {
	answers [][]string
	errs    []error
	offered [][]string
	calls   int
}

func (s *script) choose(_ string, items []string, _ bool) ([]string, error) {
	s.offered = append(s.offered, items)
	i := s.calls
	s.calls++

	if i < len(s.errs) && s.errs[i] != nil {
		return nil, s.errs[i]
	}
	if i < len(s.answers) {
		return s.answers[i], nil
	}
	return nil, ErrCancelled
}

func TestPicksScopeAlone(t *testing.T) {
	root := fixture(t)
	saved(t, root, "seam", "api", "shared")

	s := &script{answers: [][]string{{"@seam\tapi shared"}}}

	got, err := pick(root, s.choose)
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if want := []string{"@seam"}; !slices.Equal(got, want) {
		t.Errorf("pick = %v, want %v", got, want)
	}
	if s.calls != 1 {
		t.Errorf("asked %d times; a scope carries its own order and needs one question", s.calls)
	}
}

func TestPicksPrimaryThenOthers(t *testing.T) {
	root := fixture(t)

	s := &script{answers: [][]string{
		{"services/api"},
		{"apps/shared", "apps/web"},
	}}

	got, err := pick(root, s.choose)
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if want := []string{"services/api", "apps/shared", "apps/web"}; !slices.Equal(got, want) {
		t.Errorf("pick = %v, want %v", got, want)
	}
}

// The primary must come first whatever order the second prompt returns.
func TestPrimaryStaysFirst(t *testing.T) {
	root := fixture(t)

	s := &script{answers: [][]string{
		{"apps/web"},
		{"apps/shared", "services/api"},
	}}

	got, err := pick(root, s.choose)
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if got[0] != "apps/web" {
		t.Errorf("pick = %v, want apps/web first", got)
	}
}

// Declining the second prompt is a single-repo scope, not a cancellation.
func TestDecliningExtrasGivesSingleRepo(t *testing.T) {
	root := fixture(t)

	s := &script{
		answers: [][]string{{"services/api"}},
		errs:    []error{nil, ErrCancelled},
	}

	got, err := pick(root, s.choose)
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if want := []string{"services/api"}; !slices.Equal(got, want) {
		t.Errorf("pick = %v, want %v", got, want)
	}
}

func TestCancellingFirstPromptCancels(t *testing.T) {
	root := fixture(t)

	s := &script{errs: []error{ErrCancelled}}

	if _, err := pick(root, s.choose); !errors.Is(err, ErrCancelled) {
		t.Fatalf("pick error = %v, want ErrCancelled", err)
	}
}

func TestOffersScopesAndRepos(t *testing.T) {
	root := fixture(t)
	saved(t, root, "seam", "api", "shared")

	s := &script{answers: [][]string{{"services/api"}, nil}}

	if _, err := pick(root, s.choose); err != nil {
		t.Fatalf("pick: %v", err)
	}

	first := s.offered[0]
	if !slices.ContainsFunc(first, func(i string) bool { return strings.HasPrefix(i, "@seam\t") }) {
		t.Errorf("first prompt did not offer @seam: %v", first)
	}
	for _, want := range []string{"apps/web", "apps/shared", "services/api"} {
		if !slices.Contains(first, want) {
			t.Errorf("first prompt did not offer %q: %v", want, first)
		}
	}
}

// Offering the primary again would let it be added to its own --add-dir list.
func TestSecondPromptExcludesPrimary(t *testing.T) {
	root := fixture(t)

	s := &script{answers: [][]string{{"services/api"}, nil}}

	if _, err := pick(root, s.choose); err != nil {
		t.Fatalf("pick: %v", err)
	}
	if len(s.offered) < 2 {
		t.Fatalf("second prompt never ran: %v", s.offered)
	}
	if slices.Contains(s.offered[1], "services/api") {
		t.Errorf("second prompt offered the primary again: %v", s.offered[1])
	}
}

// The description after a tab is display only.
func TestStripsDescription(t *testing.T) {
	root := fixture(t)
	saved(t, root, "seam", "api")

	s := &script{answers: [][]string{{"@seam\tapi"}}}

	got, err := pick(root, s.choose)
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if want := []string{"@seam"}; !slices.Equal(got, want) {
		t.Errorf("pick = %v, want %v", got, want)
	}
}

func TestEmptyWorkspaceErrors(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving temp dir: %v", err)
	}

	s := &script{}
	if _, err := pick(root, s.choose); err == nil {
		t.Fatal("pick on an empty workspace returned no error")
	}
}

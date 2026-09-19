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
	prompts []Prompt
	calls   int
}

func (s *script) choose(p Prompt) ([]string, error) {
	s.offered = append(s.offered, p.Items)
	s.prompts = append(s.prompts, p)
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

func TestTrimKeepsMarked(t *testing.T) {
	s := &script{answers: [][]string{{"services/api", "apps/web"}}}

	got, err := trim([]string{"services/api", "apps/web", "apps/shared"}, s.choose)
	if err != nil {
		t.Fatalf("trim: %v", err)
	}
	if want := []string{"services/api", "apps/web"}; !slices.Equal(got, want) {
		t.Errorf("trim = %v, want %v", got, want)
	}
}

// Everything arrives marked, so answering means dropping rather than picking.
func TestTrimPreselectsEverything(t *testing.T) {
	s := &script{answers: [][]string{{"services/api"}}}

	if _, err := trim([]string{"services/api", "apps/web"}, s.choose); err != nil {
		t.Fatalf("trim: %v", err)
	}
	if len(s.prompts) != 1 {
		t.Fatalf("asked %d times, want 1", len(s.prompts))
	}
	if !s.prompts[0].Preselect {
		t.Error("trim did not preselect; the person would have to re-mark everything")
	}
	if !s.prompts[0].Multi {
		t.Error("trim is not multi-select")
	}
}

func TestTrimOffersOnlyTheGivenNames(t *testing.T) {
	s := &script{answers: [][]string{{"services/api"}}}
	names := []string{"services/api", "apps/web"}

	if _, err := trim(names, s.choose); err != nil {
		t.Fatalf("trim: %v", err)
	}
	if !slices.Equal(s.offered[0], names) {
		t.Errorf("offered %v, want exactly %v", s.offered[0], names)
	}
}

func TestTrimUnmarkingEverythingCancels(t *testing.T) {
	s := &script{answers: [][]string{{}}}

	if _, err := trim([]string{"services/api"}, s.choose); !errors.Is(err, ErrCancelled) {
		t.Fatalf("trim error = %v, want ErrCancelled", err)
	}
}

func TestTrimEmptyInputCancels(t *testing.T) {
	s := &script{}

	if _, err := trim(nil, s.choose); !errors.Is(err, ErrCancelled) {
		t.Fatalf("trim error = %v, want ErrCancelled", err)
	}
	if s.calls != 0 {
		t.Error("trim asked with nothing to offer")
	}
}

// Order is the suggestion order, since the first kept repo becomes primary.
func TestTrimPreservesOrder(t *testing.T) {
	s := &script{answers: [][]string{{"apps/web", "services/api"}}}

	got, err := trim([]string{"apps/web", "services/api"}, s.choose)
	if err != nil {
		t.Fatalf("trim: %v", err)
	}
	if got[0] != "apps/web" {
		t.Errorf("trim = %v, want apps/web first", got)
	}
}

// Preselect must reach fzf as a bind, not merely be set on the Prompt.
func TestFzfArgsPreselect(t *testing.T) {
	got := fzfArgs(Prompt{Label: "x> ", Multi: true, Preselect: true})

	i := slices.Index(got, "--bind")
	if i == -1 || i+1 >= len(got) || got[i+1] != "start:select-all" {
		t.Errorf("preselect did not become a bind: %v", got)
	}
}

func TestFzfArgsSingleSelect(t *testing.T) {
	got := fzfArgs(Prompt{Label: "x> "})

	for _, unwanted := range []string{"--multi", "--bind"} {
		if slices.Contains(got, unwanted) {
			t.Errorf("single-select prompt passed %s: %v", unwanted, got)
		}
	}
}

func TestFzfArgsMultiWithoutPreselect(t *testing.T) {
	got := fzfArgs(Prompt{Label: "x> ", Multi: true})

	if !slices.Contains(got, "--multi") {
		t.Errorf("multi prompt did not pass --multi: %v", got)
	}
	if slices.Contains(got, "--bind") {
		t.Errorf("multi prompt preselected without being asked: %v", got)
	}
}

package picker

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"scopr/internal/scopefile"
	"scopr/internal/ui"
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

type spy struct {
	chosen    []string
	available []string
	answer    []string
	err       error
	model     ui.Model
	calls     int
}

func (s *spy) edit(m ui.Model) ([]string, error) {
	s.chosen, s.available = m.Chosen, m.Available
	s.model = m
	s.calls++
	return s.answer, s.err
}

func TestOffersEveryRepo(t *testing.T) {
	root := fixture(t)
	s := &spy{answer: []string{"services/api"}}

	if _, err := pick(root, Scope{}, s.edit); err != nil {
		t.Fatalf("pick: %v", err)
	}
	for _, want := range []string{"apps/web", "apps/shared", "services/api"} {
		if !slices.Contains(s.available, want) {
			t.Errorf("editor was not offered %q: %v", want, s.available)
		}
	}
}

func TestOpensEmptyWhenNothingChosen(t *testing.T) {
	root := fixture(t)
	s := &spy{answer: []string{"services/api"}}

	if _, err := pick(root, Scope{}, s.edit); err != nil {
		t.Fatalf("pick: %v", err)
	}
	if len(s.chosen) != 0 {
		t.Errorf("editor opened with %v, want nothing chosen", s.chosen)
	}
}

func TestOpensOnGivenScope(t *testing.T) {
	root := fixture(t)
	s := &spy{answer: []string{"services/api"}}

	in := []string{"services/api", "apps/web"}
	if _, err := pick(root, Scope{Names: in}, s.edit); err != nil {
		t.Fatalf("pick: %v", err)
	}
	if !slices.Equal(s.chosen, in) {
		t.Errorf("editor opened with %v, want %v", s.chosen, in)
	}
}

func TestExpandsScopeBeforeEditing(t *testing.T) {
	root := fixture(t)
	if err := scopefile.Save(root, "seam", []string{"services/api", "apps/web"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	s := &spy{answer: []string{"services/api"}}

	if _, err := pick(root, Scope{Names: []string{"@seam"}}, s.edit); err != nil {
		t.Fatalf("pick: %v", err)
	}
	if want := []string{"services/api", "apps/web"}; !slices.Equal(s.chosen, want) {
		t.Errorf("editor opened with %v, want %v", s.chosen, want)
	}
}

func TestUnknownScopeErrors(t *testing.T) {
	root := fixture(t)
	s := &spy{}

	if _, err := pick(root, Scope{Names: []string{"@nope"}}, s.edit); !errors.Is(err, scopefile.ErrNoSuchScope) {
		t.Fatalf("pick error = %v, want ErrNoSuchScope", err)
	}
	if s.calls != 0 {
		t.Error("editor opened despite an unresolvable scope")
	}
}

func TestPassesResultThrough(t *testing.T) {
	root := fixture(t)
	want := []string{"apps/web", "services/api"}
	s := &spy{answer: want}

	got, err := pick(root, Scope{}, s.edit)
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("pick = %v, want %v", got, want)
	}
}

func TestCancellationPropagates(t *testing.T) {
	root := fixture(t)
	s := &spy{err: ErrCancelled}

	if _, err := pick(root, Scope{}, s.edit); !errors.Is(err, ErrCancelled) {
		t.Fatalf("pick error = %v, want ErrCancelled", err)
	}
}

func TestEmptyWorkspaceErrors(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving temp dir: %v", err)
	}
	s := &spy{}

	if _, err := pick(root, Scope{}, s.edit); err == nil {
		t.Fatal("pick on an empty workspace returned no error")
	}
	if s.calls != 0 {
		t.Error("editor opened with nothing to offer")
	}
}

func TestCarriesNotesAndHeader(t *testing.T) {
	root := fixture(t)
	s := &spy{answer: []string{"services/api"}}

	in := Scope{
		Names:  []string{"services/api"},
		Notes:  map[string]string{"services/api": "owns the checkout endpoint"},
		Header: "suggested for: trace the checkout call",
	}
	if _, err := pick(root, in, s.edit); err != nil {
		t.Fatalf("pick: %v", err)
	}

	if got := s.model.Notes["services/api"]; got != "owns the checkout endpoint" {
		t.Errorf("note = %q, want the reason", got)
	}
	if s.model.Header != in.Header {
		t.Errorf("header = %q, want %q", s.model.Header, in.Header)
	}
}

package scope_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scopr/internal/scope"
)

func TestDeclarationNamesPrimary(t *testing.T) {
	root := fixture(t)

	s, err := scope.Resolve(root, []string{"api"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	got := scope.Declaration(s)
	if !strings.Contains(got, s.Primary().Path) {
		t.Errorf("declaration does not name the primary %q:\n%s", s.Primary().Path, got)
	}
}

func TestDeclarationNamesEveryRepo(t *testing.T) {
	root := fixture(t)

	s, err := scope.Resolve(root, []string{"api", "shared"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	got := scope.Declaration(s)
	for _, r := range s.Repos {
		if !strings.Contains(got, r.Path) {
			t.Errorf("declaration missing %q:\n%s", r.Path, got)
		}
	}
}

func TestDeclarationOmitsOutOfScope(t *testing.T) {
	root := fixture(t)

	s, err := scope.Resolve(root, []string{"api"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	got := scope.Declaration(s)
	if out := filepath.Join(root, "apps", "shared"); strings.Contains(got, out) {
		t.Errorf("declaration names out-of-scope %q:\n%s", out, got)
	}
}

func TestDeclarationOmitsCdAdviceForSingleRepo(t *testing.T) {
	root := fixture(t)

	s, err := scope.Resolve(root, []string{"api"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if got := scope.Declaration(s); strings.Contains(got, "cd into it") {
		t.Errorf("single-repo declaration should not advise cd:\n%s", got)
	}
}

func TestAgentsJSONIsValidJSON(t *testing.T) {
	root := fixture(t)

	s, err := scope.Resolve(root, []string{"api", "shared"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	raw, err := scope.AgentsJSON(s)
	if err != nil {
		t.Fatalf("AgentsJSON: %v", err)
	}

	var parsed map[string]map[string]string
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("unmarshal %q: %v", raw, err)
	}
	if _, ok := parsed[scope.AgentName]; !ok {
		t.Errorf("agents JSON has no %q key: %v", scope.AgentName, parsed)
	}
}

func TestAgentsJSONCarriesScopeText(t *testing.T) {
	root := fixture(t)

	s, err := scope.Resolve(root, []string{"api", "shared"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	raw, err := scope.AgentsJSON(s)
	if err != nil {
		t.Fatalf("AgentsJSON: %v", err)
	}

	var parsed map[string]map[string]string
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	prompt := parsed[scope.AgentName]["prompt"]
	for _, r := range s.Repos {
		if !strings.Contains(prompt, r.Path) {
			t.Errorf("agent prompt missing %q:\n%s", r.Path, prompt)
		}
	}
}

func TestHandlesPathsNeedingEscaping(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving temp dir: %v", err)
	}

	odd := `we"ird\repo`
	if err := os.MkdirAll(filepath.Join(root, odd, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	s, err := scope.Resolve(root, []string{odd})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	raw, err := scope.AgentsJSON(s)
	if err != nil {
		t.Fatalf("AgentsJSON: %v", err)
	}

	var parsed map[string]map[string]string
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("unmarshal %q: %v", raw, err)
	}
	if !strings.Contains(parsed[scope.AgentName]["prompt"], s.Primary().Path) {
		t.Errorf("escaped path lost in round trip: %v", parsed)
	}
}

func TestDeclarationDoesNotListPrimaryAsAdditional(t *testing.T) {
	root := fixture(t)

	s, err := scope.Resolve(root, []string{"api", "shared"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	got := scope.Declaration(s)
	if bullet := "- " + s.Primary().Path + "\n"; strings.Contains(got, bullet) {
		t.Errorf("primary listed as an additional repo:\n%s", got)
	}
}

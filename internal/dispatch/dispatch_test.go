package dispatch

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"scopr/internal/repo"
)

func fixture(t *testing.T) string {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving temp dir: %v", err)
	}
	for _, d := range []string{"apps/web/.git", "services/api/.git"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	return root
}

// replies returns a runner that answers with the given stdout and error.
func replies(out string, err error) runner {
	return func(context.Context, Config, []string) ([]byte, error) {
		return []byte(out), err
	}
}

func inferWith(t *testing.T, out string, err error) ([]Suggestion, error) {
	t.Helper()

	return infer(context.Background(), Config{Root: fixture(t), Task: "trace the checkout call"}, replies(out, err))
}

func TestParsesStructuredOutput(t *testing.T) {
	env := `{"subtype":"success","is_error":false,"result":"...",
	  "structured_output":{"repos":[
	    {"name":"services/api","reason":"owns the checkout endpoint"},
	    {"name":"apps/web","reason":"calls it from the cart page"}]}}`

	got, err := inferWith(t, env, nil)
	if err != nil {
		t.Fatalf("infer: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d suggestions, want 2: %v", len(got), got)
	}
	if got[0].Name != "services/api" || got[0].Reason == "" {
		t.Errorf("first suggestion = %+v", got[0])
	}
}

// The documented trap: a decline is reported as success with the key omitted.
// Reading is_error would report everything fine while holding no data.
func TestDeclinedIsNotSuccess(t *testing.T) {
	env := `{"subtype":"success","is_error":false,
	  "result":"I need more information about the repositories."}`

	got, err := inferWith(t, env, nil)
	if !errors.Is(err, ErrDeclined) {
		t.Fatalf("infer error = %v, want ErrDeclined", err)
	}
	if got != nil {
		t.Errorf("got %v, want no suggestions", got)
	}
	if !strings.Contains(err.Error(), "more information") {
		t.Errorf("error %q drops the model's explanation", err)
	}
}

func TestEmptyReposIsDeclined(t *testing.T) {
	env := `{"subtype":"success","is_error":false,"structured_output":{"repos":[]}}`

	if _, err := inferWith(t, env, nil); !errors.Is(err, ErrDeclined) {
		t.Fatalf("infer error = %v, want ErrDeclined", err)
	}
}

// --json-schema is documented but not guaranteed, so a fenced result is still
// recovered.
func TestStripsCodeFences(t *testing.T) {
	env := `{"subtype":"success","is_error":false,
	  "result":"` + "```json\\n{\\\"repos\\\":[{\\\"name\\\":\\\"apps/web\\\",\\\"reason\\\":\\\"the cart page\\\"}]}\\n```" + `"}`

	got, err := inferWith(t, env, nil)
	if err != nil {
		t.Fatalf("infer: %v", err)
	}
	if len(got) != 1 || got[0].Name != "apps/web" {
		t.Errorf("got %v, want one suggestion for apps/web", got)
	}
}

func TestNonZeroExitErrors(t *testing.T) {
	if _, err := inferWith(t, "", errors.New("exit status 1")); err == nil {
		t.Fatal("infer returned no error")
	}
}

// Unparseable output is a failure, not a decline: a decline falls through to
// the picker, whereas garbage means something is broken.
func TestGarbageStdoutErrors(t *testing.T) {
	got, err := inferWith(t, "not json at all", nil)
	if err == nil {
		t.Fatal("infer returned no error")
	}
	if errors.Is(err, ErrDeclined) {
		t.Errorf("garbage reported as a decline: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want no suggestions", got)
	}
}

func TestEmptyTaskErrors(t *testing.T) {
	_, err := infer(context.Background(), Config{Root: fixture(t), Task: "  "}, replies("", nil))
	if err == nil {
		t.Fatal("infer on an empty task returned no error")
	}
}

func TestArgsCarryRequiredFlags(t *testing.T) {
	root := fixture(t)
	repos, err := repo.List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	got := Args(Config{Root: root, Task: "x"}, repos)

	for _, pair := range [][2]string{
		{"--output-format", "json"},
		{"--model", DefaultModel},
		{"--tools", "Grep,Glob,Read"},
		{"--permission-prompts", "none"},
	} {
		i := slices.Index(got, pair[0])
		if i == -1 || i+1 >= len(got) || got[i+1] != pair[1] {
			t.Errorf("%s %s missing from %v", pair[0], pair[1], got)
		}
	}

	for _, flag := range []string{"--json-schema", "--safe-mode", "--no-session-persistence"} {
		if !slices.Contains(got, flag) {
			t.Errorf("%s missing from %v", flag, got)
		}
	}

	i := slices.Index(got, "--json-schema")
	if !strings.Contains(got[i+1], `"repos"`) {
		t.Errorf("schema does not describe repos: %s", got[i+1])
	}
}

// Demanding raw JSON in a system prompt measured 0/10 against 18/18 for
// --json-schema alone. This test exists so nobody adds it back as a fix.
func TestArgsOmitAppendSystemPrompt(t *testing.T) {
	root := fixture(t)
	repos, _ := repo.List(root)

	if slices.Contains(Args(Config{Root: root, Task: "x"}, repos), "--append-system-prompt") {
		t.Error("--append-system-prompt must not be passed to the survey")
	}
}

func TestEnvDisablesAmbientClaudeMd(t *testing.T) {
	if !slices.Contains(Env(), "CLAUDE_CODE_DISABLE_CLAUDE_MDS=1") {
		t.Error("survey environment does not disable ambient CLAUDE.md")
	}
}

func TestPromptListsReposAndTask(t *testing.T) {
	root := fixture(t)
	repos, _ := repo.List(root)

	got := prompt(Config{Root: root, Task: "trace the checkout call"}, repos)

	for _, want := range []string{"apps/web", "services/api", "trace the checkout call"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "Prefer including") {
		t.Errorf("prompt does not ask for over-suggestion:\n%s", got)
	}
}

func TestContextCancellationPropagates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	slow := func(ctx context.Context, _ Config, _ []string) ([]byte, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			return nil, errors.New("runner was not cancelled")
		}
	}

	_, err := infer(ctx, Config{Root: fixture(t), Task: "x"}, slow)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("infer error = %v, want context.Canceled", err)
	}
}

func TestArgsWithoutTraceUseJSON(t *testing.T) {
	root := fixture(t)
	repos, _ := repo.List(root)

	got := Args(Config{Root: root, Task: "x"}, repos)

	i := slices.Index(got, "--output-format")
	if i == -1 || got[i+1] != "json" {
		t.Errorf("want --output-format json, got %v", got)
	}
	if slices.Contains(got, "--verbose") {
		t.Errorf("--verbose passed without tracing: %v", got)
	}
}

// stream-json requires --verbose in print mode; without it the CLI refuses.
func TestArgsWithTraceStream(t *testing.T) {
	root := fixture(t)
	repos, _ := repo.List(root)

	got := Args(Config{Root: root, Task: "x", Trace: io.Discard}, repos)

	i := slices.Index(got, "--output-format")
	if i == -1 || got[i+1] != "stream-json" {
		t.Errorf("want --output-format stream-json, got %v", got)
	}
	if !slices.Contains(got, "--verbose") {
		t.Errorf("stream-json without --verbose will be refused: %v", got)
	}
}

// The result line of a stream is identical to the single-object envelope, so
// only the framing differs.
func TestParsesResultFromStream(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Grep","input":{"pattern":"checkout"}}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"structured_output":{"repos":[{"name":"services/api","reason":"owns it"}]}}`,
	}, "\n")

	got, err := inferWith(t, stream, nil)
	if err != nil {
		t.Fatalf("infer: %v", err)
	}
	if len(got) != 1 || got[0].Name != "services/api" {
		t.Errorf("got %v, want one suggestion for services/api", got)
	}
}

func TestStreamWithoutResultErrors(t *testing.T) {
	stream := `{"type":"system","subtype":"init"}` + "\n" + `{"type":"assistant","message":{"content":[]}}`

	if _, err := inferWith(t, stream, nil); err == nil {
		t.Fatal("a stream with no result returned no error")
	}
}

func TestRenderShowsToolCallsAndText(t *testing.T) {
	var b strings.Builder

	render(&b, []byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Grep","input":{"pattern":"checkout","path":"services"}}]}}`))
	render(&b, []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"  found it in the api  "}]}}`))
	render(&b, []byte(`{"type":"system","subtype":"api_retry"}`))

	got := b.String()
	for _, want := range []string{"Grep", "checkout", "found it in the api", "retrying"} {
		if !strings.Contains(got, want) {
			t.Errorf("render dropped %q:\n%s", want, got)
		}
	}
}

func TestRenderIgnoresNoise(t *testing.T) {
	var b strings.Builder

	render(&b, []byte(`{"type":"system","subtype":"status"}`))
	render(&b, []byte(`{"type":"user","message":{"content":[]}}`))
	render(&b, []byte(`not json`))

	if b.Len() != 0 {
		t.Errorf("render emitted noise:\n%s", b.String())
	}
}

// Tracing changes what is shown, not what is read.
func TestTraceDoesNotChangeResult(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"looking"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"structured_output":{"repos":[{"name":"apps/web","reason":"x"}]}}`,
	}, "\n")

	var b strings.Builder
	got, err := infer(context.Background(),
		Config{Root: fixture(t), Task: "x", Trace: &b},
		replies(stream, nil))
	if err != nil {
		t.Fatalf("infer: %v", err)
	}
	if len(got) != 1 || got[0].Name != "apps/web" {
		t.Errorf("got %v, want one suggestion for apps/web", got)
	}
}

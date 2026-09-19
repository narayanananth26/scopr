package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"scopr/internal/repo"
)

const (
	DefaultBin   = "claude"
	DefaultModel = "sonnet"

	// schema must stay flat. Only the flat shape has been tested; nested
	// objects, enums and oneOf may degrade.
	schema = `{"type":"object","properties":{"repos":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"reason":{"type":"string"}},"required":["name","reason"],"additionalProperties":false}}},"required":["repos"],"additionalProperties":false}`
)

// ErrDeclined reports that the survey ran but produced no repositories. The
// CLI calls this success, so it has to be detected rather than trusted.
var ErrDeclined = errors.New("no repositories suggested")

type Suggestion struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type Config struct {
	Root  string
	Task  string
	Bin   string
	Model string
}

func (c Config) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return DefaultBin
}

func (c Config) model() string {
	if c.Model != "" {
		return c.Model
	}
	return DefaultModel
}

// runner executes the survey and returns its stdout. Injected so parsing is
// tested without spawning anything.
type runner func(ctx context.Context, cfg Config, args []string) ([]byte, error)

// Infer surveys the workspace and suggests the repositories a task touches.
func Infer(ctx context.Context, cfg Config) ([]Suggestion, error) {
	return infer(ctx, cfg, run)
}

func infer(ctx context.Context, cfg Config, exec runner) ([]Suggestion, error) {
	if strings.TrimSpace(cfg.Task) == "" {
		return nil, errors.New("dispatch: empty task")
	}

	repos, err := repo.List(cfg.Root)
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return nil, errors.New("dispatch: no repositories in this workspace")
	}

	out, err := exec(ctx, cfg, Args(cfg, repos))
	if err != nil {
		return nil, err
	}
	return parse(out)
}

// Args builds the claude argv.
//
// --tools is narrow but not empty: the survey has to read the workspace, and
// every tool definition it will not use costs prompt tokens.
func Args(cfg Config, repos []repo.Repo) []string {
	return []string{
		"-p", prompt(cfg, repos),
		"--output-format", "json",
		"--json-schema", schema,
		"--model", cfg.model(),
		"--tools", "Grep,Glob,Read",
		"--safe-mode",
		"--no-session-persistence",
		"--permission-prompts", "none",
		"--setting-sources", "",
	}
}

// Env is the environment for the survey. Without CLAUDE_CODE_DISABLE_CLAUDE_MDS
// an ambient CLAUDE.md in the workspace contaminates the answer.
func Env() []string {
	return append(os.Environ(), "CLAUDE_CODE_DISABLE_CLAUDE_MDS=1")
}

func prompt(cfg Config, repos []repo.Repo) string {
	var b strings.Builder

	b.WriteString("You are choosing which repositories a task touches.\n\n")
	fmt.Fprintf(&b, "Workspace root: %s\n\n", cfg.Root)

	b.WriteString("Repositories available, named as they must be reported:\n\n")
	for _, r := range repos {
		rel := strings.TrimPrefix(strings.TrimPrefix(r.Path, cfg.Root), "/")
		if rel == "" {
			continue
		}
		fmt.Fprintf(&b, "- %s\n", rel)
	}

	fmt.Fprintf(&b, "\nTask:\n\n%s\n\n", cfg.Task)

	b.WriteString("Search the workspace to find where this work belongs. ")
	b.WriteString("Report every repository the task plausibly touches, with one line saying why.\n\n")
	b.WriteString("Prefer including a repository over leaving it out. ")
	b.WriteString("An extra repository costs almost nothing; a missing one means the work cannot be done.\n\n")
	b.WriteString("Report names exactly as listed above: a base name, or a relative path when two repositories share a base name. ")
	b.WriteString("Never report a path below a repository.\n")

	return b.String()
}

// envelope is the subset of claude's --output-format json result we read.
type envelope struct {
	StructuredOutput *struct {
		Repos []Suggestion `json:"repos"`
	} `json:"structured_output"`
	Result  string `json:"result"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
}

// parse reads the envelope.
//
// The gate is structured_output, never is_error: when the model declines to
// fill the schema the CLI still reports subtype success and is_error false,
// and simply omits the key.
func parse(out []byte) ([]Suggestion, error) {
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		return nil, fmt.Errorf("parse survey output: %w", err)
	}

	if env.StructuredOutput != nil {
		if len(env.StructuredOutput.Repos) == 0 {
			return nil, ErrDeclined
		}
		return env.StructuredOutput.Repos, nil
	}

	// Documented but not guaranteed, so fall back to the raw result.
	if repos, err := parseFenced(env.Result); err == nil && len(repos) > 0 {
		return repos, nil
	}

	if env.IsError {
		return nil, fmt.Errorf("survey failed: %s", env.Result)
	}
	return nil, fmt.Errorf("%w: %s", ErrDeclined, strings.TrimSpace(env.Result))
}

// parseFenced recovers JSON the model wrapped in a code fence.
func parseFenced(s string) ([]Suggestion, error) {
	s = strings.TrimSpace(s)
	if fence := strings.Index(s, "```"); fence >= 0 {
		s = s[fence+3:]
		if nl := strings.Index(s, "\n"); nl >= 0 {
			s = s[nl+1:]
		}
		if end := strings.Index(s, "```"); end >= 0 {
			s = s[:end]
		}
	}

	var body struct {
		Repos []Suggestion `json:"repos"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &body); err != nil {
		return nil, err
	}
	return body.Repos, nil
}

func run(ctx context.Context, cfg Config, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, cfg.bin(), args...)
	cmd.Dir = cfg.Root
	cmd.Env = Env()
	cmd.Stdin = strings.NewReader("")
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("run survey: %w", err)
	}
	return out, nil
}

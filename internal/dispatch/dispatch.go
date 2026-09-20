package dispatch

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"scopr/internal/repo"
)

const (
	DefaultBin   = "claude"
	DefaultModel = "sonnet"

	// Must stay flat: nested objects, enums and oneOf are untested.
	schema = `{"type":"object","properties":{"repos":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"reason":{"type":"string"}},"required":["name","reason"],"additionalProperties":false}}},"required":["repos"],"additionalProperties":false}`
)

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

	Trace io.Writer
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

type runner func(ctx context.Context, cfg Config, args []string) ([]byte, error)

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

func Args(cfg Config, repos []repo.Repo) []string {
	format := []string{"--output-format", "json"}
	if cfg.Trace != nil {
		// stream-json requires --verbose in print mode.
		format = []string{"--output-format", "stream-json", "--verbose"}
	}

	args := []string{
		"-p", prompt(cfg, repos),
		"--json-schema", schema,
		"--model", cfg.model(),
		"--tools", "Grep,Glob,Read",
		"--safe-mode",
		"--no-session-persistence",
		"--permission-prompts", "none",
		"--setting-sources", "",
	}
	return append(args, format...)
}

// Without this an ambient CLAUDE.md contaminates the answer.
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

type envelope struct {
	StructuredOutput *struct {
		Repos []Suggestion `json:"repos"`
	} `json:"structured_output"`
	Result  string `json:"result"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
}

type event struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Message struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	} `json:"message"`
}

func render(w io.Writer, line []byte) {
	var e event
	if err := json.Unmarshal(line, &e); err != nil {
		return
	}

	switch e.Type {
	case "system":
		if e.Subtype == "api_retry" {
			fmt.Fprintln(w, "  ... retrying")
		}

	case "assistant":
		for _, c := range e.Message.Content {
			switch c.Type {
			case "tool_use":
				fmt.Fprintf(w, "  %s %s\n", c.Name, compact(c.Input))
			case "text":
				if t := strings.TrimSpace(c.Text); t != "" {
					fmt.Fprintf(w, "  %s\n", t)
				}
			}
		}
	}
}

func compact(raw json.RawMessage) string {
	s := strings.Join(strings.Fields(string(raw)), " ")
	if len(s) > 160 {
		s = s[:160] + "..."
	}
	return s
}

func parse(out []byte) ([]Suggestion, error) {
	env, err := envelopeFrom(out)
	if err != nil {
		return nil, err
	}

	// structured_output is the gate: a declining model still reports success.
	if env.StructuredOutput != nil {
		if len(env.StructuredOutput.Repos) == 0 {
			return nil, ErrDeclined
		}
		return env.StructuredOutput.Repos, nil
	}

	if repos, err := parseFenced(env.Result); err == nil && len(repos) > 0 {
		return repos, nil
	}

	if env.IsError {
		return nil, fmt.Errorf("survey failed: %s", env.Result)
	}
	return nil, fmt.Errorf("%w: %s", ErrDeclined, strings.TrimSpace(env.Result))
}

func envelopeFrom(out []byte) (envelope, error) {
	var env envelope

	if err := json.Unmarshal(out, &env); err == nil {
		return env, nil
	}

	var found bool
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var probe struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(line), &probe) != nil || probe.Type != "result" {
			continue
		}
		if json.Unmarshal([]byte(line), &env) == nil {
			found = true
		}
	}

	if !found {
		return envelope{}, errors.New("parse survey output: no result found")
	}
	return env, nil
}

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

	if cfg.Trace == nil {
		out, err := cmd.Output()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("run survey: %w", err)
		}
		return out, nil
	}

	return stream(ctx, cmd, cfg.Trace)
}

func stream(ctx context.Context, cmd *exec.Cmd, trace io.Writer) ([]byte, error) {
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("survey stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start survey: %w", err)
	}

	var collected []byte

	sc := bufio.NewScanner(pipe)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		collected = append(collected, line...)
		collected = append(collected, '\n')
		render(trace, line)
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("run survey: %w", err)
	}
	return collected, nil
}

package launch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"scopr/internal/scope"
)

// executable launched when Config.Bin is empty.
const DefaultBin = "claude"

type Config struct {
	Scope  scope.Scope
	Prompt string
	Bin    string
}

func (c Config) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return DefaultBin
}

// Args builds the claude argv.
//
// The prompt comes first then --add-dir.
// --system-prompt-snapshot off stops a resumed session from
// replaying the scope it was born with.
func Args(cfg Config) ([]string, error) {
	if len(cfg.Scope.Repos) == 0 {
		return nil, errors.New("launch: empty scope")
	}

	declaration := scope.Declaration(cfg.Scope)

	agents, err := scope.AgentsJSON(cfg.Scope)
	if err != nil {
		return nil, err
	}

	var args []string

	if cfg.Prompt != "" {
		args = append(args, cfg.Prompt)
	}

	if others := cfg.Scope.Others(); len(others) > 0 {
		args = append(args, "--add-dir")
		for _, r := range others {
			args = append(args, r.Path)
		}
	}

	args = append(args,
		"--append-system-prompt", declaration,
		"--agents", agents,
		"--system-prompt-snapshot", "off",
	)

	return args, nil
}

// Run starts claude in the primary repo and waits, returning its exit code.
//
// It spawns rather than execs so a caller can act once the session ends.
func Run(cfg Config) (int, error) {
	args, err := Args(cfg)
	if err != nil {
		return 0, err
	}

	cmd := exec.Command(cfg.bin(), args...)
	cmd.Dir = cfg.Scope.Primary().Path
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	if err != nil {
		return 0, fmt.Errorf("run %s: %w", cfg.bin(), err)
	}

	return 0, nil
}

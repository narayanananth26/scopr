package launch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"scopr/internal/scope"
)

const DefaultBin = "claude"

type Config struct {
	Scope  scope.Scope
	Prompt string
	Bin    string

	Name string
}

func (c Config) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return DefaultBin
}

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
		// Stops a resumed session replaying the scope it was born with.
		"--system-prompt-snapshot", "off",
	)

	return args, nil
}

func Run(cfg Config) (int, error) {
	args, err := Args(cfg)
	if err != nil {
		return 0, err
	}

	if InTmux() {
		name, auto := tmuxWindow()
		_ = tmuxTitle(Title(cfg.Name, cfg.Prompt, cfg.Scope))
		defer restoreTmux(name, auto)
	}

	cmd := exec.Command(cfg.bin(), args...)
	cmd.Dir = cfg.Scope.Primary().Path
	cmd.Env = Env(cfg)
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

func Env(cfg Config) []string {
	names := make([]string, 0, len(cfg.Scope.Repos))
	for _, r := range cfg.Scope.Repos {
		names = append(names, r.Name)
	}

	return append(os.Environ(),
		"SCOPR_SCOPE="+strings.Join(names, " "),
		"SCOPR_PRIMARY="+cfg.Scope.Primary().Name,
		"SCOPR_TITLE="+Title(cfg.Name, cfg.Prompt, cfg.Scope),
	)
}

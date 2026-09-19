package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"scopr/internal/launch"
	"scopr/internal/scope"
	"scopr/internal/workspace"
)

// findRoot locates the workspace, reporting a missing one as advice rather
// than as a stat error.
func findRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("couldn't resolve current working directory: %w", err)
	}

	root, err := workspace.Find(cwd)
	if errors.Is(err, workspace.ErrNotFound) {
		return "", errors.New("not inside a .scopr workspace; run this command from within a workspace")
	}
	if err != nil {
		return "", err
	}
	return root, nil
}

// runWhere prints the workspace root. The path goes to stdout alone.
func runWhere() int {
	root, err := findRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fmt.Fprintln(os.Stdout, root)
	return 0
}

// runLaunch resolves names to a scope and starts a session in the primary
// repo, returning claude's exit code.
func runLaunch(names []string, prompt string) int {
	root, err := findRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	s, err := scope.Resolve(root, names)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	code, err := launch.Run(launch.Config{Scope: s, Prompt: prompt})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return code
}

func usage() {
	fmt.Fprint(os.Stderr, `scopr launches Claude Code scoped to chosen repositories.

usage:
  scopr [flags] <repo>...     start a session scoped to these repos; the first becomes the working directory
  scopr --where               print the workspace root

flags:
  -p <text>                   prompt to submit on start
  --where                     print the workspace root and exit

Flags must precede repository names.
`)
}

func main() {
	flag.Usage = usage

	where := flag.Bool("where", false, "print the workspace root and exit")
	prompt := flag.String("p", "", "prompt to submit on start")
	flag.Parse()

	if *where {
		os.Exit(runWhere())
	}

	names := flag.Args()
	if len(names) == 0 {
		usage()
		os.Exit(1)
	}

	os.Exit(runLaunch(names, *prompt))
}

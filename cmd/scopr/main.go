package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"scopr/internal/workspace"
)

// scopr --where
// Prints .scopr workspace root, returns exit signal
func runWhere() int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "couldn't resolve current working directory: %v\n", err)
		return 1
	}

	root, err := workspace.Find(cwd)
	if errors.Is(err, workspace.ErrNotFound) {
		fmt.Fprintln(os.Stderr, "not inside a .scopr workspace; run this command from within a workspace")
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}

	fmt.Fprintln(os.Stdout, root)
	return 0
}

func main() {
	where := flag.Bool("where", false, "print the workspace root and exit")
	flag.Parse()

	if *where {
		os.Exit(runWhere())
	}

	flag.Usage()
	os.Exit(1)
}

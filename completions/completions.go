// Package completions holds the shell completion scripts, kept here as real
// files so a plugin manager can install them without running the binary.
package completions

import _ "embed"

//go:embed _scopr
var Zsh string

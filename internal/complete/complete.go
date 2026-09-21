package complete

import (
	"strings"

	"scopr/internal/cli"
	"scopr/internal/scope"
)

type Candidate struct {
	Value string
	Desc  string
}

type Result struct {
	Candidates []Candidate

	// Files asks the shell to offer path completion too.
	Files bool
}

type Env struct {
	Root       func(workspace string) string
	Scopes     func(root string) []Candidate
	Repos      func(root string) []Candidate
	Workspaces func() []Candidate
}

// argv ends with the word being typed, empty when the cursor is after a space.
func Complete(env Env, argv []string) Result {
	if len(argv) == 0 {
		argv = []string{""}
	}

	prefix := argv[len(argv)-1]
	before := argv[:len(argv)-1]

	if f, ok := awaitingValue(before); ok {
		return keep(values(env, f), prefix)
	}

	s, err := cli.Flags.Scan(before)
	if err != nil {
		return Result{}
	}

	cmd, _, rest := cli.Locate(cli.Commands, s.Operands)

	if strings.HasPrefix(prefix, "-") && s.Terminus < 0 {
		return keep(flags(cmd), prefix)
	}

	var out []Candidate
	if len(rest) == 0 {
		out = append(out, children(cmd)...)
	}

	var kinds Result
	if cmd.Max < 0 || len(rest) < cmd.Max {
		kinds = forKind(env, env.Root(s.Str("workspace")), cmd.Kind(len(rest)))
	}

	return keep(Result{Candidates: append(out, kinds.Candidates...), Files: kinds.Files}, prefix)
}

// Checked before scanning: a trailing value flag makes the scan fail.
func awaitingValue(before []string) (*cli.Flag, bool) {
	if len(before) == 0 {
		return nil, false
	}

	tok := before[len(before)-1]
	if !strings.HasPrefix(tok, "-") || tok == "-" || tok == "--" || strings.Contains(tok, "=") {
		return nil, false
	}

	f, ok := cli.Flags.Lookup(strings.TrimLeft(tok, "-"))
	if !ok || f.Bool() {
		return nil, false
	}
	return f, true
}

func values(env Env, f *cli.Flag) Result {
	if f.Name == "workspace" {
		return Result{Candidates: env.Workspaces()}
	}
	return Result{}
}

func forKind(env Env, root string, k cli.Kind) Result {
	switch k {
	case cli.KindScope:
		return Result{Candidates: sigil(env.Scopes(root))}

	case cli.KindRepo:
		return Result{Candidates: env.Repos(root)}

	case cli.KindMember:
		return Result{Candidates: append(env.Repos(root), sigil(env.Scopes(root))...)}

	case cli.KindWorkspace:
		return Result{Candidates: env.Workspaces()}

	case cli.KindCommand:
		return Result{Candidates: children(cli.Commands)}

	case cli.KindPath:
		return Result{Files: true}
	}
	return Result{}
}

func sigil(in []Candidate) []Candidate {
	out := make([]Candidate, 0, len(in))
	for _, c := range in {
		out = append(out, Candidate{scope.Prefix + c.Value, c.Desc})
	}
	return out
}

func children(c *cli.Command) []Candidate {
	out := make([]Candidate, 0, len(c.Children))
	for _, k := range c.Children {
		if k.Help == "" {
			continue
		}
		out = append(out, Candidate{k.Name, k.Help})
	}
	return out
}

func flags(c *cli.Command) Result {
	var out []Candidate
	for f := range cli.Flags.All() {
		if f.Name != "help" && !accepts(c, f.Name) {
			continue
		}
		out = append(out, Candidate{"--" + f.Name, f.Help})
	}
	return Result{Candidates: out}
}

func accepts(c *cli.Command, name string) bool {
	for _, f := range c.Accepts {
		if f.Name == name {
			return true
		}
	}
	return false
}

func keep(r Result, prefix string) Result {
	if prefix == "" {
		return r
	}

	out := make([]Candidate, 0, len(r.Candidates))
	for _, c := range r.Candidates {
		if strings.HasPrefix(c.Value, prefix) {
			out = append(out, c)
		}
	}
	r.Candidates = out
	return r
}

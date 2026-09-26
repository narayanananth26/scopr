package complete

import (
	"strings"

	"scopr/internal/cli"
	"scopr/internal/scope"
)

type Candidate struct {
	Value string
	Desc  string

	// Group tags the candidate for the shell, which uses it to label and
	// preview each kind separately.
	Group string

	// Pin is the workspace to name with -w when the bare value would resolve
	// elsewhere.
	Pin string
}

func (c Candidate) Insert() string {
	if c.Pin == "" {
		return c.Value
	}
	return cli.Quote(c.Value) + " -w " + cli.Quote(c.Pin)
}

type Result struct {
	Candidates []Candidate

	// Files asks the shell to offer path completion too.
	Files bool
}

type Env struct {
	Root       func(workspace string) string
	Scopes     func(workspace string, local bool) []Candidate
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
		kinds = forKind(env, s.Str("workspace"), cmd.Writes, cmd.Kind(len(rest)))
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
		return Result{Candidates: stamp(env.Workspaces(), "workspaces", "")}
	}
	return Result{}
}

func forKind(env Env, workspace string, local bool, k cli.Kind) Result {
	switch k {
	case cli.KindScope:
		return Result{Candidates: stamp(env.Scopes(workspace, local), "scopes", scope.Prefix)}

	case cli.KindRepo:
		return Result{Candidates: stamp(env.Repos(env.Root(workspace)), "repos", "")}

	case cli.KindMember:
		return Result{Candidates: append(
			stamp(env.Repos(env.Root(workspace)), "repos", ""),
			stamp(env.Scopes(workspace, local), "scopes", scope.Prefix)...,
		)}

	case cli.KindWorkspace:
		return Result{Candidates: stamp(env.Workspaces(), "workspaces", "")}

	case cli.KindCommand:
		return Result{Candidates: children(cli.Commands)}

	case cli.KindPath:
		return Result{Files: true}
	}
	return Result{}
}

func stamp(in []Candidate, group, prefix string) []Candidate {
	out := make([]Candidate, 0, len(in))
	for _, c := range in {
		out = append(out, Candidate{Value: prefix + c.Value, Desc: c.Desc, Group: group, Pin: c.Pin})
	}
	return out
}

func children(c *cli.Command) []Candidate {
	out := make([]Candidate, 0, len(c.Children))
	for _, k := range c.Children {
		if k.Help == "" {
			continue
		}
		out = append(out, Candidate{Value: k.Name, Desc: k.Help, Group: "commands"})
	}
	return out
}

func flags(c *cli.Command) Result {
	var out []Candidate
	for f := range cli.Flags.All() {
		if f.Name != "help" && !accepts(c, f.Name) {
			continue
		}
		out = append(out, Candidate{Value: "--" + f.Name, Desc: f.Help, Group: "flags"})
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

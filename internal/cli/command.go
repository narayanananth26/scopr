package cli

import (
	"fmt"
	"slices"
	"strings"

	"scopr/internal/scope"
)

type Command struct {
	Name     string
	Use      string
	Help     string
	Children []*Command
	Accepts  []*Flag
	Min      int
	Max      int
	Scopes   int
	FreeText bool
}

func (c *Command) child(name string) (*Command, bool) {
	for _, k := range c.Children {
		if k.Name == name {
			return k, true
		}
	}
	return nil, false
}

func (c *Command) accepts(f *Flag) bool {
	return slices.Contains(c.Accepts, f)
}

func (c *Command) Nearest(name string) string {
	return nearestOf(c.options(), name)
}

func (c *Command) options() []string {
	out := make([]string, 0, len(c.Children))
	for _, k := range c.Children {
		out = append(out, k.Name)
	}
	return out
}

type Args struct {
	Scan
	Command *Command
	Path    []string
	Help    bool
}

func (a Args) Use() string {
	return strings.Join(append([]string{"scopr"}, a.Path...), " ")
}

func flagset(t *Table, names ...string) []*Flag {
	out := make([]*Flag, 0, len(names))
	for _, n := range names {
		out = append(out, t.Get(n))
	}
	return out
}

var Commands = &Command{
	Use:     "<@scope|repo>...",
	Help:    "start a session; the first repository becomes the working directory",
	Min:     0,
	Max:     -1,
	Accepts: flagset(Flags, "workspace", "prompt", "label"),
	Children: []*Command{
		{
			Name: "run", Use: "<@scope|repo>...",
			Help: "start a session, even for a repository named like a command",
			Min:  1, Max: -1,
			Accepts: flagset(Flags, "workspace", "prompt", "label"),
		},
		{
			Name: "infer", Use: "<task>",
			Help: "suggest a scope for the task, then start",
			Min:  1, Max: -1, FreeText: true,
			Accepts: flagset(Flags, "workspace", "prompt", "label", "verbose"),
		},
		{
			Name: "list",
			Help: "list saved scopes",
			Min:  0, Max: 0,
			Accepts: flagset(Flags, "workspace", "json", "all"),
		},
		{
			Name: "show", Use: "@name",
			Help: "print the repositories a scope names",
			Min:  1, Max: 1, Scopes: 1,
			Accepts: flagset(Flags, "workspace", "json"),
		},
		{
			Name: "save", Use: "@name <repo>...",
			Help: "save a scope under that name",
			Min:  2, Max: -1, Scopes: 1,
			Accepts: flagset(Flags, "workspace"),
		},
		{
			Name: "delete", Use: "@name",
			Help: "delete a saved scope",
			Min:  1, Max: 1, Scopes: 1,
			Accepts: flagset(Flags, "workspace"),
		},
		{
			Name: "rename", Use: "@old @new",
			Help: "rename a saved scope",
			Min:  2, Max: 2, Scopes: 2,
			Accepts: flagset(Flags, "workspace"),
		},
		{
			Name: "where",
			Help: "print the workspace root",
			Min:  0, Max: 0,
			Accepts: flagset(Flags, "workspace", "json"),
		},
		{
			Name: "workspace",
			Help: "manage registered workspaces",
			Min:  0, Max: 0,
			Children: []*Command{
				{Name: "list", Help: "list registered workspaces", Min: 0, Max: 0, Accepts: flagset(Flags, "json")},
				{Name: "add", Use: "[path]", Help: "register a workspace", Min: 0, Max: 1},
				{Name: "remove", Use: "<name>", Help: "forget a workspace", Min: 1, Max: 1},
			},
		},
		{Name: "help", Use: "[command]", Help: "show usage for a command", Min: 0, Max: 1},
		{Name: "version", Help: "print the version", Min: 0, Max: 0},
	},
}

type UnknownCommandError struct {
	Parent  string
	Token   string
	Suggest string
	Options []string
}

func (e *UnknownCommandError) Error() string {
	var b strings.Builder

	fmt.Fprintf(&b, "unknown command %q for %s", e.Token, e.Parent)
	if e.Suggest != "" {
		fmt.Fprintf(&b, "; did you mean %s?", e.Suggest)
	} else if len(e.Options) > 0 {
		fmt.Fprintf(&b, "; want %s", or(e.Options))
	}
	return b.String()
}

type MissingCommandError struct {
	Parent  string
	Options []string
}

func (e *MissingCommandError) Error() string {
	return fmt.Sprintf("%s needs a subcommand: %s", e.Parent, or(e.Options))
}

type FlagNotAcceptedError struct {
	Token   string
	Command string
}

func (e *FlagNotAcceptedError) Error() string {
	return fmt.Sprintf("%s does not take %s", e.Command, e.Token)
}

type SigilError struct {
	Command   string
	Corrected string
}

func (e *SigilError) Error() string {
	return fmt.Sprintf("a scope is named with %s: %s", scope.Prefix, e.Corrected)
}

type ArityError struct {
	Command string
	Use     string
	Got     int
	Min     int
	Max     int
}

func (e *ArityError) Error() string {
	if e.Use == "" {
		return fmt.Sprintf("%s takes no arguments, got %d", e.Command, e.Got)
	}
	return fmt.Sprintf("usage: %s %s", e.Command, e.Use)
}

func Parse(argv []string) (Args, error) {
	s, err := Flags.Scan(argv)
	if err != nil {
		return Args{}, err
	}
	return Bind(Commands, s)
}

func Bind(root *Command, s Scan) (Args, error) {
	cmd, path, operands, err := descend(root, s.Operands)
	if err != nil {
		return Args{}, err
	}

	out := Args{Scan: s, Command: cmd, Path: path}
	out.Scan.Operands = operands

	if s.Has("help") {
		out.Help = true
		return out, nil
	}

	use := out.Use()

	for _, o := range s.Given {
		if !cmd.accepts(o.Flag) {
			return Args{}, &FlagNotAcceptedError{Token: o.Token, Command: use}
		}
	}

	if len(operands) < cmd.Min || (cmd.Max >= 0 && len(operands) > cmd.Max) {
		return Args{}, &ArityError{
			Command: use,
			Use:     cmd.Use,
			Got:     len(operands),
			Min:     cmd.Min,
			Max:     cmd.Max,
		}
	}

	if cmd.Scopes > 0 && !sigiled(operands, cmd.Scopes) {
		return Args{}, &SigilError{Command: use, Corrected: corrected(path, operands, cmd.Scopes)}
	}

	if cmd.FreeText && len(operands) > 0 {
		out.Scan.Operands = []string{strings.Join(operands, " ")}
	}

	return out, nil
}

func sigiled(operands []string, n int) bool {
	for _, o := range operands[:n] {
		if !strings.HasPrefix(o, scope.Prefix) {
			return false
		}
	}
	return true
}

func corrected(path, operands []string, n int) string {
	parts := append([]string{"scopr"}, path...)
	for i, o := range operands {
		if i < n && !strings.HasPrefix(o, scope.Prefix) {
			o = scope.Prefix + o
		}
		parts = append(parts, o)
	}
	return strings.Join(parts, " ")
}

func (a Args) CommandHint() string {
	if len(a.Path) > 0 || len(a.Operands) == 0 {
		return ""
	}

	near := Commands.Nearest(a.Operands[0])
	if near == "" {
		return ""
	}
	return fmt.Sprintf("did you mean the command %q?", "scopr "+near)
}

func descend(root *Command, operands []string) (*Command, []string, []string, error) {
	cmd := root
	var path []string

	for len(cmd.Children) > 0 {
		if len(operands) > 0 {
			if next, ok := cmd.child(operands[0]); ok {
				path = append(path, operands[0])
				operands = operands[1:]
				cmd = next
				continue
			}
		}

		if cmd.Max != 0 {
			break
		}

		parent := strings.Join(append([]string{"scopr"}, path...), " ")
		if len(operands) == 0 {
			return nil, nil, nil, &MissingCommandError{Parent: parent, Options: cmd.options()}
		}
		return nil, nil, nil, &UnknownCommandError{
			Parent:  parent,
			Token:   operands[0],
			Suggest: nearestOf(cmd.options(), operands[0]),
			Options: cmd.options(),
		}
	}

	return cmd, path, operands, nil
}

func nearestOf(names []string, name string) string {
	best, closest := "", 3
	for _, n := range names {
		if d := distance(name, n); d < closest {
			best, closest = n, d
		}
	}
	return best
}

func or(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " or " + items[len(items)-1]
}

package cli

import (
	"fmt"
	"slices"
	"strings"
)

type Command struct {
	Name     string
	Use      string
	Children []*Command
	Accepts  []*Flag
	Min      int
	Max      int
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
	Min:     0,
	Max:     -1,
	Accepts: flagset(Flags, "workspace", "prompt", "label"),
	Children: []*Command{
		{
			Name: "run", Use: "<@scope|repo>...",
			Min: 1, Max: -1,
			Accepts: flagset(Flags, "workspace", "prompt", "label"),
		},
		{
			Name: "infer", Use: "<task>",
			Min: 1, Max: -1, FreeText: true,
			Accepts: flagset(Flags, "workspace", "prompt", "label", "verbose"),
		},
		{
			Name: "list",
			Min:  0, Max: 0,
			Accepts: flagset(Flags, "workspace", "json"),
		},
		{
			Name: "show", Use: "@name",
			Min: 1, Max: 1,
			Accepts: flagset(Flags, "workspace", "json"),
		},
		{
			Name: "save", Use: "@name <repo>...",
			Min: 2, Max: -1,
			Accepts: flagset(Flags, "workspace"),
		},
		{
			Name: "delete", Use: "@name",
			Min: 1, Max: 1,
			Accepts: flagset(Flags, "workspace"),
		},
		{
			Name: "rename", Use: "@old @new",
			Min: 2, Max: 2,
			Accepts: flagset(Flags, "workspace"),
		},
		{
			Name: "where",
			Min:  0, Max: 0,
			Accepts: flagset(Flags, "workspace", "json"),
		},
		{
			Name: "workspace",
			Min:  0, Max: 0,
			Children: []*Command{
				{Name: "list", Min: 0, Max: 0, Accepts: flagset(Flags, "json")},
				{Name: "add", Use: "[path]", Min: 0, Max: 1},
				{Name: "remove", Use: "<name>", Min: 1, Max: 1},
			},
		},
		{Name: "help", Use: "[command]", Min: 0, Max: 1},
		{Name: "version", Min: 0, Max: 0},
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

	if cmd.FreeText && len(operands) > 0 {
		out.Scan.Operands = []string{strings.Join(operands, " ")}
	}

	return out, nil
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

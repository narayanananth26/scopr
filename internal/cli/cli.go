package cli

import (
	"fmt"
	"iter"
	"slices"
	"strconv"
	"strings"
)

type Flag struct {
	Name  string
	Short string
	Arg   string
	Help  string
}

func (f *Flag) Bool() bool { return f.Arg == "" }

type Table struct {
	flags []Flag
	index map[string]*Flag
}

func NewTable(flags ...Flag) (*Table, error) {
	t := &Table{
		flags: slices.Clone(flags),
		index: make(map[string]*Flag, 2*len(flags)),
	}

	for i := range t.flags {
		f := &t.flags[i]

		switch {
		case f.Name == "":
			return nil, fmt.Errorf("cli: flag %d has no name", i)
		case strings.ContainsAny(f.Name, "-="):
			return nil, fmt.Errorf("cli: flag %q contains a dash or an equals sign", f.Name)
		case len(f.Short) > 1:
			return nil, fmt.Errorf("cli: short flag %q for %q is not one letter", f.Short, f.Name)
		case f.Short == "-":
			return nil, fmt.Errorf("cli: short flag for %q is a dash", f.Name)
		case f.Help == "":
			return nil, fmt.Errorf("cli: flag %q has no help", f.Name)
		}

		for _, key := range []string{f.Name, f.Short} {
			if key == "" {
				continue
			}
			if prev, ok := t.index[key]; ok {
				return nil, fmt.Errorf("cli: %q is claimed by both %q and %q", key, prev.Name, f.Name)
			}
			t.index[key] = f
		}
	}

	return t, nil
}

func MustTable(flags ...Flag) *Table {
	t, err := NewTable(flags...)
	if err != nil {
		panic(err)
	}
	return t
}

// name must already be stripped of dashes.
func (t *Table) Lookup(name string) (*Flag, bool) {
	f, ok := t.index[name]
	return f, ok
}

func (t *Table) Get(name string) *Flag {
	f, ok := t.index[name]
	if !ok {
		panic(fmt.Sprintf("cli: no flag named %q", name))
	}
	return f
}

func (t *Table) All() iter.Seq[Flag] {
	return func(yield func(Flag) bool) {
		for _, f := range t.flags {
			if !yield(f) {
				return
			}
		}
	}
}

func (t *Table) nearest(name string) string {
	if name == "" {
		return ""
	}

	var (
		prefix string
		hits   int
	)
	for _, f := range t.flags {
		if strings.HasPrefix(f.Name, name) {
			prefix, hits = f.Name, hits+1
		}
	}
	if hits == 1 {
		return prefix
	}

	best, closest := "", 3
	for _, f := range t.flags {
		if d := distance(name, f.Name); d < closest {
			best, closest = f.Name, d
		}
	}
	return best
}

var Flags = MustTable(
	Flag{Name: "workspace", Short: "w", Arg: "NAME", Help: "which workspace to resolve against"},
	Flag{Name: "prompt", Short: "p", Arg: "TEXT", Help: "prompt to submit on start"},
	Flag{Name: "label", Short: "l", Arg: "TEXT", Help: "name the session"},
	Flag{Name: "verbose", Help: "show the survey's tool calls"},
	Flag{Name: "json", Help: "machine-readable output"},
	Flag{Name: "all", Short: "a", Help: "every workspace, grouped"},
	Flag{Name: "help", Short: "h", Help: "show usage"},
	Flag{Name: "protocol", Arg: "N", Help: "completion protocol the caller speaks"},
)

type Occurrence struct {
	Flag  *Flag
	Value string
	Token string
	Index int
}

type Scan struct {
	Given    []Occurrence
	Operands []string
	Terminus int
}

func (s Scan) Has(name string) bool {
	for _, o := range s.Given {
		if o.Flag.Name == name {
			return true
		}
	}
	return false
}

func (s Scan) Str(name string) string {
	for i := len(s.Given) - 1; i >= 0; i-- {
		if s.Given[i].Flag.Name == name {
			return s.Given[i].Value
		}
	}
	return ""
}

func (s Scan) Bool(name string) bool {
	return s.Str(name) == "true"
}

type UnknownFlagError struct {
	Token   string
	Index   int
	Suggest string
}

func (e *UnknownFlagError) Error() string {
	if e.Suggest != "" {
		return fmt.Sprintf("unknown flag %s; did you mean --%s?", e.Token, e.Suggest)
	}
	return fmt.Sprintf("unknown flag %s", e.Token)
}

type MissingValueError struct {
	Token string
	Index int
	Arg   string
}

func (e *MissingValueError) Error() string {
	return fmt.Sprintf("flag %s needs a value: %s %s", e.Token, e.Token, e.Arg)
}

type BoolValueError struct {
	Token string
	Index int
	Value string
}

func (e *BoolValueError) Error() string {
	return fmt.Sprintf("flag %s takes true or false, not %q", e.Token, e.Value)
}

// argv excludes the program name.
func (t *Table) Scan(argv []string) (Scan, error) {
	out := Scan{Terminus: -1}

	for i := 0; i < len(argv); i++ {
		tok := argv[i]

		if out.Terminus >= 0 {
			out.Operands = append(out.Operands, tok)
			continue
		}

		if tok == "--" {
			out.Terminus = i
			continue
		}

		if tok == "-" || !strings.HasPrefix(tok, "-") {
			out.Operands = append(out.Operands, tok)
			continue
		}

		name, value, inline := strings.Cut(strings.TrimLeft(tok, "-"), "=")

		f, ok := t.Lookup(name)
		if !ok {
			return Scan{Terminus: -1}, &UnknownFlagError{Token: tok, Index: i, Suggest: t.nearest(name)}
		}

		at := Occurrence{Flag: f, Value: value, Token: tok, Index: i}

		switch {
		case f.Bool() && !inline:
			at.Value = "true"

		case f.Bool():
			b, err := strconv.ParseBool(value)
			if err != nil {
				return Scan{Terminus: -1}, &BoolValueError{Token: tok, Index: i, Value: value}
			}
			at.Value = strconv.FormatBool(b)

		case !inline && i+1 >= len(argv):
			return Scan{Terminus: -1}, &MissingValueError{Token: tok, Index: i, Arg: f.Arg}

		case !inline:
			at.Value = argv[i+1]
			i++
		}

		out.Given = append(out.Given, at)
	}

	return out, nil
}

func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)

	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}

	return prev[len(b)]
}

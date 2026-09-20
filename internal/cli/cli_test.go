package cli_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"scopr/internal/cli"
)

func scanned(t *testing.T, argv ...string) cli.Scan {
	t.Helper()

	s, err := cli.Flags.Scan(argv)
	if err != nil {
		t.Fatalf("Scan %q: %v", argv, err)
	}
	return s
}

func given(s cli.Scan) []string {
	out := make([]string, 0, len(s.Given))
	for _, o := range s.Given {
		out = append(out, fmt.Sprintf("%s=%s", o.Flag.Name, o.Value))
	}
	return out
}

func panics(t *testing.T, call func()) (recovered bool) {
	t.Helper()

	defer func() { recovered = recover() != nil }()
	call()
	return
}

func TestScanKeepsOperandOrder(t *testing.T) {
	s := scanned(t, "gl-panel", "gl-api", "gl-web")

	if want := []string{"gl-panel", "gl-api", "gl-web"}; !slices.Equal(s.Operands, want) {
		t.Errorf("operands = %v, want %v", s.Operands, want)
	}
}

func TestScanFindsFlagsAnywhere(t *testing.T) {
	s := scanned(t, "save", "@n", "-p", "x", "a", "--verbose", "b")

	if want := []string{"save", "@n", "a", "b"}; !slices.Equal(s.Operands, want) {
		t.Errorf("operands = %v, want %v", s.Operands, want)
	}
	if want := []string{"prompt=x", "verbose=true"}; !slices.Equal(given(s), want) {
		t.Errorf("flags = %v, want %v", given(s), want)
	}
}

func TestScanAcceptsEverySpelling(t *testing.T) {
	for _, argv := range [][]string{
		{"-p", "x"},
		{"--prompt", "x"},
		{"-p=x"},
		{"--prompt=x"},
		{"-prompt", "x"},
	} {
		if got := scanned(t, argv...).Str("prompt"); got != "x" {
			t.Errorf("Scan %q: prompt = %q, want %q", argv, got, "x")
		}
	}
}

func TestScanBooleanNeverEatsNextToken(t *testing.T) {
	s := scanned(t, "--verbose", "alpha")

	if !s.Bool("verbose") {
		t.Error("verbose not set")
	}
	if want := []string{"alpha"}; !slices.Equal(s.Operands, want) {
		t.Errorf("operands = %v, want %v", s.Operands, want)
	}
}

func TestScanReadsBooleanValues(t *testing.T) {
	for argv, want := range map[string]bool{
		"--verbose":       true,
		"--verbose=true":  true,
		"--verbose=1":     true,
		"--verbose=false": false,
		"--verbose=0":     false,
	} {
		if got := scanned(t, argv).Bool("verbose"); got != want {
			t.Errorf("Scan %q: verbose = %v, want %v", argv, got, want)
		}
	}
}

func TestScanTerminatorEndsFlags(t *testing.T) {
	s := scanned(t, "run", "--", "-weird")

	if want := []string{"run", "-weird"}; !slices.Equal(s.Operands, want) {
		t.Errorf("operands = %v, want %v", s.Operands, want)
	}
	if s.Terminus != 1 {
		t.Errorf("terminus = %d, want 1", s.Terminus)
	}
	if len(s.Given) != 0 {
		t.Errorf("flags = %v, want none", given(s))
	}
}

func TestScanTerminatorIsOnlyHonouredOnce(t *testing.T) {
	s := scanned(t, "run", "--", "a", "--", "b")

	if want := []string{"run", "a", "--", "b"}; !slices.Equal(s.Operands, want) {
		t.Errorf("operands = %v, want %v", s.Operands, want)
	}
	if s.Terminus != 1 {
		t.Errorf("terminus = %d, want 1", s.Terminus)
	}
}

func TestScanTerminatorLeavesCommandLookupAlone(t *testing.T) {
	s := scanned(t, "--", "list")

	if want := []string{"list"}; !slices.Equal(s.Operands, want) {
		t.Errorf("operands = %v, want %v", s.Operands, want)
	}
}

func TestScanTakesSeparatedValueWhateverItLooksLike(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		flag string
		want string
	}{
		{[]string{"-p", "--"}, "prompt", "--"},
		{[]string{"-p", "-w"}, "prompt", "-w"},
		{[]string{"-w", "--json", "list"}, "workspace", "--json"},
	} {
		s := scanned(t, tc.argv...)
		if got := s.Str(tc.flag); got != tc.want {
			t.Errorf("Scan %q: %s = %q, want %q", tc.argv, tc.flag, got, tc.want)
		}
	}
}

func TestScanKeepsLastValue(t *testing.T) {
	if got := scanned(t, "-p", "x", "-p", "y").Str("prompt"); got != "y" {
		t.Errorf("prompt = %q, want %q", got, "y")
	}
	if got := scanned(t, "--verbose", "--verbose=false").Bool("verbose"); got {
		t.Error("verbose = true, want false")
	}
}

func TestScanTreatsLoneDashAsOperand(t *testing.T) {
	s := scanned(t, "-", "alpha")

	if want := []string{"-", "alpha"}; !slices.Equal(s.Operands, want) {
		t.Errorf("operands = %v, want %v", s.Operands, want)
	}
}

func TestScanOnNothing(t *testing.T) {
	s := scanned(t)

	if len(s.Operands) != 0 || len(s.Given) != 0 {
		t.Errorf("operands = %v, flags = %v, want none", s.Operands, given(s))
	}
	if s.Terminus != -1 {
		t.Errorf("terminus = %d, want -1", s.Terminus)
	}
}

func TestScanRecordsArgvPosition(t *testing.T) {
	s := scanned(t, "save", "@n", "-p", "x", "--verbose")

	if s.Given[0].Index != 2 {
		t.Errorf("prompt index = %d, want 2", s.Given[0].Index)
	}
	if s.Given[1].Index != 4 {
		t.Errorf("verbose index = %d, want 4", s.Given[1].Index)
	}
}

func TestScanRejectsUnknownFlag(t *testing.T) {
	_, err := cli.Flags.Scan([]string{"a", "--nope"})

	var unknown *cli.UnknownFlagError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want *UnknownFlagError", err)
	}
	if unknown.Token != "--nope" {
		t.Errorf("token = %q, want %q", unknown.Token, "--nope")
	}
	if unknown.Index != 1 {
		t.Errorf("index = %d, want 1", unknown.Index)
	}
	if unknown.Suggest != "" {
		t.Errorf("suggest = %q, want none", unknown.Suggest)
	}
}

func TestScanSuggestsNearestFlag(t *testing.T) {
	for token, want := range map[string]string{
		"--verbse":  "verbose",
		"--work":    "workspace",
		"--jsn":     "json",
		"--promptt": "prompt",
		"--labl":    "label",
	} {
		_, err := cli.Flags.Scan([]string{token})

		var unknown *cli.UnknownFlagError
		if !errors.As(err, &unknown) {
			t.Fatalf("Scan %q: err = %v, want *UnknownFlagError", token, err)
		}
		if unknown.Suggest != want {
			t.Errorf("Scan %q: suggest = %q, want %q", token, unknown.Suggest, want)
		}
	}
}

func TestScanRejectsMissingValue(t *testing.T) {
	_, err := cli.Flags.Scan([]string{"a", "-w"})

	var missing *cli.MissingValueError
	if !errors.As(err, &missing) {
		t.Fatalf("err = %v, want *MissingValueError", err)
	}
	if missing.Index != 1 {
		t.Errorf("index = %d, want 1", missing.Index)
	}
	if missing.Arg != "NAME" {
		t.Errorf("arg = %q, want %q", missing.Arg, "NAME")
	}
}

func TestScanRejectsNonBooleanValue(t *testing.T) {
	_, err := cli.Flags.Scan([]string{"--verbose=maybe"})

	var bad *cli.BoolValueError
	if !errors.As(err, &bad) {
		t.Fatalf("err = %v, want *BoolValueError", err)
	}
	if bad.Value != "maybe" {
		t.Errorf("value = %q, want %q", bad.Value, "maybe")
	}
}

func TestScanRejectsNamelessFlag(t *testing.T) {
	if _, err := cli.Flags.Scan([]string{"--=x"}); err == nil {
		t.Fatal("Scan --=x: no error")
	}
}

func TestHasDistinguishesEmptyFromAbsent(t *testing.T) {
	s := scanned(t, "-p=")

	if !s.Has("prompt") {
		t.Error("prompt not reported as given")
	}
	if got := s.Str("prompt"); got != "" {
		t.Errorf("prompt = %q, want empty", got)
	}
	if scanned(t).Has("prompt") {
		t.Error("absent prompt reported as given")
	}
}

func TestStrIsEmptyWhenAbsent(t *testing.T) {
	if got := scanned(t, "alpha").Str("workspace"); got != "" {
		t.Errorf("workspace = %q, want empty", got)
	}
}

func TestNewTableRejectsBadDeclarations(t *testing.T) {
	for name, flags := range map[string][]cli.Flag{
		"no name":         {{Arg: "X", Help: "h"}},
		"dash in name":    {{Name: "dry-run", Help: "h"}},
		"equals in name":  {{Name: "a=b", Help: "h"}},
		"long short":      {{Name: "verbose", Short: "vb", Help: "h"}},
		"dash short":      {{Name: "verbose", Short: "-", Help: "h"}},
		"no help":         {{Name: "verbose"}},
		"duplicate name":  {{Name: "verbose", Help: "h"}, {Name: "verbose", Help: "h"}},
		"duplicate short": {{Name: "verbose", Short: "v", Help: "h"}, {Name: "version", Short: "v", Help: "h"}},
	} {
		if _, err := cli.NewTable(flags...); err == nil {
			t.Errorf("NewTable %s: no error", name)
		}
	}
}

func TestNewTableIndexesNameAndShort(t *testing.T) {
	table, err := cli.NewTable(
		cli.Flag{Name: "workspace", Short: "w", Arg: "NAME", Help: "h"},
		cli.Flag{Name: "verbose", Help: "h"},
	)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}

	for _, key := range []string{"workspace", "w", "verbose"} {
		if _, ok := table.Lookup(key); !ok {
			t.Errorf("Lookup %q: not found", key)
		}
	}
	if _, ok := table.Lookup("v"); ok {
		t.Error("Lookup v: found, want none")
	}
	if _, ok := table.Lookup("--workspace"); ok {
		t.Error("Lookup --workspace: found, want none")
	}
}

func TestFlagBoolFollowsArg(t *testing.T) {
	value, _ := cli.Flags.Lookup("workspace")
	if value.Bool() {
		t.Error("workspace reported as boolean")
	}

	boolean, _ := cli.Flags.Lookup("verbose")
	if !boolean.Bool() {
		t.Error("verbose not reported as boolean")
	}
}

func TestMustTablePanicsOnBadDeclaration(t *testing.T) {
	if !panics(t, func() { cli.MustTable(cli.Flag{Name: "verbose"}) }) {
		t.Error("MustTable did not panic")
	}
}

func TestGetPanicsOnUnknownFlag(t *testing.T) {
	if !panics(t, func() { cli.Flags.Get("nope") }) {
		t.Error("Get did not panic")
	}
	if panics(t, func() { cli.Flags.Get("workspace") }) {
		t.Error("Get panicked on a known flag")
	}
}

func TestAllKeepsDeclarationOrder(t *testing.T) {
	var names []string
	for f := range cli.Flags.All() {
		names = append(names, f.Name)
	}

	want := []string{"workspace", "prompt", "label", "verbose", "json", "help"}
	if !slices.Equal(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
}

func TestAllStopsEarly(t *testing.T) {
	seen := 0
	for range cli.Flags.All() {
		seen++
		break
	}

	if seen != 1 {
		t.Errorf("yielded %d flags, want 1", seen)
	}
}

func FuzzScanAccountsForEveryToken(f *testing.F) {
	for _, seed := range []string{
		"",
		"--",
		"-w",
		"-p x -p y run a",
		"save @n -p x a --verbose b",
		"run -- -weird",
		"--verbose=false --json",
		"- alpha -p=",
		"infer fix the -p x build",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, line string) {
		argv := strings.Split(line, " ")

		s, err := cli.Flags.Scan(argv)
		if err != nil {
			return
		}

		count := len(s.Operands)
		if s.Terminus >= 0 {
			count++
		}
		for _, o := range s.Given {
			count++
			if !o.Flag.Bool() && !strings.Contains(o.Token, "=") {
				count++
			}
		}

		if count != len(argv) {
			t.Fatalf("accounted for %d of %d tokens in %q", count, len(argv), argv)
		}
	})
}

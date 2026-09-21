package cli_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"scopr/internal/cli"
)

func parsed(t *testing.T, argv ...string) cli.Args {
	t.Helper()

	a, err := cli.Parse(argv)
	if err != nil {
		t.Fatalf("Parse %q: %v", argv, err)
	}
	return a
}

func failed(t *testing.T, argv ...string) error {
	t.Helper()

	if _, err := cli.Parse(argv); err != nil {
		return err
	}
	t.Fatalf("Parse %q: no error", argv)
	return nil
}

func TestBindLaunchesWithoutAVerb(t *testing.T) {
	a := parsed(t, "@web", "gl-api")

	if a.Use() != "scopr" {
		t.Errorf("command = %q, want %q", a.Use(), "scopr")
	}
	if want := []string{"@web", "gl-api"}; !slices.Equal(a.Operands, want) {
		t.Errorf("operands = %v, want %v", a.Operands, want)
	}
}

func TestBindOnNothing(t *testing.T) {
	a := parsed(t)

	if a.Use() != "scopr" {
		t.Errorf("command = %q, want %q", a.Use(), "scopr")
	}
	if len(a.Operands) != 0 {
		t.Errorf("operands = %v, want none", a.Operands)
	}
}

func TestBindKeepsUnknownWordsAsOperands(t *testing.T) {
	a := parsed(t, "lsit")

	if a.Use() != "scopr" {
		t.Errorf("command = %q, want %q", a.Use(), "scopr")
	}
	if want := []string{"lsit"}; !slices.Equal(a.Operands, want) {
		t.Errorf("operands = %v, want %v", a.Operands, want)
	}
}

func TestBindRunLaunchesAReservedWord(t *testing.T) {
	a := parsed(t, "run", "list")

	if a.Use() != "scopr run" {
		t.Errorf("command = %q, want %q", a.Use(), "scopr run")
	}
	if want := []string{"list"}; !slices.Equal(a.Operands, want) {
		t.Errorf("operands = %v, want %v", a.Operands, want)
	}
}

func TestBindRunNeedsAnOperand(t *testing.T) {
	var arity *cli.ArityError
	if err := failed(t, "run"); !errors.As(err, &arity) {
		t.Fatalf("err = %v, want *ArityError", err)
	}
}

func TestBindTerminatorDoesNotHideAVerb(t *testing.T) {
	a := parsed(t, "--", "list")

	if a.Use() != "scopr list" {
		t.Errorf("command = %q, want %q", a.Use(), "scopr list")
	}
}

func TestBindDescendsIntoSubcommands(t *testing.T) {
	a := parsed(t, "workspace", "add", "/tmp/x")

	if a.Use() != "scopr workspace add" {
		t.Errorf("command = %q, want %q", a.Use(), "scopr workspace add")
	}
	if want := []string{"/tmp/x"}; !slices.Equal(a.Operands, want) {
		t.Errorf("operands = %v, want %v", a.Operands, want)
	}
}

func TestBindRequiresASubcommand(t *testing.T) {
	err := failed(t, "workspace")

	var missing *cli.MissingCommandError
	if !errors.As(err, &missing) {
		t.Fatalf("err = %v, want *MissingCommandError", err)
	}
	if want := []string{"list", "add", "remove"}; !slices.Equal(missing.Options, want) {
		t.Errorf("options = %v, want %v", missing.Options, want)
	}
	if got := missing.Error(); !strings.Contains(got, "list, add or remove") {
		t.Errorf("message = %q, want the options listed", got)
	}
}

func TestBindSuggestsNearestSubcommand(t *testing.T) {
	err := failed(t, "workspace", "lst")

	var unknown *cli.UnknownCommandError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want *UnknownCommandError", err)
	}
	if unknown.Suggest != "list" {
		t.Errorf("suggest = %q, want %q", unknown.Suggest, "list")
	}
	if unknown.Parent != "scopr workspace" {
		t.Errorf("parent = %q, want %q", unknown.Parent, "scopr workspace")
	}
}

func TestBindListsSubcommandsWhenNothingIsClose(t *testing.T) {
	err := failed(t, "workspace", "zzzzzz")

	var unknown *cli.UnknownCommandError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want *UnknownCommandError", err)
	}
	if unknown.Suggest != "" {
		t.Errorf("suggest = %q, want none", unknown.Suggest)
	}
	if got := unknown.Error(); !strings.Contains(got, "list, add or remove") {
		t.Errorf("message = %q, want the options listed", got)
	}
}

func TestBindRejectsUnacceptedFlags(t *testing.T) {
	for _, argv := range [][]string{
		{"list", "-p", "x"},
		{"where", "--verbose"},
		{"save", "@n", "a", "--json"},
		{"workspace", "list", "-w", "x"},
		{"workspace", "add", "--workspace", "x"},
	} {
		var refused *cli.FlagNotAcceptedError
		if err := failed(t, argv...); !errors.As(err, &refused) {
			t.Errorf("Parse %q: err = %v, want *FlagNotAcceptedError", argv, err)
		}
	}
}

func TestBindQuotesTheFlagAsTyped(t *testing.T) {
	err := failed(t, "list", "-p", "x")

	var refused *cli.FlagNotAcceptedError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want *FlagNotAcceptedError", err)
	}
	if refused.Token != "-p" {
		t.Errorf("token = %q, want %q", refused.Token, "-p")
	}
	if refused.Command != "scopr list" {
		t.Errorf("command = %q, want %q", refused.Command, "scopr list")
	}
}

func TestBindAcceptsWorkspaceOnScopedCommands(t *testing.T) {
	for _, argv := range [][]string{
		{"-w", "x", "list"},
		{"list", "-w", "x"},
		{"--workspace", "x", "save", "@n", "a"},
		{"show", "@n", "--json"},
	} {
		if a := parsed(t, argv...); a.Str("workspace") == "" && !a.Bool("json") {
			t.Errorf("Parse %q: flag not carried through", argv)
		}
	}
}

func TestBindArity(t *testing.T) {
	for _, argv := range [][]string{
		{"list", "extra"},
		{"save", "@n"},
		{"show"},
		{"show", "@a", "@b"},
		{"rename", "@a"},
		{"rename", "@a", "@b", "@c"},
		{"where", "x"},
		{"infer"},
		{"workspace", "remove"},
		{"workspace", "add", "a", "b"},
	} {
		var arity *cli.ArityError
		if err := failed(t, argv...); !errors.As(err, &arity) {
			t.Errorf("Parse %q: err = %v, want *ArityError", argv, err)
		}
	}
}

func TestBindArityMessageUsesTheRealCommand(t *testing.T) {
	err := failed(t, "save", "@n")

	if want := "usage: scopr save @name <repo>..."; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

func TestBindArityMessageWhenNothingIsTaken(t *testing.T) {
	err := failed(t, "list", "extra")

	if want := "scopr list takes no arguments, got 1"; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

func TestBindJoinsFreeText(t *testing.T) {
	a := parsed(t, "infer", "fix", "the", "flaky", "build")

	if want := []string{"fix the flaky build"}; !slices.Equal(a.Operands, want) {
		t.Errorf("operands = %v, want %v", a.Operands, want)
	}
}

func TestBindKeepsFlagsOutOfFreeText(t *testing.T) {
	a := parsed(t, "infer", "fix", "the", "-p", "x", "build")

	if want := []string{"fix the build"}; !slices.Equal(a.Operands, want) {
		t.Errorf("operands = %v, want %v", a.Operands, want)
	}
	if got := a.Str("prompt"); got != "x" {
		t.Errorf("prompt = %q, want %q", got, "x")
	}
}

func TestBindHelpSkipsArity(t *testing.T) {
	a := parsed(t, "save", "-h")

	if !a.Help {
		t.Error("help not reported")
	}
	if a.Use() != "scopr save" {
		t.Errorf("command = %q, want %q", a.Use(), "scopr save")
	}
}

func TestBindHelpSkipsFlagAcceptance(t *testing.T) {
	a := parsed(t, "list", "-p", "x", "-h")

	if !a.Help {
		t.Error("help not reported")
	}
}

func TestBindHelpIsAlsoACommand(t *testing.T) {
	a := parsed(t, "help", "save")

	if a.Use() != "scopr help" {
		t.Errorf("command = %q, want %q", a.Use(), "scopr help")
	}
	if a.Help {
		t.Error("help flag reported for the help command")
	}
}

func TestParseReportsScanErrors(t *testing.T) {
	var unknown *cli.UnknownFlagError
	if err := failed(t, "list", "--nope"); !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want *UnknownFlagError", err)
	}
}

func TestBindPromotesScanAccessors(t *testing.T) {
	a := parsed(t, "-p", "x", "@web")

	if !a.Has("prompt") {
		t.Error("prompt not reported as given")
	}
	if got := a.Str("prompt"); got != "x" {
		t.Errorf("prompt = %q, want %q", got, "x")
	}
	if a.Terminus != -1 {
		t.Errorf("terminus = %d, want -1", a.Terminus)
	}
}

func TestBindUsesTheGivenRoot(t *testing.T) {
	table, err := cli.NewTable(cli.Flag{Name: "loud", Help: "h"})
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}

	root := &cli.Command{
		Max: -1,
		Children: []*cli.Command{
			{Name: "ping", Min: 0, Max: 0, Accepts: []*cli.Flag{table.Get("loud")}},
		},
	}

	s, err := table.Scan([]string{"ping", "--loud"})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	a, err := cli.Bind(root, s)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if a.Use() != "scopr ping" {
		t.Errorf("command = %q, want %q", a.Use(), "scopr ping")
	}
	if !a.Bool("loud") {
		t.Error("loud not set")
	}
}

func TestNearestFindsAMistypedCommand(t *testing.T) {
	for typo, want := range map[string]string{
		"lsit":      "list",
		"lst":       "list",
		"svae":      "save",
		"wherr":     "where",
		"workspac":  "workspace",
		"gl-panel":  "",
		"zzzzzzzzz": "",
	} {
		if got := cli.Commands.Nearest(typo); got != want {
			t.Errorf("Nearest %q = %q, want %q", typo, got, want)
		}
	}
}

func TestBindRequiresTheSigil(t *testing.T) {
	for _, argv := range [][]string{
		{"save", "web", "gl-panel"},
		{"delete", "web"},
		{"show", "web"},
		{"rename", "old", "@new"},
		{"rename", "@old", "new"},
	} {
		var sigil *cli.SigilError
		if err := failed(t, argv...); !errors.As(err, &sigil) {
			t.Errorf("Parse %q: err = %v, want *SigilError", argv, err)
		}
	}
}

func TestBindAcceptsTheSigil(t *testing.T) {
	for _, argv := range [][]string{
		{"save", "@web", "gl-panel"},
		{"delete", "@web"},
		{"show", "@web"},
		{"rename", "@old", "@new"},
	} {
		parsed(t, argv...)
	}
}

func TestSigilErrorCorrectsOnlyTheScopes(t *testing.T) {
	err := failed(t, "save", "web", "gl-panel", "gl-api")

	var sigil *cli.SigilError
	if !errors.As(err, &sigil) {
		t.Fatalf("err = %v, want *SigilError", err)
	}
	if want := "scopr save @web gl-panel gl-api"; sigil.Corrected != want {
		t.Errorf("corrected = %q, want %q", sigil.Corrected, want)
	}
	if want := "a scope is named with @: scopr save @web gl-panel gl-api"; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

func TestSigilErrorCorrectsBothNames(t *testing.T) {
	err := failed(t, "rename", "old", "@new")

	var sigil *cli.SigilError
	if !errors.As(err, &sigil) {
		t.Fatalf("err = %v, want *SigilError", err)
	}
	if want := "scopr rename @old @new"; sigil.Corrected != want {
		t.Errorf("corrected = %q, want %q", sigil.Corrected, want)
	}
}

func TestBindChecksArityBeforeTheSigil(t *testing.T) {
	var arity *cli.ArityError
	if err := failed(t, "save", "web"); !errors.As(err, &arity) {
		t.Errorf("err = %v, want *ArityError", err)
	}
}

func TestCommandHintOnAMistypedCommand(t *testing.T) {
	a := parsed(t, "lsit")

	if want := `did you mean the command "scopr list"?`; a.CommandHint() != want {
		t.Errorf("hint = %q, want %q", a.CommandHint(), want)
	}
}

func TestCommandHintStaysQuiet(t *testing.T) {
	for _, argv := range [][]string{
		{"gl-panel"},
		{"run", "lsit"},
		{},
		{"@web"},
	} {
		if got := parsed(t, argv...).CommandHint(); got != "" {
			t.Errorf("Parse %q: hint = %q, want none", argv, got)
		}
	}
}

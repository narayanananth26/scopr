package launch_test

import (
	"strings"
	"testing"

	"scopr/internal/launch"
	"scopr/internal/repo"
	"scopr/internal/scope"
)

func scoped(names ...string) scope.Scope {
	s := scope.Scope{Root: "/w"}
	for _, n := range names {
		s.Repos = append(s.Repos, repo.Repo{Name: n, Path: "/w/" + n})
	}
	return s
}

func TestTitlePrefersName(t *testing.T) {
	got := launch.Title("coupon parity", "why is checkout called twice", scoped("gl-panel"))

	if got != "coupon parity" {
		t.Errorf("Title = %q, want the name", got)
	}
}

func TestTitleFallsBackToPrompt(t *testing.T) {
	got := launch.Title("", "why is checkout called twice", scoped("gl-panel"))

	if got != "why is checkout called twice" {
		t.Errorf("Title = %q, want the prompt", got)
	}
}

func TestTitleFallsBackToRepos(t *testing.T) {
	got := launch.Title("", "", scoped("gl-panel", "gl-extension"))

	if got != "gl-panel gl-extension" {
		t.Errorf("Title = %q, want the repo names", got)
	}
}

func TestTitleTruncates(t *testing.T) {
	got := launch.Title("", strings.Repeat("x", 200), scoped("gl-panel"))

	if len([]rune(got)) > 40 {
		t.Errorf("Title is %d runes, want it cut", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("Title = %q, want it to show it was cut", got)
	}
}

// A prompt is arbitrary text, and a stray escape would break the sequence or
// let the text drive the terminal.
func TestTitleStripsControlCharacters(t *testing.T) {
	got := launch.Title("", "trace\x1b]0;evil\x07 the call\n", scoped("gl-panel"))

	for _, bad := range []string{"\x1b", "\x07", "\n"} {
		if strings.Contains(got, bad) {
			t.Errorf("Title %q still contains a control character", got)
		}
	}
	if !strings.Contains(got, "trace") {
		t.Errorf("Title = %q, want the real words kept", got)
	}
}
func TestInTmuxReadsEnvironment(t *testing.T) {
	t.Setenv("TMUX", "")
	if launch.InTmux() {
		t.Error("InTmux true with TMUX unset")
	}

	t.Setenv("TMUX", "/tmp/tmux-501/default,1,0")
	if !launch.InTmux() {
		t.Error("InTmux false with TMUX set")
	}
}

func TestEnvCarriesTheScope(t *testing.T) {
	cfg := launch.Config{Scope: scoped("gl-panel", "gl-extension"), Name: "coupon parity"}

	got := map[string]string{}
	for _, kv := range launch.Env(cfg) {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(k, "SCOPR_") {
			got[k] = v
		}
	}

	for k, want := range map[string]string{
		"SCOPR_SCOPE":   "gl-panel gl-extension",
		"SCOPR_PRIMARY": "gl-panel",
		"SCOPR_TITLE":   "coupon parity",
	} {
		if got[k] != want {
			t.Errorf("%s = %q, want %q", k, got[k], want)
		}
	}
}

// The session still needs the environment it would otherwise have had.
func TestEnvKeepsTheRest(t *testing.T) {
	t.Setenv("SCOPR_TEST_CANARY", "kept")

	var found bool
	for _, kv := range launch.Env(launch.Config{Scope: scoped("gl-panel")}) {
		if kv == "SCOPR_TEST_CANARY=kept" {
			found = true
		}
	}
	if !found {
		t.Error("Env dropped the inherited environment")
	}
}

// A status line has less room than a tab, so the exported title is shorter.
func TestShortTitleIsShorterThanTheTabLabel(t *testing.T) {
	long := "open the native maps app from a location tile in the web app"

	tab := launch.Title("", long, scoped("web"))
	short := launch.ShortTitle("", long, scoped("web"))

	if len([]rune(short)) >= len([]rune(tab)) {
		t.Errorf("ShortTitle %q is not shorter than Title %q", short, tab)
	}
	if len([]rune(short)) > 28 {
		t.Errorf("ShortTitle is %d runes, want 28 or fewer", len([]rune(short)))
	}
	if !strings.HasSuffix(short, "...") {
		t.Errorf("ShortTitle = %q, want it to show it was cut", short)
	}
}

func TestShortTitleLeavesShortOnesAlone(t *testing.T) {
	if got := launch.ShortTitle("surfaces", "", scoped("web")); got != "surfaces" {
		t.Errorf("ShortTitle = %q, want surfaces untouched", got)
	}
}

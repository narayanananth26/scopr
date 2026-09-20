package launch

import (
	"os"
	"os/exec"
	"strings"

	"scopr/internal/scope"
)

// titleLimit keeps a tab label readable. A tab bar shows a couple of dozen
// characters before truncating on its own terms.
const titleLimit = 40

// envTitleLimit is shorter than a tab label. A status line already carries the
// directory, branch, model and clock, so the title competes for room there in
// a way a tab title does not.
const envTitleLimit = 28

// ShortTitle is Title cut for a status line.
func ShortTitle(name, prompt string, s scope.Scope) string {
	return truncate(Title(name, prompt, s), envTitleLimit)
}

// Title labels the session in the terminal tab.
//
// Given name wins, since it is what the person called the session. Otherwise a
// prompt says what they are doing, and the repositories say where.
func Title(name, prompt string, s scope.Scope) string {
	if t := clean(name); t != "" {
		return truncate(t, titleLimit)
	}

	if t := clean(prompt); t != "" {
		return truncate(t, titleLimit)
	}

	names := make([]string, 0, len(s.Repos))
	for _, r := range s.Repos {
		names = append(names, r.Name)
	}
	return truncate(strings.Join(names, " "), titleLimit)
}

// InTmux reports whether the session is running inside tmux.
func InTmux() bool { return os.Getenv("TMUX") != "" }

// tmuxTitle names the tmux window.
//
// tmux is the only place the label is set. OSC sequences were tried and
// dropped: tmux ignores them outright, and outside tmux a terminal with shell
// integration re-asserts its own title, so the sequence was doing nothing
// where anyone would have seen it. Outside tmux the scope travels in the
// environment instead, for a status line to show.
//
// Asking tmux directly works whatever allow-rename is set to, and turns off
// automatic renaming for the window as a side effect.
func tmuxTitle(title string) error {
	if title == "" {
		return nil
	}
	return exec.Command("tmux", "rename-window", "--", title).Run()
}

// tmuxWindow is the current window name and whether tmux was renaming it.
func tmuxWindow() (name string, auto bool) {
	out, err := exec.Command("tmux", "display-message", "-p", "#W").Output()
	if err != nil {
		return "", false
	}
	name = strings.TrimSpace(string(out))

	mode, err := exec.Command("tmux", "show-window-options", "-v", "automatic-rename").Output()
	if err != nil {
		return name, false
	}
	return name, strings.TrimSpace(string(mode)) == "on"
}

// restoreTmux puts the window name back. Re-enabling automatic renaming is
// enough when tmux was managing the name, since it renames immediately.
func restoreTmux(name string, auto bool) {
	if auto {
		_ = exec.Command("tmux", "set-window-option", "automatic-rename", "on").Run()
		return
	}
	if name != "" {
		_ = tmuxTitle(name)
	}
}

// clean strips the control characters that would break the sequence, since a
// prompt is arbitrary text.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-3]) + "..."
}

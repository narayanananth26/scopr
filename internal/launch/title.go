package launch

import (
	"os"
	"os/exec"
	"strings"

	"scopr/internal/scope"
)

const titleLimit = 28

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

func InTmux() bool { return os.Getenv("TMUX") != "" }

// OSC sequences were tried and dropped: tmux ignores them, and outside tmux a
// terminal with shell integration re-asserts its own title.
func tmuxTitle(title string) error {
	if title == "" {
		return nil
	}
	return exec.Command("tmux", "rename-window", "--", title).Run()
}

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

func restoreTmux(name string, auto bool) {
	if auto {
		_ = exec.Command("tmux", "set-window-option", "automatic-rename", "on").Run()
		return
	}
	if name != "" {
		_ = tmuxTitle(name)
	}
}

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

package ui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// traceLines is how much of the survey's activity stays on screen. The trace
// is context for the wait, not a transcript.
const traceLines = 8

// TraceMsg is one line of survey activity.
type TraceMsg string

// DoneMsg ends the wait. Err is the survey's own failure, kept so the caller
// reports it rather than the spinner.
type DoneMsg struct{ Err error }

// Survey shows a spinner and the survey's activity while it runs.
type Survey struct {
	Task string

	spin    spinner.Model
	lines   []string
	started time.Time
	elapsed time.Duration
	width   int

	// Err is the survey's failure, once finished.
	Err error

	// Done is set when the survey finished, either way.
	Done bool

	// Interrupted is set when the wait was cancelled from the keyboard.
	Interrupted bool
}

func NewSurvey(task string) Survey {
	s := spinner.New()
	s.Spinner = spinner.Dot

	return Survey{
		Task:    task,
		spin:    s,
		started: time.Now(),
	}
}

func (m Survey) Init() tea.Cmd { return tea.Batch(m.spin.Tick, tick()) }

// tick drives the elapsed counter independently of the spinner, so the number
// advances at a readable rate rather than at the frame rate.
func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

type tickMsg struct{}

func (m Survey) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case TraceMsg:
		m.lines = append(m.lines, string(msg))
		if len(m.lines) > traceLines {
			m.lines = m.lines[len(m.lines)-traceLines:]
		}
		return m, nil

	case DoneMsg:
		m.Done = true
		m.Err = msg.Err
		return m, tea.Quit

	case tickMsg:
		m.elapsed = time.Since(m.started).Round(time.Second)
		return m, tick()

	case tea.KeyMsg:
		// Ctrl-C during a 30-second wait must stop it, not be swallowed.
		if msg.String() == "ctrl+c" || msg.String() == "esc" {
			m.Interrupted = true
			return m, tea.Quit
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.spin, cmd = m.spin.Update(msg)
	return m, cmd
}

func (m Survey) View() string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s surveying the workspace", m.spin.View())
	if m.elapsed > 0 {
		b.WriteString(dim.Render(fmt.Sprintf("  %s", m.elapsed)))
	}
	b.WriteString("\n")

	if task := strings.TrimSpace(m.Task); task != "" {
		b.WriteString(dim.Render(clipTo("  "+task, m.width)) + "\n")
	}

	if len(m.lines) > 0 {
		b.WriteString("\n")
		for _, line := range m.lines {
			b.WriteString(dim.Render(clipTo("  "+line, m.width)) + "\n")
		}
	}

	return b.String()
}

// Work is the long operation. It writes progress lines to trace and returns
// the operation's own error.
type Work func(ctx context.Context, trace io.Writer) error

// traceWriter turns writes into TraceMsg, one per line.
type traceWriter struct {
	send func(tea.Msg)
	buf  []byte
}

func (w *traceWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)

	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		if line := strings.TrimSpace(string(w.buf[:i])); line != "" {
			w.send(TraceMsg(line))
		}
		w.buf = w.buf[i+1:]
	}
}

// RunSurvey runs work behind a spinner, returning work's own error.
//
// Interrupting cancels the context and waits, so nothing writes to a torn-down
// program. A stderr that is not a terminal skips the spinner: progress goes
// out plainly rather than as redraw escapes.
func RunSurvey(ctx context.Context, task string, work Work) error {
	if !isTerminal(os.Stderr) {
		fmt.Fprintln(os.Stderr, "surveying the workspace...")
		return work(ctx, os.Stderr)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	prog := tea.NewProgram(NewSurvey(task), tea.WithOutput(os.Stderr))

	done := make(chan error, 1)
	go func() {
		done <- work(ctx, &traceWriter{send: prog.Send})
		prog.Send(DoneMsg{})
	}()

	out, err := prog.Run()
	if err != nil {
		cancel()
		<-done
		return fmt.Errorf("run survey display: %w", err)
	}

	if m, ok := out.(Survey); ok && m.Interrupted {
		cancel()
		<-done
		return ErrCancelled
	}

	return <-done
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

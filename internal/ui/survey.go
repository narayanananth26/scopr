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

const traceLines = 8

type TraceMsg string

type DoneMsg struct{ Err error }

type Survey struct {
	Task string

	spin    spinner.Model
	lines   []string
	started time.Time
	elapsed time.Duration
	width   int

	Err error

	Done bool

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

type Work func(ctx context.Context, trace io.Writer) error

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

package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func send(m Survey, msgs ...tea.Msg) Survey {
	for _, msg := range msgs {
		out, _ := m.Update(msg)
		m = out.(Survey)
	}
	return m
}

func TestSurveyShowsTheTask(t *testing.T) {
	got := NewSurvey("trace the checkout call").View()

	if !strings.Contains(got, "trace the checkout call") {
		t.Errorf("view does not show the task:\n%s", got)
	}
	if !strings.Contains(got, "surveying") {
		t.Errorf("view does not say what it is doing:\n%s", got)
	}
}

func TestSurveyShowsTraceLines(t *testing.T) {
	m := send(NewSurvey("x"), TraceMsg("Grep checkout"), TraceMsg("found it in services/api"))

	got := m.View()
	for _, want := range []string{"Grep checkout", "found it in services/api"} {
		if !strings.Contains(got, want) {
			t.Errorf("view dropped %q:\n%s", want, got)
		}
	}
}

// The trace is context for the wait, not a transcript: it must not grow
// without bound.
func TestSurveyKeepsOnlyRecentTrace(t *testing.T) {
	m := NewSurvey("x")
	for i := range traceLines + 5 {
		m = send(m, TraceMsg(string(rune('a'+i))+" line"))
	}

	if len(m.lines) != traceLines {
		t.Errorf("kept %d lines, want %d", len(m.lines), traceLines)
	}
	if strings.Contains(m.View(), "a line") {
		t.Errorf("oldest line survived:\n%s", m.View())
	}
}

func TestSurveyDoneCarriesError(t *testing.T) {
	want := errors.New("survey blew up")
	m := send(NewSurvey("x"), DoneMsg{Err: want})

	if !m.Done {
		t.Error("DoneMsg did not finish the wait")
	}
	if !errors.Is(m.Err, want) {
		t.Errorf("Err = %v, want %v", m.Err, want)
	}
	if m.Interrupted {
		t.Error("finishing normally reported an interruption")
	}
}

// Ctrl-C during a 30-second wait must stop it rather than be swallowed.
func TestSurveyInterrupts(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyCtrlC, tea.KeyEsc} {
		m := send(NewSurvey("x"), tea.KeyMsg{Type: key})

		if !m.Interrupted {
			t.Errorf("%v did not interrupt the wait", key)
		}
	}
}

func TestSurveyIgnoresOtherKeys(t *testing.T) {
	m := send(NewSurvey("x"), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})

	if m.Interrupted || m.Done {
		t.Error("an ordinary keystroke ended the wait")
	}
}

func TestSurveyShowsElapsed(t *testing.T) {
	m := NewSurvey("x")
	m.started = time.Now().Add(-3 * time.Second)
	m = send(m, tickMsg{})

	if !strings.Contains(m.View(), "3s") {
		t.Errorf("view does not show elapsed time:\n%s", m.View())
	}
}

func TestTraceWriterSplitsLines(t *testing.T) {
	var got []string
	w := &traceWriter{send: func(msg tea.Msg) {
		got = append(got, string(msg.(TraceMsg)))
	}}

	// Split mid-line, as a pipe would deliver it.
	w.Write([]byte("Grep chec"))
	w.Write([]byte("kout\nRead api"))
	w.Write([]byte(".go\n"))

	want := []string{"Grep checkout", "Read api.go"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTraceWriterDropsBlankLines(t *testing.T) {
	var got []string
	w := &traceWriter{send: func(msg tea.Msg) { got = append(got, string(msg.(TraceMsg))) }}

	w.Write([]byte("\n   \nreal line\n"))

	if len(got) != 1 || got[0] != "real line" {
		t.Errorf("got %v, want one real line", got)
	}
}

func TestTruncateCollapsesWhitespace(t *testing.T) {
	if got := truncate("a\t\tb   c", 40); got != "a b c" {
		t.Errorf("truncate = %q, want %q", got, "a b c")
	}
	if got := truncate(strings.Repeat("x", 50), 10); len(got) != 13 {
		t.Errorf("truncate = %q, want it cut to 10 plus an ellipsis", got)
	}
}

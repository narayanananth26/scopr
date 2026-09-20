package ui

import (
	"os"
	"testing"

	"github.com/muesli/termenv"
)

// TestMain forces a colour profile. lipgloss detects one from the terminal and
// strips every style when there is none, so without this the rendering tests
// would assert on plain text and pass against a cursor that never draws.
func TestMain(m *testing.M) {
	renderer.SetColorProfile(termenv.ANSI)
	os.Exit(m.Run())
}

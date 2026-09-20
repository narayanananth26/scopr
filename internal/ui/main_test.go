package ui

import (
	"os"
	"testing"

	"github.com/muesli/termenv"
)

func TestMain(m *testing.M) {
	renderer.SetColorProfile(termenv.ANSI)
	os.Exit(m.Run())
}

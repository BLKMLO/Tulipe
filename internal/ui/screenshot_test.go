package ui

import (
	"os"
	"testing"

	"github.com/blkmlo/tulipe/internal/config"
	"github.com/blkmlo/tulipe/internal/translate"
	tea "github.com/charmbracelet/bubbletea"
)

// TestDumpScreens prints the screens the README shows, so that the excerpts in
// it stay real program output. It writes nothing unless TULIPE_SCREENS is set.
func TestDumpScreens(t *testing.T) {
	if os.Getenv("TULIPE_SCREENS") == "" {
		t.Skip("set TULIPE_SCREENS=1 to print the screens")
	}
	cfg := config.Default()
	m := New(cfg, "")
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = sized.(Model)
	t.Log("\n" + m.View())

	m.run = &runState{progress: translate.Progress{
		Current: 2,
		Documents: []translate.DocState{
			{Title: "I. Le retour au pays", Status: translate.StatusDone, Translated: 47, TotalSegments: 47},
			{Title: "II. La lettre", Status: translate.StatusDone, Translated: 61, TotalSegments: 62, Notes: 1},
			{Title: "III. Sous les tilleuls", Status: translate.StatusRunning, DoneSegments: 23, TotalSegments: 58},
			{Title: "IV. L'hiver", Status: translate.StatusPending},
			{Title: "V. Le départ", Status: translate.StatusPending},
		},
	}}
	m.screen = screenRun
	t.Log("\n" + m.View())
}

package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blkmlo/tulipe/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	var out tea.Model = m
	for _, k := range keys {
		out, _ = out.(Model).Update(key(k))
	}
	return out.(Model)
}

func newTestModel() Model {
	m := New(config.Default(), "")
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return sized.(Model)
}

func TestMenuRendersAndNavigates(t *testing.T) {
	m := newTestModel()
	view := m.View()
	for _, want := range []string{"Tulipe", "Traduire un EPUB", "Reprendre une traduction", "Réglages", "Quitter"} {
		if !strings.Contains(view, want) {
			t.Errorf("the menu does not show %q", want)
		}
	}
	if !strings.Contains(view, config.Default().Model) {
		t.Error("the menu does not show the configured model")
	}

	m = press(t, m, "down")
	if m.menuIndex != 1 {
		t.Errorf("menuIndex = %d after one 'down', want 1", m.menuIndex)
	}
	m = press(t, m, "up", "up")
	if m.menuIndex != len(m.menu)-1 {
		t.Errorf("menuIndex = %d, want the selection to wrap to the last entry", m.menuIndex)
	}
}

func TestEnterOpensSettings(t *testing.T) {
	m := newTestModel()
	m = press(t, m, "down", "down", "enter")
	if m.screen != screenSettings {
		t.Fatalf("screen = %v, want the settings screen", m.screen)
	}
	if !strings.Contains(m.View(), "Fournisseur") {
		t.Error("the settings screen does not show the provider field")
	}
}

func TestSettingsCycleProviderAndRevealRelevantFields(t *testing.T) {
	m := newTestModel()
	m = press(t, m, "down", "down", "enter") // settings

	// The effort field only concerns the Anthropic backend.
	if !strings.Contains(m.View(), "Effort") {
		t.Error("Effort should be offered for the Anthropic provider")
	}
	m = press(t, m, "right") // cycle provider -> openai-compatible
	if m.settings.cfg.Provider != config.ProviderOpenAI {
		t.Fatalf("provider = %q, want %q", m.settings.cfg.Provider, config.ProviderOpenAI)
	}
	if strings.Contains(m.View(), "Effort") {
		t.Error("Effort must be hidden for an OpenAI-compatible provider")
	}
	if !strings.Contains(m.View(), "modifications non enregistrées") {
		t.Error("an unsaved change must be signalled")
	}
}

func TestSettingsRejectInvalidNumber(t *testing.T) {
	m := newTestModel()
	m = press(t, m, "down", "down", "enter")

	// Walk to "Caractères par requête".
	for i := 0; i < 30 && m.settings.fields[m.settings.visible()[m.settings.index]].label != "Caractères par requête"; i++ {
		m = press(t, m, "down")
	}
	m = press(t, m, "enter") // start editing
	if !m.settings.editing {
		t.Fatal("the field did not enter edit mode")
	}
	m.settings.input.SetValue("beaucoup")
	m = press(t, m, "enter")
	if m.failure == "" {
		t.Error("a non-numeric value must be refused with a message")
	}
	if m.settings.cfg.ChunkChars != config.Default().ChunkChars {
		t.Error("a refused value must not be applied")
	}
}

func TestEscapeGuardsUnsavedSettings(t *testing.T) {
	m := newTestModel()
	m = press(t, m, "down", "down", "enter", "right")
	m = press(t, m, "esc")
	if m.screen != screenSettings {
		t.Error("the first escape must warn instead of discarding unsaved changes")
	}
	m = press(t, m, "esc")
	if m.screen != screenMenu {
		t.Error("the second escape must return to the menu")
	}
}

func TestOutputPathDerivation(t *testing.T) {
	cfg := config.Default()
	got := outputPath(cfg, filepath.Join("/livres", "1984.epub"))
	want := filepath.Join("/livres", "1984.fr.epub")
	if got != want {
		t.Errorf("outputPath = %q, want %q", got, want)
	}

	cfg.TargetCode = ""
	cfg.TargetLanguage = "português (Brasil)"
	cfg.OutputDir = "/sortie"
	got = outputPath(cfg, "/livres/1984.epub")
	want = filepath.Join("/sortie", "1984.portugues-brasil.epub")
	if got != want {
		t.Errorf("outputPath without a code = %q, want %q", got, want)
	}
}

func TestFreeNameNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.epub")
	if got := freeName(first); got != first {
		t.Errorf("freeName on a free path = %q, want %q", got, first)
	}
	if err := writeEmpty(first); err != nil {
		t.Fatal(err)
	}
	got := freeName(first)
	want := filepath.Join(dir, "a.2.epub")
	if got != want {
		t.Errorf("freeName on an existing path = %q, want %q", got, want)
	}
}

func TestPickerEscapeReturnsToMenu(t *testing.T) {
	m := newTestModel()
	m = press(t, m, "enter") // "Traduire un EPUB"
	if m.screen != screenPicker {
		t.Fatalf("screen = %v, want the file picker", m.screen)
	}
	m = press(t, m, "esc")
	if m.screen != screenMenu {
		t.Error("escape must leave the file picker")
	}
}

func TestResumeScreenExplainsAnEmptyCache(t *testing.T) {
	m := newTestModel()
	m = press(t, m, "down", "enter")
	updated, _ := m.Update(runsLoadedMsg{})
	m = updated.(Model)
	if !strings.Contains(m.View(), "Aucune traduction en cache") {
		t.Error("an empty resume list must explain itself")
	}
}

func TestBookLoadFailureIsReported(t *testing.T) {
	m := newTestModel()
	updated, _ := m.Update(bookLoadedMsg{path: "x.epub", err: errFake})
	m = updated.(Model)
	if m.screen != screenMenu {
		t.Error("a failed load must return to the menu")
	}
	if !strings.Contains(m.View(), "pas un EPUB") {
		t.Errorf("the failure is not shown to the user:\n%s", m.View())
	}
}

var errFake = fakeErr("ce fichier n'est pas un EPUB")

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

func writeEmpty(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	return f.Close()
}

func TestSlugFoldsAccentsAndNeverReturnsEmpty(t *testing.T) {
	cases := map[string]string{
		"français":           "francais",
		"português (Brasil)": "portugues-brasil",
		"español":            "espanol",
		"日本語":                "traduit",
		"":                   "traduit",
	}
	for in, want := range cases {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

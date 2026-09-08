package ui

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blkmlo/tulipe/internal/config"
	"github.com/blkmlo/tulipe/internal/epub"
	"github.com/blkmlo/tulipe/internal/llm"
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
	if !strings.Contains(m.View(), "Service") {
		t.Error("the settings screen does not show the service field")
	}
}

func TestSettingsCycleServiceAndRevealRelevantFields(t *testing.T) {
	m := newTestModel()
	m = press(t, m, "down", "down", "enter") // settings

	// Effort only concerns the Anthropic backend.
	if m.settings.cfg.Provider != config.ProviderAnthropic {
		t.Fatalf("the default service is %q", m.settings.cfg.Provider)
	}
	if !strings.Contains(m.View(), "Effort") {
		t.Error("Effort should be offered for the Anthropic backend")
	}

	m = press(t, m, "right") // next service in the catalogue
	if m.settings.cfg.Provider == config.ProviderAnthropic {
		t.Fatal("the service did not change")
	}
	if strings.Contains(m.View(), "Effort") {
		t.Error("Effort must be hidden for a service that does not have it")
	}
	if !strings.Contains(m.View(), "modifications non enregistrées") {
		t.Error("an unsaved change must be signalled")
	}
	// Picking a service must bring its endpoint along, or the user has to
	// look it up by hand.
	preset, ok := llm.LookupPreset(m.settings.cfg.Provider)
	if !ok {
		t.Fatalf("unknown service %q", m.settings.cfg.Provider)
	}
	if m.settings.cfg.BaseURL != preset.BaseURL {
		t.Errorf("BaseURL = %q, want the preset's %q", m.settings.cfg.BaseURL, preset.BaseURL)
	}
}

func TestCyclingServicesNeverLandsOnAnInvalidOne(t *testing.T) {
	m := newTestModel()
	m = press(t, m, "down", "down", "enter")
	seen := map[string]bool{}
	for i := 0; i < len(llm.PresetIDs())+1; i++ {
		id := m.settings.cfg.Provider
		if _, ok := llm.LookupPreset(id); !ok {
			t.Fatalf("cycling produced the unknown service %q", id)
		}
		seen[id] = true
		m = press(t, m, "right")
	}
	if len(seen) != len(llm.PresetIDs()) {
		t.Errorf("cycling visited %d services out of %d", len(seen), len(llm.PresetIDs()))
	}
}

func TestAHandTypedEndpointSurvivesAServiceChange(t *testing.T) {
	m := newTestModel()
	m.settings.cfg.Provider = config.ProviderOpenAI
	m.settings.cfg.BaseURL = "http://localhost:9999/v1"
	m = press(t, m, "down", "down", "enter")
	m.settings.cfg.Provider = config.ProviderOpenAI
	m.settings.cfg.BaseURL = "http://localhost:9999/v1"
	m = press(t, m, "right")
	if m.settings.cfg.BaseURL != "http://localhost:9999/v1" {
		t.Errorf("BaseURL = %q; an endpoint the user typed must not be overwritten", m.settings.cfg.BaseURL)
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

func TestModelPickerFiltersAndSelects(t *testing.T) {
	m := newTestModel()
	m = press(t, m, "down", "down", "enter") // settings
	updated, _ := m.Update(modelsLoadedMsg{
		provider: "groq",
		models:   []string{"alpha-8b", "beta-70b", "gamma-8b"},
	})
	m = updated.(Model)
	m.screen = screenModels

	view := m.View()
	for _, want := range []string{"alpha-8b", "beta-70b", "3 modèle(s)"} {
		if !strings.Contains(view, want) {
			t.Errorf("the picker does not show %q", want)
		}
	}

	m = press(t, m, "7") // filter
	if shown := m.shownModels(); len(shown) != 1 || shown[0] != "beta-70b" {
		t.Fatalf("filtering on \"7\" gives %q, want [beta-70b]", shown)
	}
	m = press(t, m, "enter")
	if m.settings.cfg.Model != "beta-70b" {
		t.Errorf("Model = %q, want beta-70b", m.settings.cfg.Model)
	}
	if m.screen != screenSettings {
		t.Error("choosing a model must return to the settings")
	}
	if !m.settings.dirty {
		t.Error("choosing a model must mark the settings as unsaved")
	}
}

func TestModelPickerExplainsAnEmptyList(t *testing.T) {
	m := newTestModel()
	updated, _ := m.Update(modelsLoadedMsg{provider: "x"})
	m = updated.(Model)
	m.screen = screenModels
	if !strings.Contains(m.View(), "Aucun modèle listé") {
		t.Error("an empty list must explain itself")
	}
}

func TestOutputPathFollowsTheFormat(t *testing.T) {
	cfg := config.Default()
	if got := outputPath(cfg, "/livres/1984.epub"); got != filepath.Join("/livres", "1984.fr.epub") {
		t.Errorf("outputPath = %q", got)
	}
	cfg.Format = config.FormatText
	if got := outputPath(cfg, "/livres/1984.epub"); got != filepath.Join("/livres", "1984.fr.txt") {
		t.Errorf("outputPath in text mode = %q, want a .txt file", got)
	}
}

func TestWriteBookHonoursTheFormat(t *testing.T) {
	dir := t.TempDir()
	book := testBookForUI(t)

	epubPath := filepath.Join(dir, "livre.fr.epub")
	if _, err := writeBook(book, epubPath, config.FormatEPUB); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(epubPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := epub.Parse(raw); err != nil {
		t.Errorf("the EPUB export is not readable: %v", err)
	}

	txtPath := filepath.Join(dir, "livre.fr.txt")
	if _, err := writeBook(book, txtPath, config.FormatText); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(txtPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(text), "<") || !strings.Contains(string(text), "bright cold day") {
		t.Errorf("the text export is not plain prose:\n%s", text)
	}
}

// testBookForUI builds a small EPUB so that the export paths can be exercised
// without a fixture file on disk.
func testBookForUI(t *testing.T) *epub.Book {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) {
		t.Helper()
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	add("mimetype", "application/epub+zip")
	add("META-INF/container.xml", `<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="c.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`)
	add("c.opf", `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Un livre</dc:title><dc:language>en</dc:language></metadata><manifest><item id="a" href="a.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="a"/></spine></package>`)
	add("a.xhtml", `<?xml version="1.0" encoding="utf-8"?><html xmlns="http://www.w3.org/1999/xhtml"><body><h1>Chapitre</h1><p>It was a bright cold day.</p></body></html>`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	book, err := epub.Parse(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return book
}

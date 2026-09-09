package ui

import (
	"errors"
	"testing"

	"github.com/blkmlo/tulipe/internal/config"
	"github.com/blkmlo/tulipe/internal/translate"
	tea "github.com/charmbracelet/bubbletea"
)

// everyKey is the alphabet a user can actually produce, plus the special keys
// the model reads by name.
func everyKey() []tea.KeyMsg {
	var keys []tea.KeyMsg
	for _, t := range []tea.KeyType{
		tea.KeyEnter, tea.KeyEsc, tea.KeyUp, tea.KeyDown, tea.KeyLeft, tea.KeyRight,
		tea.KeyBackspace, tea.KeyTab, tea.KeySpace, tea.KeyDelete, tea.KeyHome, tea.KeyEnd,
		tea.KeyPgUp, tea.KeyPgDown, tea.KeyCtrlA, tea.KeyCtrlE, tea.KeyCtrlN, tea.KeyCtrlP,
		tea.KeyCtrlU, tea.KeyCtrlW,
	} {
		keys = append(keys, tea.KeyMsg{Type: t})
	}
	for _, r := range "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 -_/\\.,;:!?éàü€\"'`~$*#@%&()[]{}<>|+=" {
		keys = append(keys, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return keys
}

// everyScreen builds a model parked on each screen, in the states a user can
// reach — including the empty ones, which is where indexing goes wrong.
func everyScreen(t *testing.T) map[string]Model {
	t.Helper()

	states := map[string]Model{}

	base := newTestModel()
	states["menu"] = base

	picker := newTestModel()
	picker.screen = screenPicker
	states["picker"] = picker

	settings := newTestModel()
	settings.screen = screenSettings
	states["settings"] = settings

	editing := newTestModel()
	editing.screen = screenSettings
	editing.settings.editing = true
	states["settings-en-saisie"] = editing

	withBook := newTestModel()
	withBook.screen = screenBook
	withBook.book = testBookForUI(t)
	withBook.bookPath = "/livres/x.epub"
	withBook.stats = bookStats{Documents: 1, Segments: 4, Chars: 100}
	states["livre"] = withBook

	withPending := withBook
	withPending.pending = 3
	withPending.cacheDir = t.TempDir()
	states["livre-avec-attente"] = withPending

	noBook := newTestModel()
	noBook.screen = screenBook
	states["livre-sans-livre"] = noBook

	emptyRuns := newTestModel()
	emptyRuns.screen = screenResume
	states["reprise-vide"] = emptyRuns

	withRuns := newTestModel()
	withRuns.screen = screenResume
	withRuns.runs = []translate.RunInfo{
		{Dir: t.TempDir(), Source: "/nexiste/pas.epub", BookTitle: "A", TargetLanguage: "fr"},
		{Dir: t.TempDir(), Source: "/nexiste/pas2.epub", BookTitle: "B", TargetLanguage: "es"},
	}
	states["reprise-avec-travaux"] = withRuns

	// The dangerous one: an index left past the end of a shrunken list.
	staleIndex := withRuns
	staleIndex.runsIndex = 7
	states["reprise-index-perime"] = staleIndex

	noModels := newTestModel()
	noModels.screen = screenModels
	states["modeles-vide"] = noModels

	withModels := newTestModel()
	withModels.screen = screenModels
	withModels.models = []string{"alpha", "beta", "gamma"}
	states["modeles"] = withModels

	staleModel := withModels
	staleModel.modelsIndex = 9
	staleModel.modelsFilter = "zzz"
	states["modeles-index-perime"] = staleModel

	report := newTestModel()
	report.screen = screenReport
	report.book = testBookForUI(t)
	report.result = &translate.Result{Documents: []translate.DocState{
		{Title: "I", Status: translate.StatusDone, Pending: 2, Notes: 2, Err: errors.New("x")},
	}}
	states["rapport"] = report

	emptyReport := newTestModel()
	emptyReport.screen = screenReport
	states["rapport-sans-resultat"] = emptyReport

	running := newTestModel()
	running.screen = screenRun
	running.run = &runState{cancel: func() {}, progress: translate.Progress{}}
	states["en-cours-sans-document"] = running

	orphan := newTestModel()
	orphan.screen = screenRun // no run attached: nothing should reach for it
	states["en-cours-sans-execution"] = orphan

	return states
}

// TestNoKeyCrashesAnyScreen is the blunt instrument: every key, on every
// screen, in every state. A user mashing the keyboard must never bring the
// program down.
func TestNoKeyCrashesAnyScreen(t *testing.T) {
	for name, start := range everyScreen(t) {
		for _, key := range everyKey() {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("écran %s, touche %q : %v", name, key.String(), r)
					}
				}()
				next, _ := start.Update(key)
				// The view must render too: half the indexing lives there.
				_ = next.(Model).View()
			}()
		}
	}
}

// TestKeySequencesDoNotCrash walks longer paths, where state accumulates.
func TestKeySequencesDoNotCrash(t *testing.T) {
	sequences := [][]string{
		{"enter", "esc", "enter", "esc"},
		{"down", "down", "enter", "d", "d", "d", "enter"},
		{"down", "down", "enter", "right", "right", "enter", "esc", "esc", "esc"},
		{"down", "down", "enter", "m", "esc", "esc"},
		{"down", "down", "enter", "s", "esc"},
		{"j", "j", "j", "j", "j", "k", "k", "enter", "esc"},
		{"p", "p", "p"},
		{"down", "enter", "up", "up", "down", "down", "d", "enter", "esc"},
	}
	for _, seq := range sequences {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("séquence %v : %v", seq, r)
				}
			}()
			m := newTestModel()
			var model tea.Model = m
			for _, k := range seq {
				model, _ = model.Update(key(k))
				_ = model.(Model).View()
			}
		}()
	}
}

// TestOddWindowSizesDoNotCrash covers a terminal squeezed to nothing.
func TestOddWindowSizesDoNotCrash(t *testing.T) {
	sizes := []tea.WindowSizeMsg{
		{Width: 0, Height: 0}, {Width: 1, Height: 1}, {Width: 5, Height: 2},
		{Width: 10, Height: 3}, {Width: 500, Height: 200}, {Width: -1, Height: -1},
	}
	for name, start := range everyScreen(t) {
		for _, size := range sizes {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("écran %s, taille %dx%d : %v", name, size.Width, size.Height, r)
					}
				}()
				next, _ := start.Update(size)
				_ = next.(Model).View()
			}()
		}
	}
}

// TestOutOfOrderMessagesDoNotCrash: nothing guarantees the order in which
// messages reach the model.
func TestOutOfOrderMessagesDoNotCrash(t *testing.T) {
	msgs := []tea.Msg{
		bookLoadedMsg{path: "x.epub", err: errors.New("cassé")},
		bookLoadedMsg{path: "x.epub"},
		progressMsg(translate.Progress{}),
		finishedMsg{},
		finishedMsg{err: errors.New("cassé")},
		runsLoadedMsg{err: errors.New("cassé")},
		modelsLoadedMsg{err: errors.New("cassé")},
		modelsLoadedMsg{models: []string{"a"}},
		testResultMsg{err: errors.New("cassé")},
		testResultMsg{sample: "ok"},
		tickMsg{},
	}
	for name, start := range everyScreen(t) {
		for _, msg := range msgs {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("écran %s, message %T : %v", name, msg, r)
					}
				}()
				next, _ := start.Update(msg)
				_ = next.(Model).View()
			}()
		}
	}
	_ = config.Default
}

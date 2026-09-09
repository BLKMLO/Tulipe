// Package ui is Tulipe's interactive terminal interface.
package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blkmlo/tulipe/internal/config"
	"github.com/blkmlo/tulipe/internal/epub"
	"github.com/blkmlo/tulipe/internal/i18n"
	"github.com/blkmlo/tulipe/internal/llm"
	"github.com/blkmlo/tulipe/internal/translate"
	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type screen int

const (
	screenMenu screen = iota
	screenPicker
	screenBook
	screenRun
	screenReport
	screenSettings
	screenResume
	screenModels
)

type menuEntry struct {
	label  string
	detail string
	action func(*Model) tea.Cmd
}

// Model is the root Bubble Tea model.
type Model struct {
	cfg    config.Config
	screen screen
	width  int
	height int

	menu      []menuEntry
	menuIndex int

	picker  filepicker.Model
	spin    spinner.Model
	bar     progress.Model
	loading bool

	bookPath string
	book     *epub.Book
	stats    bookStats
	outPath  string
	cacheDir string
	reusable int
	// pending is how many passages an earlier run left in the source language
	// for this book; retryOnly asks the next run to retry just those.
	pending   int
	retryOnly bool

	run      *runState
	result   *translate.Result
	elapsed  time.Duration
	notice   string
	noticeOK bool
	failure  string

	settings settingsForm

	runs      []translate.RunInfo
	runsIndex int

	models       []string
	modelsIndex  int
	modelsFilter string
}

type bookStats struct {
	Documents int
	Segments  int
	Chars     int
}

type runState struct {
	cancel   context.CancelFunc
	events   chan tea.Msg
	progress translate.Progress
	started  time.Time
	log      []string
}

// New builds the interface. startPath, when not empty, is an EPUB to load
// straight away.
func New(cfg config.Config, startPath string) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = accentStyle

	fp := filepicker.New()
	fp.AllowedTypes = []string{".epub"}
	fp.ShowPermissions = false
	fp.CurrentDirectory = startDirectory(startPath)
	fp.Styles.Selected = lipgloss.NewStyle().Foreground(petal).Bold(true)
	fp.Styles.Cursor = accentStyle

	m := Model{
		cfg:    cfg,
		screen: screenMenu,
		picker: fp,
		spin:   sp,
		bar:    progress.New(progress.WithScaledGradient("#F783AC", "#69DB7C")),
	}
	m.menu = buildMenu()
	m.settings = newSettingsForm(cfg)
	if startPath != "" {
		m.bookPath = startPath
	}
	return m
}

func startDirectory(startPath string) string {
	if startPath != "" {
		if dir := filepath.Dir(startPath); dir != "" {
			return dir
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	home, _ := os.UserHomeDir()
	return home
}

func buildMenu() []menuEntry {
	return []menuEntry{
		{i18n.T("ui.menu.translate"), i18n.T("ui.menu.translate.detail"), func(m *Model) tea.Cmd {
			m.screen = screenPicker
			return m.picker.Init()
		}},
		{i18n.T("ui.menu.resume"), i18n.T("ui.menu.resume.detail"), func(m *Model) tea.Cmd {
			m.screen = screenResume
			return loadRuns
		}},
		{i18n.T("ui.menu.settings"), i18n.T("ui.menu.settings.detail"), func(m *Model) tea.Cmd {
			m.settings = newSettingsForm(m.cfg)
			m.screen = screenSettings
			return nil
		}},
		{i18n.T("ui.menu.test"), i18n.T("ui.menu.test.detail"), func(m *Model) tea.Cmd {
			m.loading = true
			m.notice, m.failure = "", ""
			return tea.Batch(m.spin.Tick, testConnection(m.cfg))
		}},
		{i18n.T("ui.menu.quit"), "", func(m *Model) tea.Cmd { return tea.Quit }},
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	if m.bookPath != "" {
		m.loading = true
		return tea.Batch(m.spin.Tick, loadBook(m.bookPath))
	}
	return nil
}

// -- messages ---------------------------------------------------------------

type bookLoadedMsg struct {
	path  string
	book  *epub.Book
	stats bookStats
	err   error
}

type progressMsg translate.Progress

type finishedMsg struct {
	result *translate.Result
	out    string
	err    error
}

type testResultMsg struct {
	sample string
	usage  llm.Usage
	model  string
	err    error
}

type runsLoadedMsg struct {
	runs []translate.RunInfo
	err  error
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func loadBook(path string) tea.Cmd {
	return func() tea.Msg {
		book, err := epub.Open(path)
		if err != nil {
			return bookLoadedMsg{path: path, err: err}
		}
		var st bookStats
		for _, name := range book.TranslatableDocuments() {
			doc, ok := book.Read(name)
			if !ok {
				continue
			}
			st.Documents++
			segs, err := epub.Extract(doc)
			if err != nil {
				continue
			}
			st.Segments += len(segs)
			for _, s := range segs {
				st.Chars += len(s.Source)
			}
		}
		return bookLoadedMsg{path: path, book: book, stats: st}
	}
}

func loadRuns() tea.Msg {
	runs, err := translate.ListRuns(translate.CacheRoot())
	return runsLoadedMsg{runs: runs, err: err}
}

func testConnection(cfg config.Config) tea.Cmd {
	return func() tea.Msg {
		if err := cfg.Validate(); err != nil {
			return testResultMsg{err: err}
		}
		p, err := cfg.NewProvider()
		if err != nil {
			return testResultMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		resp, err := p.Complete(ctx, llm.Request{
			System:    fmt.Sprintf("Translate into %s. Answer with {\"translations\": [...]} and nothing else.", cfg.TargetLanguage),
			User:      "Translate these 1 segments.\n\n[\"The quick brown fox.\"]",
			MaxTokens: 1024,
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"translations": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
				"required":   []string{"translations"},
			},
		})
		if err != nil {
			return testResultMsg{err: err}
		}
		return testResultMsg{sample: strings.TrimSpace(resp.Text), usage: resp.Usage, model: resp.Model}
	}
}

// -- update -----------------------------------------------------------------

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.picker.SetHeight(max(6, msg.Height-12))
		m.bar.Width = clamp(msg.Width-16, 20, 60)
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tickMsg:
		if m.run != nil {
			m.elapsed = time.Since(m.run.started)
			return m, tick()
		}
		return m, nil

	case bookLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.failure = msg.err.Error()
			m.screen = screenMenu
			return m, nil
		}
		m.bookPath, m.book, m.stats = msg.path, msg.book, msg.stats
		m.outPath = outputPath(m.cfg, msg.path)
		m.reusable, m.cacheDir = m.inspectCache()
		m.pending = m.pendingCount()
		m.screen = screenBook
		return m, nil

	case modelsLoadedMsg:
		m.loading = false
		m.models, m.modelsIndex, m.modelsFilter = msg.models, 0, ""
		if msg.err != nil {
			m.failure = msg.err.Error()
		}
		return m, nil

	case runsLoadedMsg:
		m.runs, m.runsIndex = msg.runs, 0
		if msg.err != nil {
			m.failure = msg.err.Error()
		}
		return m, nil

	case testResultMsg:
		m.loading = false
		if msg.err != nil {
			m.failure = msg.err.Error()
			return m, nil
		}
		m.noticeOK = true
		m.notice = i18n.T("ui.test.ok", msg.model, truncate(msg.sample, 90))
		if msg.usage.Reported {
			m.notice += i18n.T("ui.test.usage", msg.usage.InputTokens, msg.usage.OutputTokens)
		} else {
			m.notice += i18n.T("ui.test.no-usage")
		}
		return m, nil

	case progressMsg:
		if m.run == nil {
			return m, nil
		}
		p := translate.Progress(msg)
		m.run.progress = p
		if p.Retry != nil {
			m.run.appendLog(warnStyle.Render(i18n.T("ui.run.retry", p.Retry.Attempt, p.Retry.Wait.Round(time.Second), p.Retry.Err)))
		} else if p.Message != "" {
			m.run.appendLog(dimStyle.Render(p.Message))
		}
		return m, listen(m.run.events)

	case finishedMsg:
		m.run = nil
		m.result = msg.result
		m.screen = screenReport
		if msg.err != nil {
			m.failure = msg.err.Error()
		} else {
			m.failure = ""
			m.outPath = msg.out
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	if m.screen == screenPicker {
		var cmd tea.Cmd
		m.picker, cmd = m.picker.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		if m.run != nil {
			m.run.cancel()
			return m, nil
		}
		return m, tea.Quit
	}
	m.notice, m.failure = "", ""

	switch m.screen {
	case screenMenu:
		return m.updateMenu(msg)
	case screenPicker:
		return m.updatePicker(msg)
	case screenBook:
		return m.updateBook(msg)
	case screenRun:
		if m.run == nil {
			// Nothing is running: the screen is a leftover, let it be left.
			m.screen = screenMenu
			return m, nil
		}
		if msg.String() == "esc" {
			m.run.cancel()
			m.run.appendLog(warnStyle.Render(i18n.T("ui.run.cancelling")))
		}
		return m, nil
	case screenReport:
		switch msg.String() {
		case "p":
			if m.result == nil || !m.result.Retryable() || m.book == nil {
				return m, nil
			}
			m.retryOnly = true
			m.result = nil
			return m.startRun()
		case "esc", "enter", "q":
			m.screen = screenMenu
			m.result = nil
		}
		return m, nil
	case screenSettings:
		return m.updateSettings(msg)
	case screenResume:
		return m.updateResume(msg)
	case screenModels:
		return m.updateModels(msg)
	}
	return m, nil
}

func (m Model) updateMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		m.menuIndex = (m.menuIndex - 1 + len(m.menu)) % len(m.menu)
	case "down", "j":
		m.menuIndex = (m.menuIndex + 1) % len(m.menu)
	case "enter":
		cmd := m.menu[m.menuIndex].action(&m)
		return m, cmd
	}
	return m, nil
}

func (m Model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		m.screen = screenMenu
		return m, nil
	}
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	if ok, path := m.picker.DidSelectFile(msg); ok {
		m.loading = true
		return m, tea.Batch(m.spin.Tick, loadBook(path))
	}
	return m, cmd
}

func (m Model) updateBook(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.screen = screenMenu
		m.book = nil
		return m, nil
	case "r":
		if m.cacheDir != "" {
			_ = os.RemoveAll(m.cacheDir)
			m.reusable, m.cacheDir = m.inspectCache()
			m.pending = m.pendingCount()
			m.notice, m.noticeOK = i18n.T("ui.book.cache-cleared"), true
		}
		return m, nil
	case "p":
		if m.pending == 0 {
			m.notice, m.noticeOK = i18n.T("ui.book.nothing-pending"), false
			return m, nil
		}
		m.retryOnly = true
		return m.startRun()
	case "enter":
		m.retryOnly = false
		return m.startRun()
	}
	return m, nil
}

func (m Model) updateResume(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A list can shrink under the cursor — a deleted entry, a reload that
	// returns fewer. Clamping here means no branch below has to remember.
	m.runsIndex = clampIndex(m.runsIndex, len(m.runs))

	switch msg.String() {
	case "esc":
		m.screen = screenMenu
	case "up", "k":
		if m.runsIndex > 0 {
			m.runsIndex--
		}
	case "down", "j":
		if m.runsIndex < len(m.runs)-1 {
			m.runsIndex++
		}
	case "d":
		if len(m.runs) > 0 {
			_ = os.RemoveAll(m.runs[m.runsIndex].Dir)
			return m, loadRuns
		}
	case "enter":
		if len(m.runs) == 0 {
			return m, nil
		}
		r := m.runs[m.runsIndex]
		if _, err := os.Stat(r.Source); err != nil {
			m.failure = i18n.T("ui.resume.missing", r.Source)
			return m, nil
		}
		m.cfg.TargetLanguage = r.TargetLanguage
		m.loading = true
		return m, tea.Batch(m.spin.Tick, loadBook(r.Source))
	}
	return m, nil
}

// clampIndex keeps a selection inside a list, and returns 0 for an empty one.
func clampIndex(i, length int) int {
	if length <= 0 {
		return 0
	}
	if i < 0 {
		return 0
	}
	if i >= length {
		return length - 1
	}
	return i
}

func (s *runState) appendLog(line string) {
	if line == "" {
		return
	}
	s.log = append(s.log, line)
	if len(s.log) > 200 {
		s.log = s.log[len(s.log)-200:]
	}
}

func listen(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// inspectCache reports how many documents of this book are already translated.
func (m Model) inspectCache() (int, string) {
	if !m.cfg.Resume || m.bookPath == "" {
		return 0, ""
	}
	fp, err := translate.Fingerprint(m.bookPath)
	if err != nil {
		return 0, ""
	}
	cache, err := translate.OpenCache(translate.CacheRoot(), fp, m.cfg.Recipe(), translate.RunInfo{
		Source: m.bookPath, BookTitle: m.bookTitle(), TargetLanguage: m.cfg.TargetLanguage, Model: m.cfg.Model,
	})
	if err != nil {
		return 0, ""
	}
	n := 0
	if m.book != nil {
		for _, name := range m.book.TranslatableDocuments() {
			if _, ok := cache.Get(name); ok {
				n++
			}
		}
	}
	return n, cache.Dir()
}

// pendingInCache counts the passages an earlier run left in the source
// language for this book.
func (m Model) pendingCount() int {
	if m.cacheDir == "" {
		return 0
	}
	return translate.RunInfo{Dir: m.cacheDir}.Pending()
}

func (m Model) bookTitle() string {
	if m.book != nil && m.book.Title != "" {
		return m.book.Title
	}
	return filepath.Base(m.bookPath)
}

// startRun kicks off the translation in a goroutine and switches to the live
// progress screen.
func (m Model) startRun() (tea.Model, tea.Cmd) {
	if m.book == nil || m.bookPath == "" {
		m.failure = i18n.T("ui.book.not-loaded")
		m.screen = screenMenu
		return m, nil
	}
	if err := m.cfg.Validate(); err != nil {
		m.failure = err.Error()
		return m, nil
	}
	provider, err := m.cfg.NewProvider()
	if err != nil {
		m.failure = err.Error()
		return m, nil
	}
	if err := checkWritable(m.outPath); err != nil {
		m.failure = err.Error()
		return m, nil
	}

	opts := translate.BookOptions{Options: m.cfg.TranslateOptions(), RetryPending: m.retryOnly}
	if m.cfg.Resume {
		fp, err := translate.Fingerprint(m.bookPath)
		if err == nil {
			cache, err := translate.OpenCache(translate.CacheRoot(), fp, m.cfg.Recipe(), translate.RunInfo{
				Source:         m.bookPath,
				BookTitle:      m.bookTitle(),
				TargetLanguage: m.cfg.TargetLanguage,
				Model:          m.cfg.Model,
				Documents:      m.stats.Documents,
			})
			if err == nil {
				opts.Cache = cache
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan tea.Msg, 64)
	book := m.book
	out := m.outPath

	m.run = &runState{cancel: cancel, events: events, started: time.Now()}
	m.screen = screenRun
	m.elapsed = 0

	go func() {
		defer cancel()
		res, err := translate.Book(ctx, provider, book, opts, func(p translate.Progress) {
			select {
			case events <- progressMsg(p):
			default: // the display is behind; the next event carries the same state
			}
		})
		if err != nil {
			events <- finishedMsg{result: res, err: err}
			return
		}
		final, err := writeBook(book, out, m.cfg.Format)
		events <- finishedMsg{result: res, out: final, err: err}
	}()

	return m, tea.Batch(listen(events), m.spin.Tick, tick())
}

// writeBook saves the translated book without ever overwriting an existing
// file: a suffix is added until the name is free.
func writeBook(book *epub.Book, path, format string) (string, error) {
	final := freeName(path)
	if format == config.FormatText {
		if err := os.WriteFile(final, []byte(book.PlainText()), 0o644); err != nil {
			return "", err
		}
		return final, nil
	}
	if err := book.WriteFile(final); err != nil {
		return "", err
	}
	return final, nil
}

// checkWritable proves the destination can be written before a single token is
// spent. Discovering a bad path after translating three hundred pages would be
// the worst possible moment.
func checkWritable(path string) error {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return fmt.Errorf(i18n.T("cli.err.is-a-directory"), path)
	}
	dir := filepath.Dir(path)
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf(i18n.T("cli.err.output-dir"), err)
	}
	if !info.IsDir() {
		return fmt.Errorf(i18n.T("cli.err.not-a-directory"), dir)
	}
	probe, err := os.CreateTemp(dir, ".tulipe-*")
	if err != nil {
		return fmt.Errorf(i18n.T("cli.err.cannot-write"), dir, err)
	}
	name := probe.Name()
	probe.Close()
	return os.Remove(name)
}

func freeName(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s.%d%s", stem, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return path
}

// outputPath derives the destination file name from the source, the target
// language and the chosen format.
func outputPath(cfg config.Config, src string) string {
	dir := cfg.OutputDir
	if dir == "" {
		dir = filepath.Dir(src)
	}
	base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	tag := cfg.TargetCode
	if tag == "" {
		tag = slug(cfg.TargetLanguage)
	}
	ext := cfg.Format
	if ext == "" {
		ext = config.FormatEPUB
	}
	return filepath.Join(dir, base+"."+tag+"."+ext)
}

// accentFolding maps the Latin letters that carry a diacritic in the languages
// Tulipe offers onto their unaccented form, so that a language name becomes a
// usable file-name fragment.
var accentFolding = strings.NewReplacer(
	"à", "a", "á", "a", "â", "a", "ã", "a", "ä", "a", "å", "a", "ā", "a",
	"ç", "c", "ć", "c", "č", "c",
	"è", "e", "é", "e", "ê", "e", "ë", "e", "ē", "e", "ę", "e", "ě", "e",
	"ì", "i", "í", "i", "î", "i", "ï", "i", "ī", "i",
	"ñ", "n", "ń", "n", "ň", "n",
	"ò", "o", "ó", "o", "ô", "o", "õ", "o", "ö", "o", "ø", "o", "ō", "o",
	"ù", "u", "ú", "u", "û", "u", "ü", "u", "ū", "u", "ů", "u",
	"ý", "y", "ÿ", "y",
	"ß", "ss", "æ", "ae", "œ", "oe",
	"ł", "l", "ś", "s", "š", "s", "ź", "z", "ż", "z", "ž", "z", "ř", "r", "ť", "t", "ď", "d",
)

// slug turns a language name into a file-name fragment. It is only used when
// no BCP 47 code is configured; a name written in a script it cannot render
// falls back to a neutral marker rather than to an empty suffix.
func slug(s string) string {
	var b strings.Builder
	for _, r := range accentFolding.Replace(strings.ToLower(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && b.String()[b.Len()-1] != '-':
			b.WriteByte('-')
		}
	}
	if out := strings.Trim(b.String(), "-"); out != "" {
		return out
	}
	return i18n.T("ui.slug-fallback")
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

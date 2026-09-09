package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/blkmlo/tulipe/internal/config"
	"github.com/blkmlo/tulipe/internal/i18n"
	"github.com/blkmlo/tulipe/internal/llm"
	tea "github.com/charmbracelet/bubbletea"
)

// The model list is asked of the service rather than kept in the program. A
// list shipped in the binary goes stale the day a provider renames something,
// and a name invented from memory sends the user chasing a 404.

type modelsLoadedMsg struct {
	provider string
	models   []string
	err      error
}

func fetchModels(cfg config.Config) tea.Cmd {
	return func() tea.Msg {
		if cfg.Model == "" {
			// Listing needs no model; Validate does.
			cfg.Model = "-"
		}
		if err := cfg.Validate(); err != nil {
			return modelsLoadedMsg{provider: cfg.Provider, err: err}
		}
		provider, err := cfg.NewProvider()
		if err != nil {
			return modelsLoadedMsg{provider: cfg.Provider, err: err}
		}
		lister, ok := provider.(llm.ModelLister)
		if !ok {
			return modelsLoadedMsg{provider: cfg.Provider,
				err: fmt.Errorf(i18n.T("ui.models.cannot"), cfg.Provider)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		models, err := lister.ListModels(ctx)
		return modelsLoadedMsg{provider: cfg.Provider, models: models, err: err}
	}
}

// openModels switches to the model picker and starts the query.
func (m Model) openModels() (tea.Model, tea.Cmd) {
	m.models, m.modelsIndex, m.modelsFilter = nil, 0, ""
	m.screen = screenModels
	m.loading = true
	return m, tea.Batch(m.spin.Tick, fetchModels(m.settings.cfg))
}

func (m Model) updateModels(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	shown := m.shownModels()
	m.modelsIndex = clampIndex(m.modelsIndex, len(shown))
	switch msg.String() {
	case "esc":
		m.screen = screenSettings
		return m, nil
	case "up", "ctrl+p":
		if m.modelsIndex > 0 {
			m.modelsIndex--
		}
	case "down", "ctrl+n":
		if m.modelsIndex < len(shown)-1 {
			m.modelsIndex++
		}
	case "backspace":
		if r := []rune(m.modelsFilter); len(r) > 0 {
			m.modelsFilter = string(r[:len(r)-1])
			m.modelsIndex = 0
		}
	case "enter":
		if len(shown) == 0 {
			return m, nil
		}
		m.settings.cfg.Model = shown[m.modelsIndex]
		m.settings.dirty = true
		m.screen = screenSettings
		m.notice, m.noticeOK = i18n.T("ui.models.chosen", m.settings.cfg.Model), true
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.modelsFilter += string(msg.Runes)
			m.modelsIndex = 0
		}
	}
	return m, nil
}

// shownModels applies the typed filter.
func (m Model) shownModels() []string {
	if m.modelsFilter == "" {
		return m.models
	}
	needle := strings.ToLower(m.modelsFilter)
	var out []string
	for _, name := range m.models {
		if strings.Contains(strings.ToLower(name), needle) {
			out = append(out, name)
		}
	}
	return out
}

// modelsChrome is what the model picker spends on its header, counters and
// key hints.
const modelsChrome = 9

func (m Model) viewModels() string {
	var b strings.Builder
	b.WriteString(header(i18n.T("ui.models.title")) + "\n\n")

	if m.loading {
		b.WriteString(m.spin.View() + dimStyle.Render(i18n.T("ui.models.querying")))
		b.WriteString("\n\n" + m.help(i18n.T("ui.key.esc"), i18n.T("ui.act.back")))
		return b.String()
	}
	if len(m.models) == 0 {
		b.WriteString(dimStyle.Render(i18n.T("ui.models.empty")))
		b.WriteString("\n\n" + m.help(i18n.T("ui.key.esc"), i18n.T("ui.act.back")))
		return b.String()
	}

	shown := m.shownModels()
	b.WriteString(dimStyle.Render(i18n.T("ui.models.count", len(m.models), m.settings.cfg.Provider)))
	if m.modelsFilter != "" {
		b.WriteString(accentStyle.Render(i18n.T("ui.models.filter", m.modelsFilter)))
	}
	b.WriteString("\n\n")

	cursor := clampIndex(m.modelsIndex, len(shown))
	start, end := window(cursor, len(shown), m.listRows(modelsChrome))
	if start > 0 {
		b.WriteString(dimStyle.Render(i18n.T("ui.list.more-above", start)) + "\n")
	}
	for i := start; i < end; i++ {
		b.WriteString(selectLine(i == cursor, shown[i], "") + "\n")
	}
	if len(shown) > end {
		b.WriteString(dimStyle.Render(i18n.T("ui.book.and-more", len(shown)-end)))
	}
	if len(shown) == 0 {
		b.WriteString(dimStyle.Render(i18n.T("ui.models.no-match")))
	}

	b.WriteString("\n" + m.help(i18n.T("ui.key.up-down"), i18n.T("ui.act.choose"), i18n.T("ui.key.enter"), i18n.T("ui.act.keep"), i18n.T("ui.key.type"), i18n.T("ui.act.filter"), i18n.T("ui.key.esc"), i18n.T("ui.act.back")))
	return b.String()
}

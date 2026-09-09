package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/blkmlo/tulipe/internal/config"
	"github.com/blkmlo/tulipe/internal/i18n"
	"github.com/blkmlo/tulipe/internal/llm"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type fieldKind int

const (
	fieldText fieldKind = iota
	fieldInt
	fieldChoice
	fieldBool
)

type field struct {
	label   string
	help    string
	kind    fieldKind
	choices []string
	secret  bool
	// only, when set, restricts the field to one provider.
	only string
	get  func(config.Config) string
	set  func(*config.Config, string) error
}

type settingsForm struct {
	cfg     config.Config
	fields  []field
	index   int
	editing bool
	input   textinput.Model
	dirty   bool
}

func newSettingsForm(cfg config.Config) settingsForm {
	in := textinput.New()
	in.Prompt = "› "
	in.PromptStyle = accentStyle
	in.CharLimit = 400

	return settingsForm{cfg: cfg, fields: settingsFields(), input: in}
}

func settingsFields() []field {
	return []field{
		{
			// The interface language comes first because it decides how every
			// other line on this screen reads.
			label: i18n.T("ui.field.interface-language"), kind: fieldChoice, choices: i18n.LocaleNames(),
			help: i18n.T("ui.field.interface-language.help"),
			get:  func(c config.Config) string { return i18n.LocaleName(c.Language) },
			set: func(c *config.Config, v string) error {
				if code, ok := i18n.LocaleByName(v); ok {
					c.Language = code
				}
				return nil
			},
		},
		{
			label: i18n.T("ui.field.service"), kind: fieldChoice, choices: llm.PresetIDs(),
			help: i18n.T("ui.field.service.help"),
			get:  func(c config.Config) string { return c.Provider },
			set: func(c *config.Config, v string) error {
				c.Provider = v
				preset, ok := llm.LookupPreset(v)
				if !ok {
					return fmt.Errorf(i18n.T("ui.settings.unknown-service"), v)
				}
				// Carry the preset's endpoint over, unless the user typed one
				// of their own.
				if c.BaseURL == "" || fromAPreset(c.BaseURL) {
					c.BaseURL = preset.BaseURL
				}
				if preset.Kind == llm.KindAnthropic && c.Model == "" {
					c.Model = llm.DefaultAnthropicModel
				}
				return nil
			},
		},
		{
			label: i18n.T("ui.field.model"), kind: fieldText,
			help: i18n.T("ui.field.model.help"),
			get:  func(c config.Config) string { return c.Model },
			set:  func(c *config.Config, v string) error { c.Model = strings.TrimSpace(v); return nil },
		},
		{
			label: i18n.T("ui.field.effort"), kind: fieldChoice, choices: config.Efforts, only: llm.KindAnthropic,
			help: i18n.T("ui.field.effort.help"),
			get:  func(c config.Config) string { return c.Effort },
			set:  func(c *config.Config, v string) error { c.Effort = v; return nil },
		},
		{
			label: i18n.T("ui.field.base-url"), kind: fieldText,
			help: i18n.T("ui.field.base-url.help"),
			get:  func(c config.Config) string { return c.BaseURL },
			set:  func(c *config.Config, v string) error { c.BaseURL = strings.TrimSpace(v); return nil },
		},
		{
			label: i18n.T("ui.field.api-key"), kind: fieldText, secret: true,
			help: i18n.T("ui.field.api-key.help"),
			get:  func(c config.Config) string { return c.KeyStatus() },
			set:  func(c *config.Config, v string) error { c.APIKey = strings.TrimSpace(v); return nil },
		},
		{
			label: i18n.T("ui.field.target-language"), kind: fieldChoice, choices: languageNames(),
			help: i18n.T("ui.field.target-language.help"),
			get:  func(c config.Config) string { return c.TargetLanguage },
			set: func(c *config.Config, v string) error {
				c.TargetLanguage = v
				if l, ok := config.LookupLanguage(v); ok {
					c.TargetCode = l.Code
				}
				return nil
			},
		},
		{
			label: i18n.T("ui.field.language-code"), kind: fieldText,
			help: i18n.T("ui.field.language-code.help"),
			get:  func(c config.Config) string { return c.TargetCode },
			set:  func(c *config.Config, v string) error { c.TargetCode = strings.TrimSpace(v); return nil },
		},
		{
			label: i18n.T("ui.field.source-language"), kind: fieldText,
			help: i18n.T("ui.field.source-language.help"),
			get:  func(c config.Config) string { return c.SourceLanguage },
			set:  func(c *config.Config, v string) error { c.SourceLanguage = strings.TrimSpace(v); return nil },
		},
		{
			label: i18n.T("ui.field.source-code"), kind: fieldText,
			help: i18n.T("ui.field.source-code.help"),
			get:  func(c config.Config) string { return c.SourceCode },
			set:  func(c *config.Config, v string) error { c.SourceCode = strings.TrimSpace(v); return nil },
		},
		{
			label: i18n.T("ui.field.titles"), kind: fieldBool,
			help: i18n.T("ui.field.titles.help"),
			get:  func(c config.Config) string { return boolLabel(c.TranslateTitles) },
			set:  setBool(func(c *config.Config, b bool) { c.TranslateTitles = b }),
		},
		{
			label: i18n.T("ui.field.chunk"), kind: fieldInt,
			help: i18n.T("ui.field.chunk.help"),
			get:  func(c config.Config) string { return strconv.Itoa(c.ChunkChars) },
			set:  setInt(func(c *config.Config, n int) { c.ChunkChars = n }, 200, 200000),
		},
		{
			label: i18n.T("ui.field.max-segments"), kind: fieldInt,
			help: i18n.T("ui.field.max-segments.help"),
			get:  func(c config.Config) string { return strconv.Itoa(c.MaxSegments) },
			set:  setInt(func(c *config.Config, n int) { c.MaxSegments = n }, 1, 500),
		},
		{
			label: i18n.T("ui.field.max-tokens"), kind: fieldInt,
			help: i18n.T("ui.field.max-tokens.help"),
			get:  func(c config.Config) string { return strconv.FormatInt(c.MaxTokens, 10) },
			set:  setInt(func(c *config.Config, n int) { c.MaxTokens = int64(n) }, 512, 200000),
		},
		{
			label: i18n.T("ui.field.attempts"), kind: fieldInt,
			help: i18n.T("ui.field.attempts.help"),
			get:  func(c config.Config) string { return strconv.Itoa(c.Attempts) },
			set:  setInt(func(c *config.Config, n int) { c.Attempts = n }, 1, 12),
		},
		{
			label: i18n.T("ui.field.timeout"), kind: fieldInt,
			help: i18n.T("ui.field.timeout.help"),
			get:  func(c config.Config) string { return strconv.Itoa(c.TimeoutSeconds) },
			set:  setInt(func(c *config.Config, n int) { c.TimeoutSeconds = n }, 5, 3600),
		},
		{
			label: i18n.T("ui.field.context"), kind: fieldInt,
			help: i18n.T("ui.field.context.help"),
			get:  func(c config.Config) string { return strconv.Itoa(c.ContextChars) },
			set:  setInt(func(c *config.Config, n int) { c.ContextChars = n }, 0, 4000),
		},
		{
			label: i18n.T("ui.field.structured"), kind: fieldBool,
			help: i18n.T("ui.field.structured.help"),
			get:  func(c config.Config) string { return boolLabel(c.StructuredOutput) },
			set:  setBool(func(c *config.Config, b bool) { c.StructuredOutput = b }),
		},
		{
			label: i18n.T("ui.field.resume"), kind: fieldBool,
			help: i18n.T("ui.field.resume.help"),
			get:  func(c config.Config) string { return boolLabel(c.Resume) },
			set:  setBool(func(c *config.Config, b bool) { c.Resume = b }),
		},
		{
			label: i18n.T("ui.field.format"), kind: fieldChoice, choices: config.Formats,
			help: i18n.T("ui.field.format.help"),
			get:  func(c config.Config) string { return c.Format },
			set:  func(c *config.Config, v string) error { c.Format = v; return nil },
		},
		{
			label: i18n.T("ui.field.output-dir"), kind: fieldText,
			help: i18n.T("ui.field.output-dir.help"),
			get:  func(c config.Config) string { return c.OutputDir },
			set:  func(c *config.Config, v string) error { c.OutputDir = strings.TrimSpace(v); return nil },
		},
		{
			label: i18n.T("ui.field.about"), kind: fieldText,
			help: i18n.T("ui.field.about.help"),
			get:  func(c config.Config) string { return oneLine(c.About) },
			set:  func(c *config.Config, v string) error { c.About = strings.TrimSpace(v); return nil },
		},
		{
			label: i18n.T("ui.field.glossary"), kind: fieldText,
			help: i18n.T("ui.field.glossary.help", config.Path()),
			get:  func(c config.Config) string { return oneLine(c.Glossary) },
			set:  func(c *config.Config, v string) error { c.Glossary = v; return nil },
		},
		{
			label: i18n.T("ui.field.style"), kind: fieldText,
			help: i18n.T("ui.field.style.help"),
			get:  func(c config.Config) string { return oneLine(c.StyleNotes) },
			set:  func(c *config.Config, v string) error { c.StyleNotes = v; return nil },
		},
	}
}

func setInt(apply func(*config.Config, int), lo, hi int) func(*config.Config, string) error {
	return func(c *config.Config, v string) error {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf(i18n.T("ui.settings.not-an-int"), v)
		}
		if n < lo || n > hi {
			return fmt.Errorf(i18n.T("ui.settings.out-of-range"), lo, hi)
		}
		apply(c, n)
		return nil
	}
}

func setBool(apply func(*config.Config, bool)) func(*config.Config, string) error {
	return func(c *config.Config, v string) error {
		apply(c, v == i18n.T("ui.yes"))
		return nil
	}
}

func boolLabel(b bool) string {
	if b {
		return i18n.T("ui.yes")
	}
	return i18n.T("ui.no")
}

func languageNames() []string {
	out := make([]string, 0, len(config.Languages))
	for _, l := range config.Languages {
		out = append(out, l.Name)
	}
	return out
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ⏎ ")), " ")
}

// visible returns the indices of the fields that apply to the current provider.
func (f settingsForm) visible() []int {
	kind := f.cfg.Kind()
	var out []int
	for i, fd := range f.fields {
		if fd.only != "" && fd.only != kind {
			continue
		}
		out = append(out, i)
	}
	return out
}

// fromAPreset reports whether a base URL is one the catalogue supplied, and so
// may be replaced when the service changes.
func fromAPreset(url string) bool {
	for _, p := range llm.Presets() {
		if p.BaseURL != "" && p.BaseURL == url {
			return true
		}
	}
	return false
}

func (m Model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &m.settings
	vis := f.visible()
	if len(vis) == 0 {
		m.screen = screenMenu
		return m, nil
	}
	if f.index >= len(vis) {
		f.index = len(vis) - 1
	}
	fd := f.fields[vis[f.index]]

	if f.editing {
		switch msg.String() {
		case "esc":
			f.editing = false
			f.input.Blur()
			return m, nil
		case "enter":
			if err := fd.set(&f.cfg, f.input.Value()); err != nil {
				m.failure = err.Error()
				return m, nil
			}
			f.dirty = true
			f.editing = false
			f.input.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		f.input, cmd = f.input.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "esc":
		if f.dirty {
			m.notice, m.noticeOK = i18n.T("ui.settings.unsaved.warn"), false
			f.dirty = false
			return m, nil
		}
		m.screen = screenMenu
		return m, nil
	case "up", "k":
		if f.index > 0 {
			f.index--
		}
	case "down", "j":
		if f.index < len(vis)-1 {
			f.index++
		}
	case "m":
		m.settings = *f
		return m.openModels()
	case "s":
		if err := f.cfg.Save(); err != nil {
			m.failure = err.Error()
			return m, nil
		}
		m.cfg = f.cfg
		// The language takes effect on save, not while the choice is being
		// cycled: abandoning the screen must leave the interface as it was.
		i18n.SetLocale(m.cfg.Language)
		m.menu = buildMenu()
		f.fields = settingsFields()
		f.dirty = false
		m.notice, m.noticeOK = i18n.T("ui.settings.saved", config.Path()), true
	case "left", "h":
		if fd.kind == fieldChoice || fd.kind == fieldBool {
			m.cycleChoice(fd, -1)
		}
	case "right", "l", "enter", " ":
		switch fd.kind {
		case fieldChoice, fieldBool:
			m.cycleChoice(fd, 1)
		default:
			f.editing = true
			value := fd.get(f.cfg)
			if fd.secret {
				value = f.cfg.APIKey
				f.input.EchoMode = textinput.EchoPassword
			} else {
				f.input.EchoMode = textinput.EchoNormal
			}
			f.input.SetValue(value)
			f.input.CursorEnd()
			return m, f.input.Focus()
		}
	}
	return m, nil
}

// cycleChoice moves a choice or boolean field by one step.
func (m *Model) cycleChoice(fd field, step int) {
	f := &m.settings
	choices := fd.choices
	if fd.kind == fieldBool {
		choices = []string{i18n.T("ui.no"), i18n.T("ui.yes")}
	}
	current := fd.get(f.cfg)
	pos := 0
	for i, c := range choices {
		if c == current {
			pos = i
		}
	}
	next := choices[((pos+step)%len(choices)+len(choices))%len(choices)]
	if err := fd.set(&f.cfg, next); err != nil {
		m.failure = err.Error()
		return
	}
	f.dirty = true
}

// settingsChrome is how many lines the settings screen spends on everything
// that is not a field: the header, the help panel under the list, the unsaved
// marker and the key hints.
const settingsChrome = 13

func (m Model) viewSettings() string {
	f := m.settings
	var b strings.Builder
	b.WriteString(header(i18n.T("ui.settings.title")) + "\n\n")

	vis := f.visible()
	// The list is windowed to what the terminal can actually show. Rendering
	// every field and letting the terminal scroll away the top is what forced
	// people to resize their window to reach the last setting.
	rows := m.listRows(settingsChrome)
	start := clamp(f.index-rows/2, 0, max(0, len(vis)-rows))
	end := min(len(vis), start+rows)

	if start > 0 {
		b.WriteString(dimStyle.Render(i18n.T("ui.list.more-above", start)) + "\n")
	}
	for i := start; i < end; i++ {
		fd := f.fields[vis[i]]
		selected := i == f.index
		value := fd.get(f.cfg)
		if fd.kind == fieldChoice || fd.kind == fieldBool {
			value = "‹ " + value + " ›"
		}
		if value == "" {
			value = dimStyle.Render(i18n.T("ui.dash"))
		}
		if selected && f.editing {
			b.WriteString(accentStyle.Render("› ") + accentStyle.Bold(true).Render(pad(fd.label, 22)) + f.input.View() + "\n")
			continue
		}
		prefix := "  "
		labelSt := labelStyle
		if selected {
			prefix = accentStyle.Render("› ")
			labelSt = accentStyle.Bold(true)
		}
		b.WriteString(prefix + labelSt.Render(pad(fd.label, 22)) + valueStyle.Render(value) + "\n")
	}
	if end < len(vis) {
		b.WriteString(dimStyle.Render(i18n.T("ui.list.more-below", len(vis)-end)) + "\n")
	}

	if len(vis) > 0 {
		fd := f.fields[vis[f.index]]
		if fd.help != "" {
			b.WriteString("\n" + panelStyle.Render(dimStyle.Render(wrap(fd.help, clamp(m.width-8, 40, 96)))))
		}
	}

	if f.dirty {
		b.WriteString("\n\n" + warnStyle.Render(i18n.T("ui.settings.unsaved")))
	}
	if f.editing {
		b.WriteString("\n\n" + help(i18n.T("ui.key.enter"), i18n.T("ui.act.confirm"), i18n.T("ui.key.esc"), i18n.T("ui.act.cancel-edit")))
	} else {
		b.WriteString("\n\n" + help(
			i18n.T("ui.key.up-down"), i18n.T("ui.act.field"),
			"←/→", i18n.T("ui.act.change"),
			i18n.T("ui.key.enter"), i18n.T("ui.act.edit"),
			"m", i18n.T("ui.act.models"),
			"s", i18n.T("ui.act.save"),
			i18n.T("ui.key.esc"), i18n.T("ui.act.back")))
	}
	return b.String()
}

func pad(s string, n int) string {
	for len([]rune(s)) < n {
		s += " "
	}
	return s
}

func wrap(s string, width int) string {
	words := strings.Fields(s)
	var lines []string
	line := ""
	for _, w := range words {
		if line == "" {
			line = w
			continue
		}
		if len([]rune(line))+1+len([]rune(w)) > width {
			lines = append(lines, line)
			line = w
			continue
		}
		line += " " + w
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

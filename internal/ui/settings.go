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
	"github.com/charmbracelet/lipgloss"
)

type fieldKind int

const (
	fieldText fieldKind = iota
	fieldInt
	fieldChoice
	fieldBool
)

type field struct {
	// section is the heading this field belongs under. Fields of one section
	// are consecutive in the list, which is the only thing that makes the
	// headings mean anything.
	section string
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
			section: i18n.T("ui.section.interface"),
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
			section: i18n.T("ui.section.service"),
			label:   i18n.T("ui.field.service"), kind: fieldChoice, choices: llm.PresetIDs(),
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
			section: i18n.T("ui.section.service"),
			label:   i18n.T("ui.field.model"), kind: fieldText,
			help: i18n.T("ui.field.model.help"),
			get:  func(c config.Config) string { return c.Model },
			set:  func(c *config.Config, v string) error { c.Model = strings.TrimSpace(v); return nil },
		},
		{
			section: i18n.T("ui.section.service"),
			label:   i18n.T("ui.field.base-url"), kind: fieldText,
			help: i18n.T("ui.field.base-url.help"),
			get:  func(c config.Config) string { return c.BaseURL },
			set:  func(c *config.Config, v string) error { c.BaseURL = strings.TrimSpace(v); return nil },
		},
		{
			section: i18n.T("ui.section.service"),
			label:   i18n.T("ui.field.api-key"), kind: fieldText, secret: true,
			help: i18n.T("ui.field.api-key.help"),
			get:  func(c config.Config) string { return c.KeyStatus() },
			set:  func(c *config.Config, v string) error { c.APIKey = strings.TrimSpace(v); return nil },
		},
		{
			section: i18n.T("ui.section.service"),
			label:   i18n.T("ui.field.effort"), kind: fieldChoice, choices: config.Efforts, only: llm.KindAnthropic,
			help: i18n.T("ui.field.effort.help"),
			get:  func(c config.Config) string { return c.Effort },
			set:  func(c *config.Config, v string) error { c.Effort = v; return nil },
		},
		{
			section: i18n.T("ui.section.service"),
			label:   i18n.T("ui.field.structured"), kind: fieldBool,
			help: i18n.T("ui.field.structured.help"),
			get:  func(c config.Config) string { return boolLabel(c.StructuredOutput) },
			set:  setBool(func(c *config.Config, b bool) { c.StructuredOutput = b }),
		},
		{
			section: i18n.T("ui.section.languages"),
			label:   i18n.T("ui.field.target-language"), kind: fieldChoice, choices: languageNames(),
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
			section: i18n.T("ui.section.languages"),
			label:   i18n.T("ui.field.language-code"), kind: fieldText,
			help: i18n.T("ui.field.language-code.help"),
			get:  func(c config.Config) string { return c.TargetCode },
			set:  func(c *config.Config, v string) error { c.TargetCode = strings.TrimSpace(v); return nil },
		},
		{
			section: i18n.T("ui.section.languages"),
			label:   i18n.T("ui.field.source-language"), kind: fieldText,
			help: i18n.T("ui.field.source-language.help"),
			get:  func(c config.Config) string { return c.SourceLanguage },
			set:  func(c *config.Config, v string) error { c.SourceLanguage = strings.TrimSpace(v); return nil },
		},
		{
			section: i18n.T("ui.section.languages"),
			label:   i18n.T("ui.field.source-code"), kind: fieldText,
			help: i18n.T("ui.field.source-code.help"),
			get:  func(c config.Config) string { return c.SourceCode },
			set:  func(c *config.Config, v string) error { c.SourceCode = strings.TrimSpace(v); return nil },
		},
		{
			section: i18n.T("ui.section.translation"),
			label:   i18n.T("ui.field.titles"), kind: fieldBool,
			help: i18n.T("ui.field.titles.help"),
			get:  func(c config.Config) string { return boolLabel(c.TranslateTitles) },
			set:  setBool(func(c *config.Config, b bool) { c.TranslateTitles = b }),
		},
		{
			section: i18n.T("ui.section.translation"),
			label:   i18n.T("ui.field.about"), kind: fieldText,
			help: i18n.T("ui.field.about.help"),
			get:  func(c config.Config) string { return oneLine(c.About) },
			set:  func(c *config.Config, v string) error { c.About = strings.TrimSpace(v); return nil },
		},
		{
			section: i18n.T("ui.section.translation"),
			label:   i18n.T("ui.field.glossary"), kind: fieldText,
			help: i18n.T("ui.field.glossary.help", config.Path()),
			get:  func(c config.Config) string { return oneLine(c.Glossary) },
			set:  func(c *config.Config, v string) error { c.Glossary = v; return nil },
		},
		{
			section: i18n.T("ui.section.translation"),
			label:   i18n.T("ui.field.style"), kind: fieldText,
			help: i18n.T("ui.field.style.help"),
			get:  func(c config.Config) string { return oneLine(c.StyleNotes) },
			set:  func(c *config.Config, v string) error { c.StyleNotes = v; return nil },
		},
		{
			section: i18n.T("ui.section.translation"),
			label:   i18n.T("ui.field.context"), kind: fieldInt,
			help: i18n.T("ui.field.context.help"),
			get:  func(c config.Config) string { return strconv.Itoa(c.ContextChars) },
			set:  setInt(func(c *config.Config, n int) { c.ContextChars = n }, 0, 4000),
		},
		{
			section: i18n.T("ui.section.requests"),
			label:   i18n.T("ui.field.chunk"), kind: fieldInt,
			help: i18n.T("ui.field.chunk.help"),
			get:  func(c config.Config) string { return strconv.Itoa(c.ChunkChars) },
			set:  setInt(func(c *config.Config, n int) { c.ChunkChars = n }, 200, 200000),
		},
		{
			section: i18n.T("ui.section.requests"),
			label:   i18n.T("ui.field.max-segments"), kind: fieldInt,
			help: i18n.T("ui.field.max-segments.help"),
			get:  func(c config.Config) string { return strconv.Itoa(c.MaxSegments) },
			set:  setInt(func(c *config.Config, n int) { c.MaxSegments = n }, 1, 500),
		},
		{
			section: i18n.T("ui.section.requests"),
			label:   i18n.T("ui.field.max-tokens"), kind: fieldInt,
			help: i18n.T("ui.field.max-tokens.help"),
			get:  func(c config.Config) string { return strconv.FormatInt(c.MaxTokens, 10) },
			set:  setInt(func(c *config.Config, n int) { c.MaxTokens = int64(n) }, 512, 200000),
		},
		{
			section: i18n.T("ui.section.requests"),
			label:   i18n.T("ui.field.attempts"), kind: fieldInt,
			help: i18n.T("ui.field.attempts.help"),
			get:  func(c config.Config) string { return strconv.Itoa(c.Attempts) },
			set:  setInt(func(c *config.Config, n int) { c.Attempts = n }, 1, 12),
		},
		{
			section: i18n.T("ui.section.requests"),
			label:   i18n.T("ui.field.timeout"), kind: fieldInt,
			help: i18n.T("ui.field.timeout.help"),
			get:  func(c config.Config) string { return strconv.Itoa(c.TimeoutSeconds) },
			set:  setInt(func(c *config.Config, n int) { c.TimeoutSeconds = n }, 5, 3600),
		},
		{
			section: i18n.T("ui.section.recovery"),
			label:   i18n.T("ui.field.resume"), kind: fieldBool,
			help: i18n.T("ui.field.resume.help"),
			get:  func(c config.Config) string { return boolLabel(c.Resume) },
			set:  setBool(func(c *config.Config, b bool) { c.Resume = b }),
		},
		{
			section: i18n.T("ui.section.recovery"),
			label:   i18n.T("ui.field.salvage"), kind: fieldBool,
			help: i18n.T("ui.field.salvage.help"),
			get:  func(c config.Config) string { return boolLabel(c.SalvagePass) },
			set:  setBool(func(c *config.Config, b bool) { c.SalvagePass = b }),
		},
		{
			section: i18n.T("ui.section.output"),
			label:   i18n.T("ui.field.format"), kind: fieldChoice, choices: config.Formats,
			help: i18n.T("ui.field.format.help"),
			get:  func(c config.Config) string { return c.Format },
			set:  func(c *config.Config, v string) error { c.Format = v; return nil },
		},
		{
			section: i18n.T("ui.section.output"),
			label:   i18n.T("ui.field.output-dir"), kind: fieldText,
			help: i18n.T("ui.field.output-dir.help"),
			get:  func(c config.Config) string { return c.OutputDir },
			set:  func(c *config.Config, v string) error { c.OutputDir = strings.TrimSpace(v); return nil },
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

// labelColumn is the width of the label column, measured from the labels
// themselves rather than written down. A number chosen by eye is right in one
// language and one field list, and wrong the moment either changes: the widest
// English label was exactly 22 characters, so "Characters per request4000" had
// nowhere to put a space.
func (f settingsForm) labelColumn() int {
	w := 0
	for _, fd := range f.fields {
		if n := lipgloss.Width(fd.label); n > w {
			w = n
		}
	}
	return w + 2
}

// settingsRow is one line of the settings list: either a section heading or a
// field. Building the two into a single list is what keeps the headings inside
// the window's budget — a heading drawn outside it is a line the screen did not
// count, and the bottom goes missing again.
type settingsRows []settingsRow

type settingsRow struct {
	heading string
	// field indexes into visible(), or is -1 for a heading.
	field int
}

// listRows lays the visible fields out with a heading above each group. A
// section whose every field is hidden — effort, outside Anthropic — takes no
// heading either: a title with nothing under it reads as a bug.
func (f settingsForm) listRows() settingsRows {
	var rows settingsRows
	section := ""
	for i, idx := range f.visible() {
		if fd := f.fields[idx]; fd.section != section {
			section = fd.section
			rows = append(rows, settingsRow{heading: section, field: -1})
		}
		rows = append(rows, settingsRow{field: i})
	}
	return rows
}

// rowOf finds the line a visible field sits on, which is where the window has
// to be centred.
func (rows settingsRows) rowOf(field int) int {
	for i, r := range rows {
		if r.field == field {
			return i
		}
	}
	return 0
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

func (m Model) viewSettings() string {
	f := m.settings
	vis := f.visible()
	column := f.labelColumn()

	head := header(i18n.T("ui.settings.title")) + "\n"

	// The chrome is measured, not assumed. Its height depends on the help of
	// whichever field is selected, on how that help wraps at this width, and
	// on the language it is written in — a constant is right for one of those
	// and wrong for the next one added. Getting it wrong pushes the bottom of
	// the list off the screen, which is the whole thing this window exists to
	// prevent.
	foot := m.settingsFoot(true)
	rows := m.rows() - lipgloss.Height(head) - lipgloss.Height(foot)
	if rows < minListRows {
		// On a very short terminal something has to go, and it is the help
		// text: the reader came here for the settings, and the sentence
		// explaining one of them is worth less than seeing three of them.
		foot = m.settingsFoot(false)
		rows = m.rows() - lipgloss.Height(head) - lipgloss.Height(foot)
	}
	// The window runs over rows rather than fields, so a heading costs a line
	// like anything else and the count above stays true.
	list := f.listRows()
	start, end := window(list.rowOf(f.index), len(list), max(minListRows, rows))

	var b strings.Builder
	b.WriteString(head)
	if start > 0 {
		b.WriteString(dimStyle.Render(i18n.T("ui.list.more-above", countFields(list[:start]))) + "\n")
	}
	for _, row := range list[start:end] {
		if row.field < 0 {
			b.WriteString(sectionStyle.Render(row.heading) + "\n")
			continue
		}
		fd := f.fields[vis[row.field]]
		selected := row.field == f.index
		value := fd.get(f.cfg)
		if fd.kind == fieldChoice || fd.kind == fieldBool {
			value = "‹ " + value + " ›"
		}
		if value == "" {
			value = dimStyle.Render(i18n.T("ui.dash"))
		}
		if selected && f.editing {
			b.WriteString(accentStyle.Render("› ") + accentStyle.Bold(true).Render(pad(fd.label, column)) + f.input.View() + "\n")
			continue
		}
		prefix := "  "
		labelSt := labelStyle
		if selected {
			prefix = accentStyle.Render("› ")
			labelSt = accentStyle.Bold(true)
		}
		b.WriteString(prefix + labelSt.Render(pad(fd.label, column)) + valueStyle.Render(value) + "\n")
	}
	if end < len(list) {
		b.WriteString(dimStyle.Render(i18n.T("ui.list.more-below", countFields(list[end:]))) + "\n")
	}
	b.WriteString(foot)
	return b.String()
}

// countFields counts the settings in a stretch of rows, ignoring the headings.
// "12 more below" has to mean twelve settings: counting the titles too would
// promise more than the list holds.
func countFields(rows settingsRows) int {
	n := 0
	for _, r := range rows {
		if r.field >= 0 {
			n++
		}
	}
	return n
}

// settingsFoot is everything below the list: the help of the selected field,
// the unsaved marker, and the key hints. It is built separately from the list
// so that its height can be measured before the list is sized.
func (m Model) settingsFoot(withHelp bool) string {
	f := m.settings
	var b strings.Builder

	if vis := f.visible(); withHelp && len(vis) > 0 {
		if fd := f.fields[vis[f.index]]; fd.help != "" {
			b.WriteString("\n" + panelStyle.Render(dimStyle.Render(wrap(fd.help, m.panelWidth()))))
		}
	}
	if f.dirty {
		b.WriteString("\n\n" + warnStyle.Render(i18n.T("ui.settings.unsaved")))
	}
	if f.editing {
		b.WriteString("\n\n" + m.help(i18n.T("ui.key.enter"), i18n.T("ui.act.confirm"), i18n.T("ui.key.esc"), i18n.T("ui.act.cancel-edit")))
	} else {
		b.WriteString("\n\n" + m.help(
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

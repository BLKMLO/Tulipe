package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/blkmlo/tulipe/internal/config"
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
			label: "Service", kind: fieldChoice, choices: llm.PresetIDs(),
			help: "« m » interroge le service pour la liste de ses modèles ; « tulipe providers » détaille chaque entrée",
			get:  func(c config.Config) string { return c.Provider },
			set: func(c *config.Config, v string) error {
				c.Provider = v
				preset, ok := llm.LookupPreset(v)
				if !ok {
					return fmt.Errorf("service inconnu %q", v)
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
			label: "Modèle", kind: fieldText,
			help: "identifiant exact du modèle",
			get:  func(c config.Config) string { return c.Model },
			set:  func(c *config.Config, v string) error { c.Model = strings.TrimSpace(v); return nil },
		},
		{
			label: "Effort", kind: fieldChoice, choices: config.Efforts, only: llm.KindAnthropic,
			help: "profondeur de réflexion du modèle : plus haut traduit mieux les passages difficiles et coûte plus cher",
			get:  func(c config.Config) string { return c.Effort },
			set:  func(c *config.Config, v string) error { c.Effort = v; return nil },
		},
		{
			label: "URL de base", kind: fieldText,
			help: "obligatoire pour un serveur compatible OpenAI, par exemple http://localhost:11434/v1 ; vide sinon",
			get:  func(c config.Config) string { return c.BaseURL },
			set:  func(c *config.Config, v string) error { c.BaseURL = strings.TrimSpace(v); return nil },
		},
		{
			label: "Clé API", kind: fieldText, secret: true,
			help: "laisser vide et utiliser la variable d'environnement est plus sûr : la clé ne touche jamais le disque",
			get:  func(c config.Config) string { return c.KeyStatus() },
			set:  func(c *config.Config, v string) error { c.APIKey = strings.TrimSpace(v); return nil },
		},
		{
			label: "Langue cible", kind: fieldChoice, choices: languageNames(),
			help: "nom de la langue tel qu'un humain l'écrirait ; le code BCP 47 correspondant est inscrit dans les métadonnées du livre",
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
			label: "Code de langue", kind: fieldText,
			help: "étiquette BCP 47 écrite dans <dc:language> ; vide laisse la métadonnée d'origine",
			get:  func(c config.Config) string { return c.TargetCode },
			set:  func(c *config.Config, v string) error { c.TargetCode = strings.TrimSpace(v); return nil },
		},
		{
			label: "Langue source", kind: fieldText,
			help: "vide laisse le modèle la reconnaître",
			get:  func(c config.Config) string { return c.SourceLanguage },
			set:  func(c *config.Config, v string) error { c.SourceLanguage = strings.TrimSpace(v); return nil },
		},
		{
			label: "Code de langue source", kind: fieldText,
			help: "étiquette BCP 47 de la source ; DeepL s'en sert, vide lui laisse la détecter",
			get:  func(c config.Config) string { return c.SourceCode },
			set:  func(c *config.Config, v string) error { c.SourceCode = strings.TrimSpace(v); return nil },
		},
		{
			label: "Caractères par requête", kind: fieldInt,
			help: "taille maximale d'un lot de segments ; c'est ce réglage qui empêche le contexte de déborder",
			get:  func(c config.Config) string { return strconv.Itoa(c.ChunkChars) },
			set:  setInt(func(c *config.Config, n int) { c.ChunkChars = n }, 200, 200000),
		},
		{
			label: "Segments par requête", kind: fieldInt,
			help: "second plafond, en nombre de paragraphes",
			get:  func(c config.Config) string { return strconv.Itoa(c.MaxSegments) },
			set:  setInt(func(c *config.Config, n int) { c.MaxSegments = n }, 1, 500),
		},
		{
			label: "Jetons de réponse", kind: fieldInt,
			help: "plafond de la réponse du modèle ; trop bas, la traduction est coupée et le lot est redécoupé",
			get:  func(c config.Config) string { return strconv.FormatInt(c.MaxTokens, 10) },
			set:  setInt(func(c *config.Config, n int) { c.MaxTokens = int64(n) }, 512, 200000),
		},
		{
			label: "Tentatives", kind: fieldInt,
			help: "nombre d'essais avant de redécouper un lot en deux",
			get:  func(c config.Config) string { return strconv.Itoa(c.Attempts) },
			set:  setInt(func(c *config.Config, n int) { c.Attempts = n }, 1, 12),
		},
		{
			label: "Délai par appel (s)", kind: fieldInt,
			help: "au-delà, l'appel est abandonné et réessayé ; empêche un service muet de bloquer la traduction",
			get:  func(c config.Config) string { return strconv.Itoa(c.TimeoutSeconds) },
			set:  setInt(func(c *config.Config, n int) { c.TimeoutSeconds = n }, 5, 3600),
		},
		{
			label: "Continuité", kind: fieldInt,
			help: "caractères de la traduction précédente montrés au modèle pour tenir le ton et le vocabulaire",
			get:  func(c config.Config) string { return strconv.Itoa(c.ContextChars) },
			set:  setInt(func(c *config.Config, n int) { c.ContextChars = n }, 0, 4000),
		},
		{
			label: "Sortie structurée", kind: fieldBool,
			help: "contraint la réponse à un schéma JSON ; désactivé automatiquement si le service la refuse",
			get:  func(c config.Config) string { return boolLabel(c.StructuredOutput) },
			set:  setBool(func(c *config.Config, b bool) { c.StructuredOutput = b }),
		},
		{
			label: "Reprise", kind: fieldBool,
			help: "conserve les chapitres traduits pour ne jamais les repayer après une interruption",
			get:  func(c config.Config) string { return boolLabel(c.Resume) },
			set:  setBool(func(c *config.Config, b bool) { c.Resume = b }),
		},
		{
			label: "Format de sortie", kind: fieldChoice, choices: config.Formats,
			help: "epub conserve la mise en forme, les images et la table des matières ; txt ne garde que la prose",
			get:  func(c config.Config) string { return c.Format },
			set:  func(c *config.Config, v string) error { c.Format = v; return nil },
		},
		{
			label: "Dossier de sortie", kind: fieldText,
			help: "vide écrit à côté du livre d'origine",
			get:  func(c config.Config) string { return c.OutputDir },
			set:  func(c *config.Config, v string) error { c.OutputDir = strings.TrimSpace(v); return nil },
		},
		{
			label: "Glossaire", kind: fieldText,
			help: "une règle « source = cible » par ligne ; pour un glossaire long, éditer glossary dans " + config.Path(),
			get:  func(c config.Config) string { return oneLine(c.Glossary) },
			set:  func(c *config.Config, v string) error { c.Glossary = v; return nil },
		},
		{
			label: "Consignes de style", kind: fieldText,
			help: "ajoutées telles quelles aux instructions du traducteur",
			get:  func(c config.Config) string { return oneLine(c.StyleNotes) },
			set:  func(c *config.Config, v string) error { c.StyleNotes = v; return nil },
		},
	}
}

func setInt(apply func(*config.Config, int), lo, hi int) func(*config.Config, string) error {
	return func(c *config.Config, v string) error {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("%q n'est pas un nombre entier", v)
		}
		if n < lo || n > hi {
			return fmt.Errorf("valeur attendue entre %d et %d", lo, hi)
		}
		apply(c, n)
		return nil
	}
}

func setBool(apply func(*config.Config, bool)) func(*config.Config, string) error {
	return func(c *config.Config, v string) error {
		apply(c, v == "oui")
		return nil
	}
}

func boolLabel(b bool) string {
	if b {
		return "oui"
	}
	return "non"
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
			m.notice, m.noticeOK = "Réglages non enregistrés — « s » pour enregistrer, « échap » à nouveau pour abandonner.", false
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
		m.menu = buildMenu()
		f.dirty = false
		m.notice, m.noticeOK = "Réglages enregistrés dans "+config.Path(), true
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
		choices = []string{"non", "oui"}
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
	var b strings.Builder
	b.WriteString(header("réglages") + "\n\n")

	vis := f.visible()
	for i, idx := range vis {
		fd := f.fields[idx]
		selected := i == f.index
		value := fd.get(f.cfg)
		if fd.kind == fieldChoice || fd.kind == fieldBool {
			value = "‹ " + value + " ›"
		}
		if value == "" {
			value = dimStyle.Render("—")
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

	if len(vis) > 0 {
		fd := f.fields[vis[f.index]]
		if fd.help != "" {
			b.WriteString("\n" + panelStyle.Render(dimStyle.Render(wrap(fd.help, clamp(m.width-8, 40, 96)))))
		}
	}

	if f.dirty {
		b.WriteString("\n\n" + warnStyle.Render("modifications non enregistrées"))
	}
	if f.editing {
		b.WriteString("\n\n" + help("entrée", "valider", "échap", "annuler la saisie"))
	} else {
		b.WriteString("\n\n" + help("↑/↓", "champ", "←/→", "changer", "entrée", "modifier", "m", "modèles", "s", "enregistrer", "échap", "retour"))
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

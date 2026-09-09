package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/blkmlo/tulipe/internal/llm"
	"github.com/blkmlo/tulipe/internal/translate"
)

// View implements tea.Model.
func (m Model) View() string {
	var body string
	switch m.screen {
	case screenMenu:
		body = m.viewMenu()
	case screenPicker:
		body = m.viewPicker()
	case screenBook:
		body = m.viewBook()
	case screenRun:
		body = m.viewRun()
	case screenReport:
		body = m.viewReport()
	case screenSettings:
		body = m.viewSettings()
	case screenResume:
		body = m.viewResume()
	case screenModels:
		body = m.viewModels()
	}

	var out strings.Builder
	out.WriteString(body)
	if m.failure != "" {
		out.WriteString("\n\n" + errStyle.Render("✗ "+truncate(m.failure, 400)))
	} else if m.notice != "" {
		style := warnStyle
		if m.noticeOK {
			style = okStyle
		}
		out.WriteString("\n\n" + style.Render("• "+truncate(m.notice, 400)))
	}
	return out.String() + "\n"
}

func (m Model) viewMenu() string {
	var b strings.Builder
	b.WriteString(header("traduction d'EPUB, chapitre par chapitre") + "\n\n")

	lines := make([]string, 0, len(m.menu))
	for i, e := range m.menu {
		lines = append(lines, selectLine(i == m.menuIndex, e.label, e.detail))
	}
	b.WriteString(strings.Join(lines, "\n"))

	b.WriteString("\n\n" + panelStyle.Render(strings.Join([]string{
		labelStyle.Render("modèle  ") + valueStyle.Render(m.cfg.Model) + dimStyle.Render(" via "+m.cfg.Provider),
		labelStyle.Render("langue  ") + valueStyle.Render(m.cfg.TargetLanguage),
		labelStyle.Render("clé API ") + valueStyle.Render(m.cfg.KeyStatus()),
	}, "\n")))

	if m.loading {
		b.WriteString("\n\n" + m.spin.View() + dimStyle.Render(" en cours…"))
	}
	b.WriteString("\n\n" + help("↑/↓", "naviguer", "entrée", "choisir", "q", "quitter"))
	return b.String()
}

func (m Model) viewPicker() string {
	var b strings.Builder
	b.WriteString(header("choisir un fichier EPUB") + "\n\n")
	b.WriteString(dimStyle.Render(m.picker.CurrentDirectory) + "\n\n")
	b.WriteString(m.picker.View())
	if m.loading {
		b.WriteString("\n" + m.spin.View() + dimStyle.Render(" lecture du livre…"))
	}
	b.WriteString("\n" + help("↑/↓", "parcourir", "entrée", "ouvrir", "échap", "retour"))
	return b.String()
}

func (m Model) viewBook() string {
	if m.book == nil {
		return header("") + "\n\naucun livre chargé"
	}
	var b strings.Builder
	b.WriteString(header(m.bookTitle()) + "\n\n")

	rows := []string{
		labelStyle.Render("fichier      ") + valueStyle.Render(m.bookPath),
		labelStyle.Render("langue source") + valueStyle.Render(orDash(m.book.Language)),
		labelStyle.Render("documents    ") + valueStyle.Render(fmt.Sprintf("%d (%d chapitres au fil de lecture)", m.stats.Documents, len(m.book.Chapters))),
		labelStyle.Render("à traduire   ") + valueStyle.Render(fmt.Sprintf("%d segments, %s caractères", m.stats.Segments, thousands(m.stats.Chars))),
		labelStyle.Render("vers         ") + accentStyle.Render(m.cfg.TargetLanguage) + dimStyle.Render("  ·  "+m.cfg.Model),
		labelStyle.Render("sortie       ") + valueStyle.Render(m.outPath),
	}
	if m.reusable > 0 {
		rows = append(rows, labelStyle.Render("reprise      ")+okStyle.Render(fmt.Sprintf("%d document(s) déjà traduits seront réutilisés", m.reusable)))
	}
	if m.pending > 0 {
		rows = append(rows, labelStyle.Render("en attente   ")+warnStyle.Render(fmt.Sprintf("%d passage(s) laissés en langue source", m.pending)))
	}
	b.WriteString(panelStyle.Render(strings.Join(rows, "\n")))

	b.WriteString("\n\n" + labelStyle.Render("chapitres") + "\n")
	limit := 10
	for i, ch := range m.book.Chapters {
		if i == limit {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  … et %d autres\n", len(m.book.Chapters)-limit)))
			break
		}
		b.WriteString(dimStyle.Render(fmt.Sprintf("  %2d. ", i+1)) + valueStyle.Render(truncate(ch.Title, 60)) + "\n")
	}

	b.WriteString("\n" + dimStyle.Render("Le livre est traduit un document à la fois, puis découpé en requêtes de "+
		fmt.Sprintf("%d caractères au plus : le contexte du modèle ne porte jamais le livre entier.", m.cfg.ChunkChars)))
	keys := []string{"entrée", "traduire"}
	if m.pending > 0 {
		keys = append(keys, "p", "reprendre les passages en attente")
	}
	keys = append(keys, "r", "vider le cache de reprise", "échap", "retour")
	b.WriteString("\n\n" + help(keys...))
	return b.String()
}

func (m Model) viewRun() string {
	var b strings.Builder
	b.WriteString(header("traduction en cours") + "\n\n")

	docs := m.run.progress.Documents
	done := 0
	for _, d := range docs {
		if d.Status == translate.StatusDone || d.Status == translate.StatusCached {
			done++
		}
	}
	ratio := 0.0
	if len(docs) > 0 {
		ratio = float64(done) / float64(len(docs))
	}
	b.WriteString(m.bar.ViewAs(ratio) + "  " + valueStyle.Render(fmt.Sprintf("%d/%d documents", done, len(docs))) + "\n\n")

	// Window the document list around the one being worked on.
	current := m.run.progress.Current
	start := max(0, current-3)
	end := min(len(docs), start+9)
	for i := start; i < end; i++ {
		b.WriteString(docLine(docs[i], i == current, m.spin.View()) + "\n")
	}

	b.WriteString("\n" + panelStyle.Render(strings.Join([]string{
		labelStyle.Render("écoulé  ") + valueStyle.Render(m.elapsed.Round(time.Second).String()),
		labelStyle.Render("jetons  ") + valueStyle.Render(usageLine(m.run.progress.Usage, m.run.progress.Attempted)),
	}, "\n")))

	if n := len(m.run.log); n > 0 {
		b.WriteString("\n\n" + labelStyle.Render("journal") + "\n")
		for _, line := range m.run.log[max(0, n-5):] {
			b.WriteString("  " + line + "\n")
		}
	}

	b.WriteString("\n" + help("échap", "annuler (le travail déjà fait est conservé)"))
	return b.String()
}

func docLine(d translate.DocState, current bool, spin string) string {
	var mark string
	switch d.Status {
	case translate.StatusDone:
		mark = okStyle.Render("✓")
	case translate.StatusCached:
		mark = okStyle.Render("◆")
	case translate.StatusFailed:
		mark = errStyle.Render("✗")
	case translate.StatusRunning:
		mark = spin
	default:
		mark = dimStyle.Render("·")
	}

	label := truncate(d.Title, 46)
	style := valueStyle
	if current {
		style = accentStyle
	}
	line := "  " + mark + " " + style.Render(label)

	switch {
	case d.Status == translate.StatusRunning && d.TotalSegments > 0:
		line += dimStyle.Render(fmt.Sprintf("  %d/%d segments", d.DoneSegments, d.TotalSegments))
	case d.Status == translate.StatusDone:
		line += dimStyle.Render(fmt.Sprintf("  %d/%d segments en %s", d.Translated, d.TotalSegments, d.Duration.Round(time.Second)))
	case d.Status == translate.StatusCached:
		line += dimStyle.Render("  réutilisé")
	case d.Status == translate.StatusFailed && d.Err != nil:
		line += errStyle.Render("  " + truncate(d.Err.Error(), 50))
	}
	if d.Pending > 0 {
		line += warnStyle.Render(fmt.Sprintf("  ⚑ %d", d.Pending))
	} else if d.Notes > 0 {
		line += warnStyle.Render(fmt.Sprintf("  ⚑ %d", d.Notes))
	}
	return line
}

func (m Model) viewReport() string {
	var b strings.Builder
	if m.failure != "" {
		b.WriteString(header("traduction interrompue") + "\n\n")
	} else {
		b.WriteString(header("traduction terminée") + "\n\n")
	}
	if m.result == nil {
		b.WriteString(dimStyle.Render("aucun résultat"))
		return b.String()
	}

	translated, totalSegments := m.result.Segments()
	var done, cached, failed int
	for _, d := range m.result.Documents {
		switch d.Status {
		case translate.StatusDone:
			done++
		case translate.StatusCached:
			cached++
		case translate.StatusFailed:
			failed++
		}
	}

	rows := []string{
		labelStyle.Render("documents ") + valueStyle.Render(fmt.Sprintf("%d traduits, %d réutilisés, %d en échec", done, cached, failed)),
		labelStyle.Render("segments  ") + valueStyle.Render(segmentsLine(translated, totalSegments)),
		labelStyle.Render("requêtes  ") + valueStyle.Render(requestsLine(m.result.Requests, m.result.Attempted)),
		labelStyle.Render("jetons    ") + valueStyle.Render(usageLine(m.result.Usage, m.result.Attempted)),
		labelStyle.Render("durée     ") + valueStyle.Render(m.result.Duration.Round(time.Second).String()),
	}
	if m.failure == "" {
		rows = append(rows, labelStyle.Render("fichier   ")+okStyle.Render(m.outPath))
	}
	b.WriteString(panelStyle.Render(strings.Join(rows, "\n")))

	if bad := m.result.Failed(); len(bad) > 0 {
		b.WriteString("\n\n" + errStyle.Render(fmt.Sprintf("✗ %d document(s) non traduits", len(bad))) +
			dimStyle.Render(" — ils figurent en langue source dans le fichier produit") + "\n")
		for i, d := range bad {
			if i == 5 {
				b.WriteString(dimStyle.Render(fmt.Sprintf("  … et %d autres\n", len(bad)-5)))
				break
			}
			b.WriteString(dimStyle.Render("  "+truncate(d.Title, 40)) + errStyle.Render("  "+truncate(errText(d.Err), 70)) + "\n")
		}
	}

	if n := len(m.result.Notes); n > 0 {
		b.WriteString("\n\n" + warnStyle.Render(fmt.Sprintf("⚑ %d passage(s) signalé(s)", n)) +
			dimStyle.Render(" — le texte source a été conservé là où la traduction n'était pas exploitable") + "\n")
		for i, note := range m.result.Notes {
			if i == 8 {
				b.WriteString(dimStyle.Render(fmt.Sprintf("  … et %d autres\n", n-8)))
				break
			}
			b.WriteString(dimStyle.Render("  " + truncate(note.String(), 100) + "\n"))
		}
	}

	if m.result.Retryable() {
		b.WriteString("\n\n" + help("p", "reprendre les passages non traduits", "entrée", "retour au menu"))
	} else {
		b.WriteString("\n\n" + help("entrée", "retour au menu"))
	}
	return b.String()
}

func (m Model) viewResume() string {
	var b strings.Builder
	b.WriteString(header("traductions reprenables") + "\n\n")
	if len(m.runs) == 0 {
		b.WriteString(dimStyle.Render("Aucune traduction en cache.\n\nUne traduction interrompue est conservée automatiquement ;\nrelancer le même livre dans la même langue reprend là où elle s'est arrêtée."))
		b.WriteString("\n\n" + help("échap", "retour"))
		return b.String()
	}
	for i, r := range m.runs {
		label := r.BookTitle
		if label == "" {
			label = r.Source
		}
		detail := fmt.Sprintf("→ %s · %s · %d document(s) en cache", r.TargetLanguage, r.Model, r.Done())
		if p := r.Pending(); p > 0 {
			detail += fmt.Sprintf(" · %d passage(s) à reprendre", p)
		}
		detail += " · " + r.UpdatedAt.Format("2006-01-02 15:04")
		b.WriteString(selectLine(i == m.runsIndex, truncate(label, 50), detail) + "\n")
	}
	b.WriteString("\n" + help("entrée", "reprendre", "d", "supprimer du cache", "échap", "retour"))
	return b.String()
}

func usageLine(u llm.Usage, attempted int) string {
	if attempted == 0 {
		return "aucun appel"
	}
	if !u.Reported {
		return "non communiqués par le fournisseur"
	}
	s := fmt.Sprintf("%s entrants · %s sortants", thousands(int(u.InputTokens)), thousands(int(u.OutputTokens)))
	if u.CacheReadTokens > 0 {
		s += fmt.Sprintf(" · %s lus en cache", thousands(int(u.CacheReadTokens)))
	}
	return s
}

// segmentsLine states what was actually translated. A run served entirely from
// the cache has nothing to count, and "0 sur 0" would read as a failure.
func segmentsLine(translated, total int) string {
	if total == 0 {
		return "aucun à traduire lors de cette passe"
	}
	return fmt.Sprintf("%d traduits sur %d", translated, total)
}

func requestsLine(requests, attempted int) string {
	if attempted == 0 {
		return "aucune — tout venait du cache de reprise"
	}
	if attempted == requests {
		return fmt.Sprintf("%d", requests)
	}
	return fmt.Sprintf("%d réussies sur %d appels", requests, attempted)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func thousands(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	return strings.Join(append([]string{s}, parts...), " ")
}

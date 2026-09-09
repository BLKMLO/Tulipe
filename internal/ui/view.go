package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/blkmlo/tulipe/internal/i18n"
	"github.com/blkmlo/tulipe/internal/llm"
	"github.com/blkmlo/tulipe/internal/translate"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
	return m.fitWidth(out.String()) + "\n"
}

// fitWidth cuts every line of a finished view down to the terminal's width.
//
// It is applied once, here, rather than at each of the thirty places a value
// is printed. A line that is too long does not merely look wrong: the terminal
// wraps it, and the extra rows are ones the screens counted on having, so a
// long file path or an API-key status pushes the bottom of the screen out of
// sight. Cutting first is what makes a view's measured height its real height.
//
// The cut is escape-aware, so a truncated line keeps its colours and does not
// leak a half-written sequence into the next one.
func (m Model) fitWidth(view string) string {
	width := m.width
	if width <= 0 {
		// No size reported yet: leave the view alone rather than guess at a
		// width and cut something that would have fitted.
		return view
	}
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if ansi.StringWidth(line) > width {
			lines[i] = ansi.Truncate(line, width, "…")
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) viewMenu() string {
	var b strings.Builder
	b.WriteString(header(i18n.T("ui.tagline")) + "\n\n")

	lines := make([]string, 0, len(m.menu))
	for i, e := range m.menu {
		lines = append(lines, selectLine(i == m.menuIndex, e.label, e.detail))
	}
	b.WriteString(strings.Join(lines, "\n"))

	b.WriteString("\n\n" + panelStyle.Render(strings.Join([]string{
		lbl("ui.label.model", 9) + valueStyle.Render(m.cfg.Model) + dimStyle.Render(" via "+m.cfg.Provider),
		lbl("ui.label.language", 9) + valueStyle.Render(m.cfg.TargetLanguage),
		lbl("ui.label.api-key", 9) + valueStyle.Render(m.cfg.KeyStatus()),
	}, "\n")))

	if m.loading {
		b.WriteString("\n\n" + m.spin.View() + dimStyle.Render(" "+i18n.T("ui.working")))
	}
	b.WriteString("\n\n" + m.help(
		i18n.T("ui.key.up-down"), i18n.T("ui.act.navigate"),
		i18n.T("ui.key.enter"), i18n.T("ui.act.choose"),
		"q", i18n.T("ui.act.quit")))
	return b.String()
}

func (m Model) viewPicker() string {
	var b strings.Builder
	b.WriteString(header(i18n.T("ui.picker.title")) + "\n\n")
	b.WriteString(dimStyle.Render(m.picker.CurrentDirectory) + "\n\n")
	b.WriteString(m.picker.View())
	if m.loading {
		b.WriteString("\n" + m.spin.View() + dimStyle.Render(" "+i18n.T("ui.picker.loading")))
	}
	b.WriteString("\n" + m.help(
		i18n.T("ui.key.up-down"), i18n.T("ui.act.browse"),
		i18n.T("ui.key.enter"), i18n.T("ui.act.open"),
		i18n.T("ui.key.esc"), i18n.T("ui.act.back")))
	return b.String()
}

// bookLabel is the width of the label column on the book screen. It is set
// once here rather than baked into each string: "source language" and "langue
// source" are not the same length, and the column must line up in both.
const bookLabel = 16

func (m Model) viewBook() string {
	if m.book == nil {
		return header("") + "\n\n" + i18n.T("ui.book.none")
	}
	var b strings.Builder
	b.WriteString(header(m.bookTitle()) + "\n\n")

	rows := []string{
		lbl("ui.book.file", bookLabel) + valueStyle.Render(m.bookPath),
		lbl("ui.book.source-language", bookLabel) + valueStyle.Render(orDash(m.book.Language)),
		lbl("ui.book.documents", bookLabel) + valueStyle.Render(i18n.T("ui.book.documents.value", m.stats.Documents, len(m.book.Chapters))),
		lbl("ui.book.to-translate", bookLabel) + valueStyle.Render(i18n.T("ui.book.segments.value", m.stats.Segments, thousands(m.stats.Chars))),
		lbl("ui.book.into", bookLabel) + accentStyle.Render(m.cfg.TargetLanguage) + dimStyle.Render("  ·  "+m.cfg.Model),
		lbl("ui.book.output", bookLabel) + valueStyle.Render(m.outPath),
	}
	if m.reusable > 0 {
		rows = append(rows, lbl("ui.book.resume", bookLabel)+okStyle.Render(i18n.T("ui.book.resume.value", m.reusable)))
	}
	if m.pending > 0 {
		rows = append(rows, lbl("ui.book.pending", bookLabel)+warnStyle.Render(i18n.T("ui.book.pending.value", m.pending)))
	}
	b.WriteString(panelStyle.Render(strings.Join(rows, "\n")))
	head := b.String()

	keys := []string{i18n.T("ui.key.enter"), i18n.T("ui.act.translate")}
	if m.pending > 0 {
		keys = append(keys, "p", i18n.T("ui.act.retry-pending"))
	}
	keys = append(keys, "r", i18n.T("ui.act.clear-cache"), i18n.T("ui.key.esc"), i18n.T("ui.act.back"))
	foot := "\n" + dimStyle.Render(i18n.T("ui.book.explanation", m.cfg.ChunkChars)) +
		"\n\n" + m.help(keys...)

	// How many chapters are listed follows the terminal rather than a fixed
	// ten: ten was too many on a split pane, where the list pushed the keys
	// off the bottom, and needlessly few on a tall one. The label and the
	// blank line above it cost two rows, so they are part of the same budget:
	// on a terminal with no room for a single chapter the whole block goes,
	// rather than half of it hanging past the edge.
	const chapterHeader = 2
	limit := m.rows() - lipgloss.Height(head) - lipgloss.Height(foot) - chapterHeader
	if len(m.book.Chapters) > limit {
		limit-- // the "and N more" line is a row too
	}

	if limit >= 1 {
		b.WriteString("\n\n" + labelStyle.Render(i18n.T("ui.book.chapters")) + "\n")
		limit = min(limit, len(m.book.Chapters))
		for i, ch := range m.book.Chapters {
			if i == limit {
				b.WriteString(dimStyle.Render(i18n.T("ui.book.and-more", len(m.book.Chapters)-limit)))
				break
			}
			b.WriteString(dimStyle.Render(fmt.Sprintf("  %2d. ", i+1)) + valueStyle.Render(truncate(ch.Title, 60)) + "\n")
		}
	}
	b.WriteString(foot)
	return b.String()
}

func (m Model) viewRun() string {
	var b strings.Builder
	b.WriteString(header(i18n.T("ui.run.title")) + "\n\n")

	if m.run == nil {
		b.WriteString(dimStyle.Render(i18n.T("ui.run.none")))
		return b.String() + "\n\n" + m.help(i18n.T("ui.key.esc"), i18n.T("ui.act.back"))
	}

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
	b.WriteString(m.bar.ViewAs(ratio) + "  " + valueStyle.Render(i18n.T("ui.run.documents", done, len(docs))) + "\n\n")

	// Window the document list around the one being worked on.
	current := m.run.progress.Current
	start := max(0, current-3)
	end := min(len(docs), start+9)
	for i := start; i < end; i++ {
		b.WriteString(docLine(docs[i], i == current, m.spin.View()) + "\n")
	}

	b.WriteString("\n" + panelStyle.Render(strings.Join([]string{
		lbl("ui.run.elapsed", 8) + valueStyle.Render(m.elapsed.Round(time.Second).String()),
		lbl("ui.run.tokens", 8) + valueStyle.Render(usageLine(m.run.progress.Usage, m.run.progress.Attempted)),
	}, "\n")))

	if n := len(m.run.log); n > 0 {
		b.WriteString("\n\n" + labelStyle.Render(i18n.T("ui.run.log")) + "\n")
		for _, line := range m.run.log[max(0, n-5):] {
			b.WriteString("  " + line + "\n")
		}
	}

	b.WriteString("\n" + m.help(i18n.T("ui.key.esc"), i18n.T("ui.act.cancel-run")))
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
		line += dimStyle.Render(i18n.T("ui.run.segments", d.DoneSegments, d.TotalSegments))
	case d.Status == translate.StatusDone:
		line += dimStyle.Render(i18n.T("ui.run.done", d.Translated, d.TotalSegments, d.Duration.Round(time.Second)))
	case d.Status == translate.StatusCached:
		line += dimStyle.Render(i18n.T("ui.run.reused"))
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
		b.WriteString(header(i18n.T("ui.report.interrupted")) + "\n\n")
	} else {
		b.WriteString(header(i18n.T("ui.report.finished")) + "\n\n")
	}
	if m.result == nil {
		b.WriteString(dimStyle.Render(i18n.T("ui.report.none")))
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

	const w = 10
	rows := []string{
		lbl("ui.report.documents", w) + valueStyle.Render(i18n.T("ui.report.documents.value", done, cached, failed)),
		lbl("ui.report.segments", w) + valueStyle.Render(segmentsLine(translated, totalSegments)),
		lbl("ui.report.requests", w) + valueStyle.Render(requestsLine(m.result.Requests, m.result.Attempted, m.result.Cached())),
		lbl("ui.report.tokens", w) + valueStyle.Render(usageLine(m.result.Usage, m.result.Attempted)),
		lbl("ui.report.duration", w) + valueStyle.Render(m.result.Duration.Round(time.Second).String()),
	}
	if m.failure == "" {
		rows = append(rows, lbl("ui.report.file", w)+okStyle.Render(m.outPath))
	}
	b.WriteString(panelStyle.Render(strings.Join(rows, "\n")))

	if bad := m.result.Failed(); len(bad) > 0 {
		b.WriteString("\n\n" + errStyle.Render(i18n.T("ui.report.failed", len(bad))) +
			dimStyle.Render(i18n.T("ui.report.failed.detail")) + "\n")
		for i, d := range bad {
			if i == 5 {
				b.WriteString(dimStyle.Render(i18n.T("ui.book.and-more", len(bad)-5)))
				break
			}
			b.WriteString(dimStyle.Render("  "+truncate(d.Title, 40)) + errStyle.Render("  "+truncate(errText(d.Err), 70)) + "\n")
		}
	}

	if n := len(m.result.Notes); n > 0 {
		b.WriteString("\n\n" + warnStyle.Render(i18n.T("ui.report.notes", n)) +
			dimStyle.Render(i18n.T("ui.report.notes.detail")) + "\n")
		for i, note := range m.result.Notes {
			if i == 8 {
				b.WriteString(dimStyle.Render(i18n.T("ui.book.and-more", n-8)))
				break
			}
			b.WriteString(dimStyle.Render("  " + truncate(note.String(), 100) + "\n"))
		}
	}

	if m.result.Retryable() {
		b.WriteString("\n\n" + m.help(
			"p", i18n.T("ui.act.retry-untranslated"),
			i18n.T("ui.key.enter"), i18n.T("ui.act.back-to-menu")))
	} else {
		b.WriteString("\n\n" + m.help(i18n.T("ui.key.enter"), i18n.T("ui.act.back-to-menu")))
	}
	return b.String()
}

func (m Model) viewResume() string {
	var b strings.Builder
	b.WriteString(header(i18n.T("ui.resume.title")) + "\n\n")
	if len(m.runs) == 0 {
		b.WriteString(dimStyle.Render(i18n.T("ui.resume.empty")))
		b.WriteString("\n\n" + m.help(i18n.T("ui.key.esc"), i18n.T("ui.act.back")))
		return b.String()
	}
	for i, r := range m.runs {
		label := r.BookTitle
		if label == "" {
			label = r.Source
		}
		detail := i18n.T("ui.resume.detail", r.TargetLanguage, r.Model, r.Done())
		if p := r.Pending(); p > 0 {
			detail += i18n.T("ui.resume.pending", p)
		}
		detail += " · " + r.UpdatedAt.Format("2006-01-02 15:04")
		b.WriteString(selectLine(i == m.runsIndex, truncate(label, 50), detail) + "\n")
	}
	b.WriteString("\n" + m.help(
		i18n.T("ui.key.enter"), i18n.T("ui.act.resume"),
		"d", i18n.T("ui.act.delete"),
		i18n.T("ui.key.esc"), i18n.T("ui.act.back")))
	return b.String()
}

func usageLine(u llm.Usage, attempted int) string {
	if attempted == 0 {
		return i18n.T("ui.usage.no-call")
	}
	if !u.Reported {
		return i18n.T("ui.usage.not-reported")
	}
	s := i18n.T("ui.usage.in-out", thousands(int(u.InputTokens)), thousands(int(u.OutputTokens)))
	if u.CacheReadTokens > 0 {
		s += i18n.T("ui.usage.cache-read", thousands(int(u.CacheReadTokens)))
	}
	return s
}

// segmentsLine states what was actually translated. A run served entirely from
// the cache has nothing to count, and "0 out of 0" would read as a failure.
func segmentsLine(translated, total int) string {
	if total == 0 {
		return i18n.T("ui.segments.none")
	}
	return i18n.T("ui.segments.count", translated, total)
}

// requestsLine explains a run's request count, and in particular a count of
// zero. Zero used to be reported as "everything came from the resume cache",
// which is only one of its causes: a book whose every document is a title, with
// titles turned off, makes no request either and was never cached. Saying so
// wrongly tells the reader their book came from somewhere it did not.
func requestsLine(requests, attempted, cached int) string {
	if attempted == 0 {
		if cached == 0 {
			return i18n.T("ui.requests.nothing-to-do")
		}
		return i18n.T("ui.requests.none")
	}
	if attempted == requests {
		return fmt.Sprintf("%d", requests)
	}
	return i18n.T("ui.requests.mixed", requests, attempted)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func orDash(s string) string {
	if s == "" {
		return i18n.T("ui.dash")
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

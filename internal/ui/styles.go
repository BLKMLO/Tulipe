package ui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/blkmlo/tulipe/internal/i18n"
)

// The palette is adaptive: every colour has a variant for light and dark
// terminals, so Tulipe stays readable whatever the user's theme.
var (
	petal  = lipgloss.AdaptiveColor{Light: "#B03060", Dark: "#F783AC"}
	petalD = lipgloss.AdaptiveColor{Light: "#8A2249", Dark: "#D6558A"}
	leaf   = lipgloss.AdaptiveColor{Light: "#2F7D4F", Dark: "#69DB7C"}
	amber  = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#FFD43B"}
	rust   = lipgloss.AdaptiveColor{Light: "#B02D2D", Dark: "#FF8787"}
	ink    = lipgloss.AdaptiveColor{Light: "#1C1C21", Dark: "#E8E8EF"}
	muted  = lipgloss.AdaptiveColor{Light: "#6C6C78", Dark: "#8E8E9E"}
	faint  = lipgloss.AdaptiveColor{Light: "#A8A8B3", Dark: "#5C5C68"}
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(petal)

	subtitleStyle = lipgloss.NewStyle().Foreground(muted)

	frameStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(petalD).
			Padding(0, 2)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(faint).
			PaddingLeft(2)

	itemStyle = lipgloss.NewStyle().Foreground(ink).PaddingLeft(2)

	selectedStyle = lipgloss.NewStyle().Foreground(petal).Bold(true).PaddingLeft(0).
			SetString("› ")

	labelStyle = lipgloss.NewStyle().Foreground(muted)

	valueStyle = lipgloss.NewStyle().Foreground(ink)

	accentStyle = lipgloss.NewStyle().Foreground(petal)

	okStyle = lipgloss.NewStyle().Foreground(leaf)

	warnStyle = lipgloss.NewStyle().Foreground(amber)

	errStyle = lipgloss.NewStyle().Foreground(rust)

	helpStyle = lipgloss.NewStyle().Foreground(faint)

	dimStyle = lipgloss.NewStyle().Foreground(muted)
)

// header is the banner shown on every screen.
func header(sub string) string {
	title := titleStyle.Render("🌷 Tulipe")
	if sub == "" {
		return title
	}
	return title + subtitleStyle.Render("  ·  "+sub)
}

// lbl renders a field label padded to a fixed column. The padding is applied
// after translation, not written into the string: a label is not the same
// length in every language, and the column has to line up in all of them.
func lbl(key string, width int) string {
	return labelStyle.Render(pad(i18n.T(key), width))
}

// help renders the key hints at the bottom of a screen, wrapped to the
// terminal.
//
// Wrapping rather than letting the line run: the hints are cut to the screen
// width like everything else, and the key that would fall off the end is
// "s save" — a reader on a narrow terminal could no longer find out how to
// keep their settings. The row a wrap costs is measured with the rest of the
// chrome, so it comes out of the list above rather than off the bottom.
func (m Model) help(pairs ...string) string {
	var out string
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			out += helpStyle.Render("  •  ")
		}
		out += accentStyle.Render(pairs[i]) + helpStyle.Render(" "+pairs[i+1])
	}
	if m.width <= 0 || lipgloss.Width(out) <= m.width {
		return out
	}
	return lipgloss.NewStyle().Width(m.width).Render(out)
}

// selectLine renders one line of a vertical selection list.
func selectLine(selected bool, label, detail string) string {
	prefix := "  "
	style := itemStyle.PaddingLeft(0)
	if selected {
		prefix = accentStyle.Render("› ")
		style = lipgloss.NewStyle().Foreground(petal).Bold(true)
	}
	line := prefix + style.Render(label)
	if detail != "" {
		line += dimStyle.Render("  " + detail)
	}
	return line
}

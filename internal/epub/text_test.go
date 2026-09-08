package epub

import (
	"strings"
	"testing"
)

func TestPlainTextRendersReadableProse(t *testing.T) {
	doc := []byte(`<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Ignoré</title><style>p { color: red }</style></head>
<body>
<h1>Premiers pas</h1>
<p>Une journée <em>froide</em> et lumineuse &amp; claire.</p>
<p>Deuxième
paragraphe   avec   des espaces.</p>
<ul><li>un</li><li>deux</li></ul>
<script>alert("non")</script>
<p>Fin.<br/>Après le saut.</p>
</body></html>`)

	got := PlainText(doc)
	want := strings.Join([]string{
		"Premiers pas",
		"Une journée froide et lumineuse & claire.",
		"Deuxième paragraphe avec des espaces.",
		"un",
		"deux",
		"Fin. Après le saut.",
	}, "\n\n")
	if got != want {
		t.Errorf("PlainText =\n%q\nwant\n%q", got, want)
	}
	for _, forbidden := range []string{"<", "&amp;", "alert", "color: red", "Ignoré"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("the rendering still carries %q", forbidden)
		}
	}
}

func TestPlainTextKeepsPreformattedLines(t *testing.T) {
	doc := []byte("<html><body><p>Avant.</p><pre>ligne 1\n  ligne 2</pre><p>Après.</p></body></html>")
	got := PlainText(doc)
	if !strings.Contains(got, "ligne 1\n  ligne 2") {
		t.Errorf("preformatted lines were collapsed:\n%q", got)
	}
}

func TestBookPlainTextFollowsReadingOrder(t *testing.T) {
	b, err := Parse(buildEPUB(t))
	if err != nil {
		t.Fatal(err)
	}
	got := b.PlainText()
	for _, want := range []string{"A Short Book", "First Steps", "bright cold day", "Second Wind", "striking thirteen"} {
		if !strings.Contains(got, want) {
			t.Errorf("the export is missing %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "First Steps") > strings.Index(got, "Second Wind") {
		t.Error("chapters are not in reading order")
	}
	// The chapter title also opens the document as an <h1>; printing it twice
	// in a row would read badly.
	if strings.Contains(got, "First Steps\n-----------\n\nFirst Steps") {
		t.Error("the chapter title is printed twice")
	}
}

func TestPlainTextOnEmptyDocument(t *testing.T) {
	if got := PlainText([]byte(`<html><body></body></html>`)); got != "" {
		t.Errorf("PlainText = %q, want empty", got)
	}
	if got := PlainText(nil); got != "" {
		t.Errorf("PlainText(nil) = %q, want empty", got)
	}
}

func TestBookPlainTextDoesNotRepeatTranslatedTitles(t *testing.T) {
	b, err := Parse(buildEPUB(t))
	if err != nil {
		t.Fatal(err)
	}
	// Translate the chapter but not the table of contents, which is what the
	// book looks like mid-run — and what used to print every title twice.
	b.Replace("OEBPS/text/ch1.xhtml", []byte(`<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Un</title></head>
<body><h1>Premiers pas</h1><p>Une journée froide.</p></body></html>`))

	got := b.PlainText()
	if strings.Contains(got, "First Steps") {
		t.Errorf("the original chapter label leaked into the export:\n%s", got)
	}
	if n := strings.Count(got, "Premiers pas"); n != 1 {
		t.Errorf("the chapter title appears %d times, want once:\n%s", n, got)
	}
}

func TestBookPlainTextLabelsAChapterWithoutHeading(t *testing.T) {
	b, err := Parse(buildEPUB(t))
	if err != nil {
		t.Fatal(err)
	}
	b.Replace("OEBPS/text/ch1.xhtml", []byte(`<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Ouverture</title></head>
<body><p>Sans titre apparent.</p></body></html>`))
	got := b.PlainText()
	if !strings.Contains(got, "Ouverture\n---------") {
		t.Errorf("a chapter without a heading must still be labelled:\n%s", got)
	}
}

func TestBookPlainTextHasNoStrayBlankLines(t *testing.T) {
	b, err := Parse(buildEPUB(t))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.PlainText(), "\n\n\n") {
		t.Errorf("the export runs three newlines together:\n%q", b.PlainText())
	}
}

package epub

import (
	"errors"
	"strings"
	"testing"
)

const sampleDoc = `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" lang="en">
<head><title>Chapter One</title><link rel="stylesheet" href="s.css"/></head>
<body epub:type="bodymatter">
<h1>The Beginning</h1>
<p>He said <em>hello</em> to the world &amp; smiled.<br/></p>
<p class="empty">   </p>
<pre>func main() { println("keep me") }</pre>
<blockquote><p>Nested prose.</p></blockquote>
<div>Loose text under a div.</div>
<img src="a.png" alt="an untouched alt"/>
</body>
</html>`

func TestExtractFindsProseAndSkipsTheRest(t *testing.T) {
	segs, err := Extract([]byte(sampleDoc))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var got []string
	for _, s := range segs {
		got = append(got, s.Source)
	}
	want := []string{
		"Chapter One",
		"The Beginning",
		"He said <em>hello</em> to the world &amp; smiled.<br/>",
		"<p>Nested prose.</p>",
		"Loose text under a div.",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d segments %q, want %d %q", len(got), got, len(want), want)
	}
	for i := range want {
		if strings.TrimSpace(got[i]) != want[i] {
			t.Errorf("segment %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestExtractSegmentsDoNotOverlap(t *testing.T) {
	segs, err := Extract([]byte(sampleDoc))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	prev := 0
	for i, s := range segs {
		if s.Start < prev {
			t.Fatalf("segment %d starts at %d, before the end of the previous one (%d)", i, s.Start, prev)
		}
		if s.Source != sampleDoc[s.Start:s.End] {
			t.Fatalf("segment %d source does not match its byte range", i)
		}
		prev = s.End
	}
}

func TestApplyPreservesEverythingOutsideSegments(t *testing.T) {
	doc := []byte(sampleDoc)
	segs, err := Extract(doc)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	tr := make([]string, len(segs))
	for i, s := range segs {
		if s.Kind == KindBlock {
			tr[i] = strings.ReplaceAll(s.Source, "hello", "bonjour")
			tr[i] = strings.ReplaceAll(tr[i], "Nested prose.", "Prose imbriquee.")
		} else {
			tr[i] = "TRADUIT"
		}
	}
	out, err := Apply(doc, segs, tr)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := string(out)
	for _, keep := range []string{
		`<?xml version="1.0" encoding="utf-8"?>`,
		`<!DOCTYPE html>`,
		`xmlns:epub="http://www.idpf.org/2007/ops"`,
		`<pre>func main() { println("keep me") }</pre>`,
		`alt="an untouched alt"`,
		`<p class="empty">   </p>`,
		`<br/>`,
	} {
		if !strings.Contains(got, keep) {
			t.Errorf("Apply dropped %q from the document", keep)
		}
	}
	if !strings.Contains(got, "<em>bonjour</em>") {
		t.Error("block translation was not spliced back")
	}
	if !strings.Contains(got, "<title>TRADUIT</title>") {
		t.Error("text translation was not spliced back")
	}
	if _, err := Extract(out); err != nil {
		t.Fatalf("document is no longer parseable after Apply: %v", err)
	}
}

func TestApplyEscapesPlainTextButNotMarkup(t *testing.T) {
	doc := []byte(`<html><body><div>x</div><p>y</p></body></html>`)
	segs, err := Extract(doc)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	out, err := Apply(doc, segs, []string{`a & b <c>`, `<em>d & e</em>`})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := `<html><body><div>a &amp; b &lt;c&gt;</div><p><em>d & e</em></p></body></html>`
	if string(out) != want {
		t.Errorf("Apply =\n%s\nwant\n%s", out, want)
	}
}

func TestApplyKeepsSourceOnEmptyTranslation(t *testing.T) {
	doc := []byte(`<html><body><p>original</p></body></html>`)
	segs, _ := Extract(doc)
	out, err := Apply(doc, segs, []string{"  "})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if string(out) != string(doc) {
		t.Errorf("Apply = %s, want the untouched source", out)
	}
}

func TestApplyRejectsMismatchedCounts(t *testing.T) {
	doc := []byte(`<html><body><p>a</p><p>b</p></body></html>`)
	segs, _ := Extract(doc)
	if _, err := Apply(doc, segs, []string{"x"}); err == nil {
		t.Fatal("Apply accepted 1 translation for 2 segments")
	}
}

func TestWellFormed(t *testing.T) {
	for _, ok := range []string{`plain`, `a <em>b</em> c`, `x &amp; y`, `<br/>`, `un &nbsp; espace`} {
		if err := WellFormed(ok); err != nil {
			t.Errorf("WellFormed(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{`<em>unclosed`, `</p>`, `<a href=>x</a>`} {
		if err := WellFormed(bad); err == nil {
			t.Errorf("WellFormed(%q) = nil, want an error", bad)
		}
	}
}

func TestTagsCountsMarkup(t *testing.T) {
	got := Tags(`<em>a</em> <em>b</em> <a href="#">c</a>`)
	if got["em"] != 2 || got["a"] != 1 || len(got) != 2 {
		t.Errorf("Tags = %v, want map[a:1 em:2]", got)
	}
}

func TestDocumentTitlePrefersHeading(t *testing.T) {
	if got := DocumentTitle([]byte(sampleDoc)); got != "The Beginning" {
		t.Errorf("DocumentTitle = %q, want %q", got, "The Beginning")
	}
	noHeading := `<html><head><title>Fallback</title></head><body><p>x</p></body></html>`
	if got := DocumentTitle([]byte(noHeading)); got != "Fallback" {
		t.Errorf("DocumentTitle = %q, want %q", got, "Fallback")
	}
}

func TestExtractHandlesNCX(t *testing.T) {
	ncx := `<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/"><navMap>` +
		`<navPoint id="n1"><navLabel><text>Chapter One</text></navLabel><content src="c1.xhtml"/></navPoint>` +
		`</navMap></ncx>`
	segs, err := Extract([]byte(ncx))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(segs) != 1 || strings.TrimSpace(segs[0].Source) != "Chapter One" {
		t.Fatalf("segments = %+v, want the single navLabel text", segs)
	}
}

func TestWellFormedAcceptsEPUBRealities(t *testing.T) {
	for _, ok := range []string{
		`<span epub:type="pagebreak" id="p12"/>`,
		`l&#8217;auteur &mdash; d&nbsp;accord`,
		`<a href="notes.xhtml#n1">1</a>`,
	} {
		if err := WellFormed(ok); err != nil {
			t.Errorf("WellFormed(%q) = %v, want nil", ok, err)
		}
	}
}

func TestSetDocumentLanguage(t *testing.T) {
	in := []byte(`<?xml version="1.0"?>` + "\n" +
		`<html xmlns="http://www.w3.org/1999/xhtml" lang="en" xml:lang="en">` +
		`<body><p lang="en">keep this one</p></body></html>`)
	out := SetDocumentLanguage(in, "fr")
	got := string(out)
	if !strings.Contains(got, `lang="fr" xml:lang="fr"`) {
		t.Errorf("root language not rewritten:\n%s", got)
	}
	if !strings.Contains(got, `<p lang="en">`) {
		t.Error("a language attribute outside the root element was rewritten")
	}
	if !strings.Contains(got, `<?xml version="1.0"?>`) {
		t.Error("the prolog was lost")
	}
	if same := SetDocumentLanguage(in, ""); string(same) != string(in) {
		t.Error("an empty code must leave the document untouched")
	}
	noLang := []byte(`<html xmlns="http://www.w3.org/1999/xhtml"><body><p>x</p></body></html>`)
	if got := SetDocumentLanguage(noLang, "fr"); string(got) != string(noLang) {
		t.Error("a document without a language attribute must be left alone")
	}
}

func TestExtractRefusesNonUTF8Declarations(t *testing.T) {
	for _, enc := range []string{"ISO-8859-1", "UTF-16", "windows-1252"} {
		doc := []byte(`<?xml version="1.0" encoding="` + enc + `"?><html><body><p>x</p></body></html>`)
		_, err := Extract(doc)
		var target *UnsupportedEncodingError
		if !errors.As(err, &target) {
			t.Errorf("Extract with encoding %s = %v, want an UnsupportedEncodingError", enc, err)
			continue
		}
		if !strings.Contains(err.Error(), enc) {
			t.Errorf("the error does not name the encoding: %v", err)
		}
	}
	for _, enc := range []string{"utf-8", "UTF-8", "us-ascii"} {
		doc := []byte(`<?xml version="1.0" encoding="` + enc + `"?><html><body><p>x</p></body></html>`)
		if _, err := Extract(doc); err != nil {
			t.Errorf("Extract with encoding %s = %v, want nil", enc, err)
		}
	}
	// No declaration at all is UTF-8 by default and must be accepted.
	if _, err := Extract([]byte(`<html><body><p>x</p></body></html>`)); err != nil {
		t.Errorf("Extract without a declaration = %v, want nil", err)
	}
}

func TestASCIIDeclarationKeepsExactOffsets(t *testing.T) {
	doc := []byte(`<?xml version="1.0" encoding="us-ascii"?><html><body><p>hello</p><p>world</p></body></html>`)
	segs, err := Extract(doc)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(segs) != 2 {
		t.Fatalf("got %d segments, want 2", len(segs))
	}
	for i, s := range segs {
		if s.Source != string(doc[s.Start:s.End]) {
			t.Errorf("segment %d: offsets %d..%d do not match its source %q", i, s.Start, s.End, s.Source)
		}
	}
	out, err := Apply(doc, segs, []string{"bonjour", "monde"})
	if err != nil {
		t.Fatal(err)
	}
	want := `<?xml version="1.0" encoding="us-ascii"?><html><body><p>bonjour</p><p>monde</p></body></html>`
	if string(out) != want {
		t.Errorf("Apply =\n%s\nwant\n%s", out, want)
	}
}

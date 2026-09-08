package epub

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// buildEPUB assembles a minimal but valid EPUB 3 container for the tests.
func buildEPUB(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string, method uint16) {
		t.Helper()
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	add("mimetype", "application/epub+zip", zip.Store)
	add("META-INF/container.xml", `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
<rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`, zip.Deflate)
	add("OEBPS/content.opf", `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="id">
<metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
<dc:title>A Short Book</dc:title><dc:language>en</dc:language><dc:identifier id="id">urn:uuid:1</dc:identifier>
</metadata>
<manifest>
<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
<item id="c1" href="text/ch1.xhtml" media-type="application/xhtml+xml"/>
<item id="c2" href="text/ch%202.xhtml" media-type="application/xhtml+xml"/>
<item id="css" href="style.css" media-type="text/css"/>
</manifest>
<spine><itemref idref="c1"/><itemref idref="c2"/></spine>
</package>`, zip.Deflate)
	add("OEBPS/nav.xhtml", `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body>
<nav epub:type="toc"><ol>
<li><a href="text/ch1.xhtml">First Steps</a></li>
<li><a href="text/ch%202.xhtml">Second Wind</a></li>
</ol></nav></body></html>`, zip.Deflate)
	add("OEBPS/text/ch1.xhtml", `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>One</title></head>
<body><h1>First Steps</h1><p>It was a bright cold day.</p></body></html>`, zip.Deflate)
	add("OEBPS/text/ch 2.xhtml", `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Two</title></head>
<body><h1>Second Wind</h1><p>The clocks were striking thirteen.</p></body></html>`, zip.Deflate)
	add("OEBPS/style.css", "body { margin: 0 }", zip.Deflate)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParseReadsSpineAndMetadata(t *testing.T) {
	b, err := Parse(buildEPUB(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if b.Title != "A Short Book" || b.Language != "en" || b.Version != "3.0" {
		t.Errorf("metadata = %q/%q/%q", b.Title, b.Language, b.Version)
	}
	if len(b.Chapters) != 2 {
		t.Fatalf("got %d chapters, want 2", len(b.Chapters))
	}
	// The second href is percent-encoded in the manifest and must resolve to
	// the real archive entry.
	if b.Chapters[1].Path != "OEBPS/text/ch 2.xhtml" {
		t.Errorf("chapter 2 path = %q", b.Chapters[1].Path)
	}
	// Titles come from the navigation document, not from the headings.
	if b.Chapters[0].Title != "First Steps" || b.Chapters[1].Title != "Second Wind" {
		t.Errorf("titles = %q, %q", b.Chapters[0].Title, b.Chapters[1].Title)
	}
	if b.NavPath != "OEBPS/nav.xhtml" {
		t.Errorf("NavPath = %q", b.NavPath)
	}
}

func TestEncodeRoundTripKeepsEverything(t *testing.T) {
	src := buildEPUB(t)
	b, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	b.Replace("OEBPS/text/ch1.xhtml", []byte(`<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Un</title></head>
<body><h1>Premiers pas</h1><p>C'etait une journee froide et lumineuse.</p></body></html>`))
	if err := b.SetLanguage("fr"); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}

	var out bytes.Buffer
	if err := b.Encode(&out); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// The mimetype entry must be first and stored uncompressed (OCF 3.0 §4.2).
	zr, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	if zr.File[0].Name != "mimetype" || zr.File[0].Method != zip.Store {
		t.Errorf("first entry = %q method %d, want mimetype stored", zr.File[0].Name, zr.File[0].Method)
	}

	b2, err := Parse(out.Bytes())
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if b2.Language != "fr" {
		t.Errorf("language = %q, want fr", b2.Language)
	}
	if b2.Title != "A Short Book" {
		t.Errorf("title changed to %q", b2.Title)
	}
	if len(b2.Files) != len(b.Files) {
		t.Errorf("got %d entries after round-trip, want %d", len(b2.Files), len(b.Files))
	}
	css, ok := b2.Read("OEBPS/style.css")
	if !ok || string(css) != "body { margin: 0 }" {
		t.Error("unrelated resource was altered")
	}
	ch1, _ := b2.Read("OEBPS/text/ch1.xhtml")
	if !strings.Contains(string(ch1), "Premiers pas") {
		t.Error("replaced chapter did not survive the round-trip")
	}
	ch2, _ := b2.Read("OEBPS/text/ch 2.xhtml")
	if !strings.Contains(string(ch2), "striking thirteen") {
		t.Error("untouched chapter was altered")
	}
}

func TestSetLanguageOnlyTouchesLanguage(t *testing.T) {
	b, err := Parse(buildEPUB(t))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := b.Read(b.OPFPath)
	if err := b.SetLanguage("fr"); err != nil {
		t.Fatal(err)
	}
	after, _ := b.Read(b.OPFPath)
	if !strings.Contains(string(after), "<dc:language>fr</dc:language>") {
		t.Error("language was not rewritten")
	}
	if !strings.Contains(string(after), "<dc:identifier id=\"id\">urn:uuid:1</dc:identifier>") {
		t.Error("identifier was lost")
	}
	if len(before)-len(after) != 0 {
		// "en" and "fr" are the same length; any other delta means collateral damage.
		t.Errorf("package document changed size by %d bytes", len(after)-len(before))
	}
}

func TestTranslatableDocumentsIncludesNavigation(t *testing.T) {
	b, err := Parse(buildEPUB(t))
	if err != nil {
		t.Fatal(err)
	}
	got := b.TranslatableDocuments()
	want := []string{"OEBPS/text/ch1.xhtml", "OEBPS/text/ch 2.xhtml", "OEBPS/nav.xhtml"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("document %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseRejectsNonEPUB(t *testing.T) {
	if _, err := Parse([]byte("not a zip")); err == nil {
		t.Fatal("Parse accepted a non-zip payload")
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("hello.txt")
	w.Write([]byte("hi"))
	zw.Close()
	if _, err := Parse(buf.Bytes()); err == nil {
		t.Fatal("Parse accepted a zip without container.xml")
	}
}

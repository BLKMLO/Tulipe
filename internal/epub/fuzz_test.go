package epub

import (
	"strings"
	"testing"
)

// FuzzExtractApply feeds arbitrary bytes through the whole splice path. Nothing
// here may panic: a malformed document must be refused with an error, never by
// bringing the program down.
func FuzzExtractApply(f *testing.F) {
	seeds := []string{
		sampleDoc,
		``,
		`<html><body><p>x</p></body></html>`,
		`<?xml version="1.0"?><html/>`,
		`<html><body><p>unclosed`,
		`<html><body><p>&amp;&#8217;&nbsp;&bogus;</p></body></html>`,
		`<html><body><p attr="<">x</p></body></html>`,
		`<!DOCTYPE html><html xmlns:epub="x"><body epub:type="a"><li>y</li></body></html>`,
		`<ncx><navMap><navPoint><navLabel><text>T</text></navLabel></navPoint></navMap></ncx>`,
		"<html><body>" + strings.Repeat("<div>", 60) + "t" + strings.Repeat("</div>", 60) + "</body></html>",
		`<?xml version="1.0" encoding="ISO-8859-1"?><html><body><p>x</p></body></html>`,
		"\x00\x01\x02 not xml at all",
		`<html><body><p>` + strings.Repeat("é", 200) + `</p></body></html>`,
	}
	for _, s := range seeds {
		f.Add(s, "TRADUIT")
	}

	f.Fuzz(func(t *testing.T, doc, translation string) {
		segs, err := Extract([]byte(doc))
		if err != nil {
			return // refusing is a valid outcome; crashing is not
		}

		// Every segment must point inside the document it came from.
		for i, s := range segs {
			if s.Start < 0 || s.End > len(doc) || s.Start > s.End {
				t.Fatalf("segment %d hors du document : %d..%d sur %d octets", i, s.Start, s.End, len(doc))
			}
			if s.Source != doc[s.Start:s.End] {
				t.Fatalf("segment %d ne correspond pas à sa plage d'octets", i)
			}
		}

		translations := make([]string, len(segs))
		for i := range translations {
			translations[i] = translation
		}
		out, err := Apply([]byte(doc), segs, translations)
		if err != nil {
			// Refusing is allowed — a document too broken to splice safely
			// must be turned down, not mangled.
			return
		}

		// Whatever it did return must still be readable by the same reader.
		if _, err := Extract(out); err != nil {
			t.Fatalf("Apply a rendu un document qui ne se relit plus : %v\nentrée : %q\nsortie : %q", err, doc, out)
		}

		// The other rewrites must survive arbitrary input too.
		_ = SetDocumentLanguage([]byte(doc), "fr")
		_ = NormaliseText(translation)
		_ = PlainText([]byte(doc))
		_ = DocumentTitle([]byte(doc))
		_ = WellFormed(translation)
		_ = Tags(translation)
	})
}

// FuzzParse checks the container reader against arbitrary archives.
func FuzzParse(f *testing.F) {
	f.Add([]byte("not a zip"))
	f.Add([]byte("PK\x03\x04"))
	f.Fuzz(func(t *testing.T, data []byte) {
		book, err := Parse(data)
		if err != nil {
			return
		}
		// A book that parsed must be usable without surprises.
		_ = book.TranslatableDocuments()
		_ = book.PlainText()
		_ = book.SetLanguage("fr")
		for _, ch := range book.Chapters {
			if _, ok := book.Read(ch.Path); !ok {
				t.Fatalf("le chapitre %q est listé mais illisible", ch.Path)
			}
		}
	})
}

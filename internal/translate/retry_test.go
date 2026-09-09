package translate

import (
	"context"
	"strings"
	"testing"

	"github.com/blkmlo/tulipe/internal/epub"
)

// flakyOnce refuses the segments naming a word the first time round, then
// translates everything — the shape of a real run where a few paragraphs come
// back unusable.
func refuseContaining(word string) func(int, []string) (string, error) {
	return func(_ int, s []string) (string, error) {
		out := upper(s)
		for i, src := range s {
			if strings.Contains(src, word) {
				out[i] = "" // an empty answer is kept as source
			}
		}
		return envelope(out), nil
	}
}

func TestRetryOnlyTouchesThePendingPassages(t *testing.T) {
	source := []byte(`<html xmlns="http://www.w3.org/1999/xhtml"><body>` +
		`<p>alpha one</p><p>bravo two</p><p>alpha three</p><p>charlie four</p>` +
		`</body></html>`)

	// First pass: everything containing "alpha" comes back empty.
	first := &fakeProvider{answer: refuseContaining("alpha")}
	opts := Options{TargetLanguage: "fr", Attempts: 1, StopAfterFailures: -1}
	pass1, err := Document(context.Background(), first, opts, DocMeta{}, "c.xhtml", source, "", nil)
	if err != nil {
		t.Fatalf("premier passage : %v", err)
	}
	if len(pass1.Pending) != 2 {
		t.Fatalf("Pending = %v, want the two alpha paragraphs", pass1.Pending)
	}
	if !strings.Contains(string(pass1.Output), "<p>alpha one</p>") ||
		!strings.Contains(string(pass1.Output), "<p>BRAVO TWO</p>") {
		t.Fatalf("premier passage inattendu :\n%s", pass1.Output)
	}

	// Second pass: the model behaves, and only the pending ones are sent.
	second := &fakeProvider{answer: func(_ int, s []string) (string, error) {
		return envelope(upper(s)), nil
	}}
	pass2, err := RetryPending(context.Background(), second, opts, DocMeta{},
		"c.xhtml", source, pass1.Output, pass1.Pending, "", nil)
	if err != nil {
		t.Fatalf("reprise : %v", err)
	}

	// Only the two failed paragraphs were sent: the rest is already paid for.
	if total := totalSegments(second); total != 2 {
		t.Errorf("%d segments envoyés lors de la reprise, want 2", total)
	}

	got := string(pass2.Output)
	for _, want := range []string{"<p>ALPHA ONE</p>", "<p>ALPHA THREE</p>", "<p>BRAVO TWO</p>", "<p>CHARLIE FOUR</p>"} {
		if !strings.Contains(got, want) {
			t.Errorf("la reprise a perdu %q :\n%s", want, got)
		}
	}
	if len(pass2.Pending) != 0 {
		t.Errorf("Pending = %v après une reprise réussie, want vide", pass2.Pending)
	}
	if _, err := epub.Extract(pass2.Output); err != nil {
		t.Errorf("le document ne se lit plus : %v", err)
	}
}

func totalSegments(p *fakeProvider) int {
	n := 0
	for _, s := range p.sizes {
		n += s
	}
	return n
}

func TestRetryKeepsTheEarlierTranslationWhenItFailsAgain(t *testing.T) {
	source := []byte(`<html><body><p>alpha one</p><p>bravo two</p></body></html>`)
	first := &fakeProvider{answer: refuseContaining("alpha")}
	opts := Options{TargetLanguage: "fr", Attempts: 1, StopAfterFailures: -1}
	pass1, _ := Document(context.Background(), first, opts, DocMeta{}, "c.xhtml", source, "", nil)

	stillBad := &fakeProvider{answer: refuseContaining("alpha")}
	pass2, err := RetryPending(context.Background(), stillBad, opts, DocMeta{},
		"c.xhtml", source, pass1.Output, pass1.Pending, "", nil)
	if err != nil {
		t.Fatalf("reprise : %v", err)
	}
	if !strings.Contains(string(pass2.Output), "<p>BRAVO TWO</p>") {
		t.Error("une reprise infructueuse ne doit rien perdre du passage précédent")
	}
	if len(pass2.Pending) != 1 || pass2.Pending[0] != 0 {
		t.Errorf("Pending = %v, want [0] — l'indice doit rester celui du document entier", pass2.Pending)
	}
}

func TestRetryRefusesADocumentWhoseStructureChanged(t *testing.T) {
	source := []byte(`<html><body><p>un</p><p>deux</p></body></html>`)
	previous := []byte(`<html><body><p>un</p></body></html>`)
	_, err := RetryPending(context.Background(), &fakeProvider{}, Options{TargetLanguage: "fr"},
		DocMeta{}, "c.xhtml", source, previous, []int{0}, "", nil)
	if err == nil || !strings.Contains(err.Error(), "structure") {
		t.Fatalf("err = %v, want a refusal naming the structure mismatch", err)
	}
}

func TestRetryWithNothingPendingCostsNothing(t *testing.T) {
	source := []byte(`<html><body><p>un</p></body></html>`)
	p := &fakeProvider{answer: func(_ int, s []string) (string, error) { return envelope(upper(s)), nil }}
	res, err := RetryPending(context.Background(), p, Options{TargetLanguage: "fr"},
		DocMeta{}, "c.xhtml", source, source, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.calls != 0 {
		t.Errorf("%d appel(s) pour une reprise sans rien à reprendre", p.calls)
	}
	if string(res.Output) != string(source) {
		t.Error("le document doit être rendu tel quel")
	}
}

func TestRetryIgnoresImpossibleIndices(t *testing.T) {
	source := []byte(`<html><body><p>un</p><p>deux</p></body></html>`)
	p := &fakeProvider{answer: func(_ int, s []string) (string, error) { return envelope(upper(s)), nil }}
	res, err := RetryPending(context.Background(), p, Options{TargetLanguage: "fr", Attempts: 1},
		DocMeta{}, "c.xhtml", source, source, []int{-1, 0, 0, 99}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if totalSegments(p) != 1 {
		t.Errorf("%d segments envoyés, want 1 — les indices hors bornes et les doublons sont ignorés", totalSegments(p))
	}
	if !strings.Contains(string(res.Output), "<p>UN</p>") || !strings.Contains(string(res.Output), "<p>deux</p>") {
		t.Errorf("résultat inattendu :\n%s", res.Output)
	}
}

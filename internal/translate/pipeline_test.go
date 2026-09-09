package translate

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"

	"strings"
	"testing"

	"github.com/blkmlo/tulipe/internal/epub"
	"github.com/blkmlo/tulipe/internal/llm"
)

func testBook(t *testing.T) *epub.Book {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	add("mimetype", "application/epub+zip")
	add("META-INF/container.xml", `<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="c.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`)
	add("c.opf", `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Livre</dc:title><dc:language>en</dc:language></metadata><manifest><item id="a" href="a.xhtml" media-type="application/xhtml+xml"/><item id="b" href="b.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="a"/><itemref idref="b"/></spine></package>`)
	add("a.xhtml", `<?xml version="1.0" encoding="utf-8"?><html xmlns="http://www.w3.org/1999/xhtml"><body><h1>One</h1><p>first chapter</p></body></html>`)
	add("b.xhtml", `<?xml version="1.0" encoding="utf-8"?><html xmlns="http://www.w3.org/1999/xhtml"><body><h1>Two</h1><p>second chapter</p></body></html>`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	book, err := epub.Parse(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return book
}

func bookOpts() BookOptions {
	return BookOptions{Options: Options{
		TargetLanguage: "français", TargetCode: "fr", Attempts: 1, StopAfterFailures: -1,
	}}
}

// TestDocumentThatTranslatesNothingIsAFailure is the guard against the worst
// possible outcome: a book handed back untranslated with a tick next to every
// chapter.
func TestDocumentThatTranslatesNothingIsAFailure(t *testing.T) {
	p := &fakeProvider{answer: func(_ int, s []string) (string, error) {
		return envelope(make([]string, len(s))), nil // every translation empty
	}}
	book := testBook(t)
	before, _ := book.Read("a.xhtml")

	res, err := Book(context.Background(), p, book, bookOpts(), nil)
	if err != nil {
		t.Fatalf("Book: %v", err)
	}
	if len(res.Failed()) != len(res.Documents) {
		t.Fatalf("%d document(s) marked failed out of %d, want all of them",
			len(res.Failed()), len(res.Documents))
	}
	translated, total := res.Segments()
	if translated != 0 || total == 0 {
		t.Errorf("Segments() = %d/%d, want 0 out of a non-zero total", translated, total)
	}
	after, _ := book.Read("a.xhtml")
	if !bytes.Equal(before, after) {
		t.Error("a document that translated nothing must be left exactly as it was")
	}
}

func TestNothingTranslatedIsNeverCached(t *testing.T) {
	p := &fakeProvider{answer: func(_ int, s []string) (string, error) {
		return envelope(make([]string, len(s))), nil
	}}
	cache := &memCache{data: map[string][]byte{}}
	opts := bookOpts()
	opts.Cache = cache
	if _, err := Book(context.Background(), p, testBook(t), opts, nil); err != nil {
		t.Fatalf("Book: %v", err)
	}
	if len(cache.data) != 0 {
		t.Errorf("%d document(s) were cached; a failure must never be frozen into the cache", len(cache.data))
	}
}

func TestServiceUnusableStopsTheWholeRun(t *testing.T) {
	p := &fakeProvider{answer: func(_ int, _ []string) (string, error) {
		return "", &llm.APIError{Provider: "fake", Status: 401, Message: "clé invalide"}
	}}
	opts := bookOpts()
	opts.StopAfterFailures = 5
	res, err := Book(context.Background(), p, testBook(t), opts, nil)
	if !errors.Is(err, ErrServiceUnusable) {
		t.Fatalf("Book = %v, want ErrServiceUnusable", err)
	}
	if p.calls != 1 {
		t.Errorf("the backend was called %d times, want 1 — the run must stop at the first hopeless answer", p.calls)
	}
	if len(res.Documents) == 0 || res.Documents[0].Status != StatusFailed {
		t.Error("the document in flight must be marked failed")
	}
	for _, d := range res.Documents[1:] {
		if d.Status != StatusPending {
			t.Errorf("document %q has status %q; the rest of the book must not be attempted", d.Title, d.Status)
		}
	}
}

func TestPartialTranslationStillSucceeds(t *testing.T) {
	// One bad segment out of several is normal: the book is still translated,
	// and the note says what was left behind.
	p := &fakeProvider{answer: func(_ int, s []string) (string, error) {
		out := upper(s)
		out[0] = ""
		return envelope(out), nil
	}}
	res, err := Book(context.Background(), p, testBook(t), bookOpts(), nil)
	if err != nil {
		t.Fatalf("Book: %v", err)
	}
	if len(res.Failed()) != 0 {
		t.Errorf("documents failed: %v", res.Failed())
	}
	translated, total := res.Segments()
	if translated == 0 || translated == total {
		t.Errorf("Segments() = %d/%d, want a partial translation", translated, total)
	}
	if len(res.Notes) == 0 {
		t.Error("the skipped segment must be reported")
	}
}

func TestAttemptedIsAccumulatedAcrossDocuments(t *testing.T) {
	p := &fakeProvider{answer: func(_ int, s []string) (string, error) { return envelope(upper(s)), nil }}
	res, err := Book(context.Background(), p, testBook(t), bookOpts(), nil)
	if err != nil {
		t.Fatalf("Book: %v", err)
	}
	if res.Attempted != p.calls || res.Requests != p.calls {
		t.Errorf("Attempted=%d Requests=%d, want %d for both", res.Attempted, res.Requests, p.calls)
	}
}

func TestUnreadableDocumentDoesNotStopTheBook(t *testing.T) {
	book := testBook(t)
	book.Replace("a.xhtml", []byte(`<?xml version="1.0" encoding="ISO-8859-1"?><html><body><p>caf\xe9</p></body></html>`))
	p := &fakeProvider{answer: func(_ int, s []string) (string, error) { return envelope(upper(s)), nil }}

	res, err := Book(context.Background(), p, book, bookOpts(), nil)
	if err != nil {
		t.Fatalf("Book: %v", err)
	}
	if res.Documents[0].Status != StatusFailed {
		t.Errorf("the unreadable document has status %q, want failed", res.Documents[0].Status)
	}
	if !strings.Contains(errString(res.Documents[0].Err), "UTF-8") {
		t.Errorf("the failure should name the encoding problem: %v", res.Documents[0].Err)
	}
	if res.Documents[1].Status != StatusDone {
		t.Error("the following documents must still be translated")
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type memCache struct{ data map[string][]byte }

func (m *memCache) Get(k string) ([]byte, bool) { v, ok := m.data[k]; return v, ok }
func (m *memCache) Put(k string, v []byte) error {
	m.data[k] = v
	return nil
}

func TestRecipeKeyChangesWithEverySemanticSetting(t *testing.T) {
	base := Recipe{
		Provider: "anthropic", Model: "m", Effort: "medium",
		TargetLanguage: "français", TargetCode: "fr",
		SourceLanguage: "english", Glossary: "a = b", StyleNotes: "sobre",
	}
	seen := map[string]string{base.key("livre"): "base"}

	variants := map[string]Recipe{}
	for name, mutate := range map[string]func(*Recipe){
		"provider":    func(r *Recipe) { r.Provider = "openai-compatible" },
		"model":       func(r *Recipe) { r.Model = "autre" },
		"effort":      func(r *Recipe) { r.Effort = "max" },
		"langue":      func(r *Recipe) { r.TargetLanguage = "español" },
		"code":        func(r *Recipe) { r.TargetCode = "es" },
		"source":      func(r *Recipe) { r.SourceLanguage = "deutsch" },
		"code source": func(r *Recipe) { r.SourceCode = "de" },
		"glossary":    func(r *Recipe) { r.Glossary = "a = c" },
		"style":       func(r *Recipe) { r.StyleNotes = "familier" },
	} {
		r := base
		mutate(&r)
		variants[name] = r
	}
	for name, r := range variants {
		k := r.key("livre")
		if other, clash := seen[k]; clash {
			t.Errorf("changing %s produces the same cache key as %s: cached work would be reused wrongly", name, other)
		}
		seen[k] = name
	}
	// A different book must not share a key either.
	if base.key("livre") == base.key("autre-livre") {
		t.Error("two different books share a cache key")
	}
	// The same recipe must be stable across calls, or resume never works.
	if base.key("livre") != base.key("livre") {
		t.Error("the cache key is not stable")
	}
}

// memCacheWithPending is a cache that also remembers untranslated passages.
type memCacheWithPending struct {
	memCache
	pending map[string][]int
}

func newMemCache() *memCacheWithPending {
	return &memCacheWithPending{
		memCache: memCache{data: map[string][]byte{}},
		pending:  map[string][]int{},
	}
}

func (m *memCacheWithPending) GetPending(k string) ([]int, bool) {
	v, ok := m.pending[k]
	return v, ok
}

func (m *memCacheWithPending) PutPending(k string, idx []int) error {
	m.pending[k] = idx
	return nil
}

func TestBookRecordsAndRetriesPendingPassages(t *testing.T) {
	cache := newMemCache()
	opts := bookOpts()
	opts.Cache = cache

	// First run: everything containing "first" comes back empty.
	shy := &fakeProvider{answer: refuseContaining("first")}
	res1, err := Book(context.Background(), shy, testBook(t), opts, nil)
	if err != nil {
		t.Fatalf("premier passage : %v", err)
	}
	if res1.Pending() == 0 {
		t.Fatal("aucun passage signalé comme laissé en langue source")
	}
	if !res1.Retryable() {
		t.Error("Retryable() = false alors que des passages restent à reprendre")
	}

	// A plain re-run reuses the cache and asks nothing.
	quiet := &fakeProvider{answer: func(_ int, s []string) (string, error) { return envelope(upper(s)), nil }}
	res2, err := Book(context.Background(), quiet, testBook(t), opts, nil)
	if err != nil {
		t.Fatalf("second passage : %v", err)
	}
	if quiet.calls != 0 {
		t.Errorf("%d appel(s) sur une simple relance ; le cache doit suffire", quiet.calls)
	}
	if res2.Pending() != res1.Pending() {
		t.Errorf("Pending = %d, want %d — le compte doit survivre au cache", res2.Pending(), res1.Pending())
	}

	// With RetryPending, only the failed passages are sent again.
	willing := &fakeProvider{answer: func(_ int, s []string) (string, error) { return envelope(upper(s)), nil }}
	opts.RetryPending = true
	book := testBook(t)
	res3, err := Book(context.Background(), willing, book, opts, nil)
	if err != nil {
		t.Fatalf("reprise : %v", err)
	}
	if got := totalSegments(willing); got != res1.Pending() {
		t.Errorf("%d segments envoyés lors de la reprise, want %d", got, res1.Pending())
	}
	if res3.Pending() != 0 {
		t.Errorf("Pending = %d après une reprise réussie, want 0", res3.Pending())
	}
	if res3.Retryable() {
		t.Error("Retryable() = true alors que tout est traduit")
	}
	doc, _ := book.Read("a.xhtml")
	if strings.Contains(string(doc), "first chapter") {
		t.Errorf("le passage repris est resté en langue source :\n%s", doc)
	}
}

func TestRetryIsSkippedWhenTheCacheCannotRemember(t *testing.T) {
	// A cache without a pending store predates the feature. Retrying on a
	// guess would re-translate the whole book, so it must not happen.
	cache := &memCache{data: map[string][]byte{}}
	opts := bookOpts()
	opts.Cache = cache
	shy := &fakeProvider{answer: refuseContaining("first")}
	if _, err := Book(context.Background(), shy, testBook(t), opts, nil); err != nil {
		t.Fatal(err)
	}

	opts.RetryPending = true
	quiet := &fakeProvider{answer: func(_ int, s []string) (string, error) { return envelope(upper(s)), nil }}
	if _, err := Book(context.Background(), quiet, testBook(t), opts, nil); err != nil {
		t.Fatal(err)
	}
	if quiet.calls != 0 {
		t.Errorf("%d appel(s) ; sans mémoire des passages, la reprise doit s'abstenir", quiet.calls)
	}
}

package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/blkmlo/tulipe/internal/epub"
	"github.com/blkmlo/tulipe/internal/llm"
)

// fakeProvider answers with a scripted transformation of the segments it is
// given, so the pipeline can be tested without a network call.
type fakeProvider struct {
	calls  int
	sizes  []int
	answer func(call int, sources []string) (string, error)
}

func (f *fakeProvider) ID() string    { return "fake" }
func (f *fakeProvider) Model() string { return "fake-model" }

func (f *fakeProvider) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	f.calls++
	sources := extractPayload(req.User)
	f.sizes = append(f.sizes, len(sources))
	text, err := f.answer(f.calls, sources)
	if err != nil {
		return nil, err
	}
	return &llm.Response{Text: text, Model: "fake-model", Usage: llm.Usage{InputTokens: 10, OutputTokens: 5, Reported: true}}, nil
}

// extractPayload recovers the JSON array the prompt ends with.
func extractPayload(prompt string) []string {
	i := strings.LastIndex(prompt, "\n[")
	if i < 0 {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(prompt[i+1:]), &out); err != nil {
		return nil
	}
	return out
}

func envelope(items []string) string {
	data, _ := json.Marshal(translationEnvelope{Translations: items})
	return string(data)
}

func upper(sources []string) []string {
	out := make([]string, len(sources))
	for i, s := range sources {
		out[i] = strings.ToUpper(s)
	}
	return out
}

const doc = `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>t</title></head>` +
	`<body><h1>one</h1><p>two <em>three</em></p><p>four</p></body></html>`

func TestDocumentTranslatesEverySegment(t *testing.T) {
	p := &fakeProvider{answer: func(_ int, s []string) (string, error) { return envelope(upper(s)), nil }}
	res, err := Document(context.Background(), p, Options{TargetLanguage: "français"}, DocMeta{}, "c1.xhtml", []byte(doc), "", nil)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if res.Segments != 4 || res.Translated != 4 {
		t.Fatalf("translated %d of %d segments, want 4 of 4", res.Translated, res.Segments)
	}
	if len(res.Notes) != 0 {
		t.Errorf("unexpected notes: %v", res.Notes)
	}
	got := string(res.Output)
	if !strings.Contains(got, "<h1>ONE</h1>") || !strings.Contains(got, "TWO <EM>THREE</EM>") {
		t.Errorf("output = %s", got)
	}
	if !res.Usage.Reported || res.Usage.InputTokens == 0 {
		t.Error("usage was not accumulated")
	}
}

func TestChunkingRespectsLimits(t *testing.T) {
	var body strings.Builder
	body.WriteString(`<html><body>`)
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&body, "<p>%s</p>", strings.Repeat("x", 100)+" mot")
	}
	body.WriteString(`</body></html>`)

	p := &fakeProvider{answer: func(_ int, s []string) (string, error) { return envelope(upper(s)), nil }}
	opts := Options{TargetLanguage: "fr", ChunkChars: 250, MaxSegments: 40}
	if _, err := Document(context.Background(), p, opts, DocMeta{}, "c.xhtml", []byte(body.String()), "", nil); err != nil {
		t.Fatalf("Document: %v", err)
	}
	if p.calls < 4 {
		t.Errorf("made %d requests for 10 long paragraphs with a 250-char budget; want the work split further", p.calls)
	}
	for i, n := range p.sizes {
		if n > 3 {
			t.Errorf("request %d carried %d segments, more than the character budget allows", i, n)
		}
	}
}

func TestMaxSegmentsCapsRequestSize(t *testing.T) {
	var body strings.Builder
	body.WriteString(`<html><body>`)
	for i := 0; i < 12; i++ {
		body.WriteString("<p>mot</p>")
	}
	body.WriteString(`</body></html>`)

	p := &fakeProvider{answer: func(_ int, s []string) (string, error) { return envelope(upper(s)), nil }}
	opts := Options{TargetLanguage: "fr", ChunkChars: 100000, MaxSegments: 5}
	if _, err := Document(context.Background(), p, opts, DocMeta{}, "c.xhtml", []byte(body.String()), "", nil); err != nil {
		t.Fatalf("Document: %v", err)
	}
	for i, n := range p.sizes {
		if n > 5 {
			t.Errorf("request %d carried %d segments, want at most 5", i, n)
		}
	}
}

func TestWrongCountFallsBackToSmallerRanges(t *testing.T) {
	// The model answers with one item too few whenever it is given more than
	// one segment, forcing the range to be halved until it issingle-segment wide.
	p := &fakeProvider{answer: func(_ int, s []string) (string, error) {
		if len(s) > 1 {
			return envelope(upper(s)[:len(s)-1]), nil
		}
		return envelope(upper(s)), nil
	}}
	opts := Options{TargetLanguage: "fr", Attempts: 1}
	res, err := Document(context.Background(), p, opts, DocMeta{}, "c.xhtml", []byte(doc), "", nil)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if res.Translated != 4 {
		t.Errorf("translated %d segments, want all 4 after splitting", res.Translated)
	}
	if len(res.Notes) != 0 {
		t.Errorf("unexpected notes: %v", res.Notes)
	}
}

func TestUntranslatableSegmentKeepsSourceAndNotes(t *testing.T) {
	p := &fakeProvider{answer: func(_ int, _ []string) (string, error) {
		return "", fmt.Errorf("upstream is unhappy")
	}}
	opts := Options{TargetLanguage: "fr", Attempts: 1}
	res, err := Document(context.Background(), p, opts, DocMeta{}, "c.xhtml", []byte(doc), "", nil)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if res.Translated != 0 {
		t.Errorf("translated %d segments, want 0", res.Translated)
	}
	if len(res.Notes) != 4 {
		t.Errorf("got %d notes, want one per untranslated segment", len(res.Notes))
	}
	if string(res.Output) != doc {
		t.Error("a failed run must leave the document exactly as it was")
	}
}

func TestBrokenMarkupIsRejected(t *testing.T) {
	p := &fakeProvider{answer: func(_ int, s []string) (string, error) {
		out := make([]string, len(s))
		for i := range s {
			out[i] = "<em>unclosed"
		}
		return envelope(out), nil
	}}
	res, err := Document(context.Background(), p, Options{TargetLanguage: "fr", Attempts: 1}, DocMeta{}, "c.xhtml", []byte(doc), "", nil)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if strings.Contains(string(res.Output), "unclosed") {
		t.Error("malformed markup was spliced into the document")
	}
	if _, err := epub.Extract(res.Output); err != nil {
		t.Errorf("document no longer parses: %v", err)
	}
}

func TestMarkupChangeIsAcceptedButNoted(t *testing.T) {
	p := &fakeProvider{answer: func(_ int, s []string) (string, error) {
		out := make([]string, len(s))
		for i, src := range s {
			out[i] = strings.ReplaceAll(strings.ToUpper(src), "<EM>THREE</EM>", "THREE")
		}
		return envelope(out), nil
	}}
	res, err := Document(context.Background(), p, Options{TargetLanguage: "fr", Attempts: 1}, DocMeta{}, "c.xhtml", []byte(doc), "", nil)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if res.Translated != 4 {
		t.Errorf("translated %d segments, want 4", res.Translated)
	}
	var found bool
	for _, n := range res.Notes {
		if strings.Contains(n.Message, "balisage modifié") {
			found = true
		}
	}
	if !found {
		t.Errorf("la perte d\x27un <em> doit être signalée ; notes = %v", res.Notes)
	}
}

func TestParseTranslations(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`{"translations":["a","b"]}`, []string{"a", "b"}},
		{`["a","b"]`, []string{"a", "b"}},
		{"```json\n{\"translations\":[\"a\",\"b\"]}\n```", []string{"a", "b"}},
		{"Voici la traduction :\n{\"translations\":[\"a\",\"b\"]}\nVoilà.", []string{"a", "b"}},
	}
	for _, c := range cases {
		got, err := parseTranslations(c.in)
		if err != nil {
			t.Errorf("parseTranslations(%q) = %v", c.in, err)
			continue
		}
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("parseTranslations(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "pas du json", "{}"} {
		if _, err := parseTranslations(bad); err == nil {
			t.Errorf("parseTranslations(%q) succeeded, want an error", bad)
		}
	}
}

func TestContextTailIsPassedOn(t *testing.T) {
	var sawTail bool
	p := &fakeProvider{answer: func(call int, s []string) (string, error) {
		return envelope(upper(s)), nil
	}}
	opts := Options{TargetLanguage: "fr", ChunkChars: 10}
	res, err := Document(context.Background(), p, opts, DocMeta{}, "c.xhtml", []byte(doc), "", nil)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if res.Tail == "" {
		t.Fatal("no continuity tail was produced")
	}
	// A second document must receive the first one's tail in its prompt.
	p2 := &fakeProvider{answer: func(_ int, s []string) (string, error) { return envelope(upper(s)), nil }}
	inner := p2.answer
	p2.answer = func(call int, s []string) (string, error) {
		if call == 1 {
			sawTail = true
		}
		return inner(call, s)
	}
	if _, err := Document(context.Background(), p2, opts, DocMeta{}, "c2.xhtml", []byte(doc), res.Tail, nil); err != nil {
		t.Fatal(err)
	}
	if !sawTail {
		t.Error("the follow-up document was translated without continuity context")
	}
}

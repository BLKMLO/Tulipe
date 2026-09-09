package translate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/blkmlo/tulipe/internal/epub"
	"github.com/blkmlo/tulipe/internal/llm"
)

// fakeProvider answers with a scripted transformation of the segments it is
// given, so the pipeline can be tested without a network call.
type fakeProvider struct {
	calls int
	sizes []int
	// honourContext makes the fake respect the per-request deadline, the way a
	// real client does.
	honourContext bool
	answer        func(call int, sources []string) (string, error)
}

func (f *fakeProvider) ID() string    { return "fake" }
func (f *fakeProvider) Model() string { return "fake-model" }

func (f *fakeProvider) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	f.calls++
	sources := extractPayload(req.User)
	f.sizes = append(f.sizes, len(sources))

	type result struct {
		text string
		err  error
	}
	done := make(chan result, 1)
	call := f.calls
	go func() {
		text, err := f.answer(call, sources)
		done <- result{text, err}
	}()

	var text string
	var err error
	if f.honourContext {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case r := <-done:
			text, err = r.text, r.err
		}
	} else {
		r := <-done
		text, err = r.text, r.err
	}
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
	// The guard is disabled here: this test is about what a single bad segment
	// does, not about a dead backend.
	opts := Options{TargetLanguage: "fr", Attempts: 1, StopAfterFailures: -1}
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
		if strings.Contains(n.Message, "markup changed") {
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

func TestGuardStopsAfterRepeatedFailures(t *testing.T) {
	p := &fakeProvider{answer: func(_ int, _ []string) (string, error) {
		return "", fmt.Errorf("le service est en panne")
	}}
	opts := Options{TargetLanguage: "fr", Attempts: 1, StopAfterFailures: 3}
	_, err := Document(context.Background(), p, opts, DocMeta{}, "c.xhtml", []byte(doc), "", nil)
	if !errors.Is(err, ErrServiceUnusable) {
		t.Fatalf("Document = %v, want ErrServiceUnusable", err)
	}
	if p.calls != 3 {
		t.Errorf("the backend was called %d times, want exactly the 3 allowed by the guard", p.calls)
	}
}

func TestGuardStopsImmediatelyOnFatalError(t *testing.T) {
	// A wrong key never becomes a right key: one call is enough to know.
	p := &fakeProvider{answer: func(_ int, _ []string) (string, error) {
		return "", &llm.APIError{Provider: "fake", Status: 401, Message: "clé invalide"}
	}}
	opts := Options{TargetLanguage: "fr", Attempts: 4, StopAfterFailures: 20}
	_, err := Document(context.Background(), p, opts, DocMeta{}, "c.xhtml", []byte(doc), "", nil)
	if !errors.Is(err, ErrServiceUnusable) {
		t.Fatalf("Document = %v, want ErrServiceUnusable", err)
	}
	if p.calls != 1 {
		t.Errorf("the backend was called %d times, want 1 — a 401 must not be retried", p.calls)
	}
}

func TestGuardResetsAfterASuccess(t *testing.T) {
	// Intermittent failures must not add up to an abort as long as requests
	// keep succeeding in between.
	p := &fakeProvider{answer: func(call int, s []string) (string, error) {
		if call%2 == 1 {
			return "", fmt.Errorf("hoquet passager")
		}
		return envelope(upper(s)), nil
	}}
	opts := Options{TargetLanguage: "fr", Attempts: 2, RetryBase: time.Millisecond, StopAfterFailures: 3}
	res, err := Document(context.Background(), p, opts, DocMeta{}, "c.xhtml", []byte(doc), "", nil)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if res.Translated != 4 {
		t.Errorf("translated %d segments, want 4", res.Translated)
	}
}

func TestAttemptedCountsEveryCall(t *testing.T) {
	p := &fakeProvider{answer: func(call int, s []string) (string, error) {
		if call == 1 {
			return "", fmt.Errorf("raté")
		}
		return envelope(upper(s)), nil
	}}
	opts := Options{TargetLanguage: "fr", Attempts: 3, RetryBase: time.Millisecond}
	res, err := Document(context.Background(), p, opts, DocMeta{}, "c.xhtml", []byte(doc), "", nil)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if res.Attempted != 2 || res.Requests != 1 {
		t.Errorf("Attempted=%d Requests=%d, want 2 and 1", res.Attempted, res.Requests)
	}
}

// TestSplittingIsNotMistakenForABrokenService guards against a false positive:
// a model that always returns one item too few forces a long chain of splits,
// and each split looks like a failure. The guard must not abort a document the
// split was about to rescue.
func TestSplittingIsNotMistakenForABrokenService(t *testing.T) {
	var body strings.Builder
	body.WriteString(`<html><body>`)
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&body, "<p>paragraphe numero %d</p>", i)
	}
	body.WriteString(`</body></html>`)

	p := &fakeProvider{answer: func(_ int, s []string) (string, error) {
		if len(s) > 1 {
			return envelope(upper(s)[:len(s)-1]), nil
		}
		return envelope(upper(s)), nil
	}}
	opts := Options{TargetLanguage: "fr", Attempts: 1, MaxSegments: 40, ChunkChars: 100000, StopAfterFailures: 6}
	res, err := Document(context.Background(), p, opts, DocMeta{}, "c.xhtml", []byte(body.String()), "", nil)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if res.Translated != 40 {
		t.Errorf("translated %d of 40 segments; the split should have rescued them all", res.Translated)
	}
}

// TestBadAnswersDoNotWaitBetweenTries keeps the split path fast: backing off
// before re-sending a malformed answer wastes the user's time for nothing.
func TestBadAnswersDoNotWaitBetweenTries(t *testing.T) {
	p := &fakeProvider{answer: func(_ int, s []string) (string, error) {
		if len(s) > 1 {
			return envelope(upper(s)[:len(s)-1]), nil
		}
		return envelope(upper(s)), nil
	}}
	opts := Options{
		TargetLanguage: "fr", Attempts: 4,
		RetryBase:         30 * time.Second, // would dominate if it were applied
		StopAfterFailures: -1,
	}
	start := time.Now()
	if _, err := Document(context.Background(), p, opts, DocMeta{}, "c.xhtml", []byte(doc), "", nil); err != nil {
		t.Fatalf("Document: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("recovering from malformed answers took %s; no backoff should apply to them", elapsed)
	}
}

func TestRequestTimeoutIsRetryableAndNamed(t *testing.T) {
	p := &fakeProvider{answer: func(call int, s []string) (string, error) {
		if call == 1 {
			// Simulate a service that accepts the call and says nothing.
			time.Sleep(80 * time.Millisecond)
			return envelope(upper(s)), nil
		}
		return envelope(upper(s)), nil
	}}
	p.honourContext = true
	opts := Options{
		TargetLanguage: "fr", Attempts: 3, RetryBase: time.Millisecond,
		RequestTimeout: 20 * time.Millisecond, StopAfterFailures: -1,
	}
	res, err := Document(context.Background(), p, opts, DocMeta{}, "c.xhtml", []byte(doc), "", nil)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if res.Attempted < 2 {
		t.Errorf("Attempted = %d; a timed-out call must be retried, not abandoned", res.Attempted)
	}
}

// fakeDirect stands in for a translation service such as DeepL: it is handed
// the segments themselves, so there is no prompt and no JSON in the way.
type fakeDirect struct {
	calls    int
	requests []llm.SegmentRequest
	answer   func(call int, req llm.SegmentRequest) ([]string, error)
}

func (f *fakeDirect) ID() string    { return "fake-direct" }
func (f *fakeDirect) Model() string { return "fake-direct" }

func (f *fakeDirect) Complete(context.Context, llm.Request) (*llm.Response, error) {
	return nil, errors.New("ce service ne suit pas d'instructions")
}

func (f *fakeDirect) TranslateSegments(_ context.Context, req llm.SegmentRequest) (*llm.SegmentResponse, error) {
	f.calls++
	f.requests = append(f.requests, req)
	out, err := f.answer(f.calls, req)
	if err != nil {
		return nil, err
	}
	return &llm.SegmentResponse{Translations: out, Model: "fake-direct"}, nil
}

func TestDirectTranslatorPathSkipsPrompting(t *testing.T) {
	p := &fakeDirect{answer: func(_ int, req llm.SegmentRequest) ([]string, error) {
		return upper(req.Segments), nil
	}}
	opts := Options{TargetLanguage: "français", TargetCode: "fr", SourceCode: "en", Attempts: 1}
	res, err := Document(context.Background(), p, opts, DocMeta{}, "c.xhtml", []byte(doc), "", nil)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if res.Translated != 4 {
		t.Errorf("translated %d segments, want 4", res.Translated)
	}
	if p.calls == 0 {
		t.Fatal("the direct path was not used")
	}
	req := p.requests[0]
	if req.TargetCode != "fr" || req.SourceCode != "en" {
		t.Errorf("language codes = %q/%q, want fr/en", req.TargetCode, req.SourceCode)
	}
	if !req.Markup {
		t.Error("a batch holding block segments must be flagged as carrying markup")
	}
	if !strings.Contains(string(res.Output), "<EM>THREE</EM>") {
		t.Errorf("the translation was not spliced back:\n%s", res.Output)
	}
	// Usage stays unreported: this backend sends none.
	if res.Usage.Reported {
		t.Error("usage must not be invented for a backend that reports none")
	}
}

func TestDirectTranslatorCountMismatchStillSplits(t *testing.T) {
	p := &fakeDirect{answer: func(_ int, req llm.SegmentRequest) ([]string, error) {
		if len(req.Segments) > 1 {
			return upper(req.Segments)[:len(req.Segments)-1], nil
		}
		return upper(req.Segments), nil
	}}
	opts := Options{TargetLanguage: "fr", TargetCode: "fr", Attempts: 1, StopAfterFailures: 6}
	res, err := Document(context.Background(), p, opts, DocMeta{}, "c.xhtml", []byte(doc), "", nil)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if res.Translated != 4 {
		t.Errorf("translated %d segments, want the split to rescue all 4", res.Translated)
	}
}

func TestDirectTranslatorFailureIsGuarded(t *testing.T) {
	p := &fakeDirect{answer: func(_ int, _ llm.SegmentRequest) ([]string, error) {
		return nil, &llm.APIError{Provider: "fake-direct", Status: 403, Message: "quota épuisé"}
	}}
	opts := Options{TargetLanguage: "fr", TargetCode: "fr", Attempts: 3, StopAfterFailures: 6}
	_, err := Document(context.Background(), p, opts, DocMeta{}, "c.xhtml", []byte(doc), "", nil)
	if !errors.Is(err, ErrServiceUnusable) {
		t.Fatalf("Document = %v, want ErrServiceUnusable", err)
	}
	if p.calls != 1 {
		t.Errorf("the backend was called %d times; an exhausted quota must stop at once", p.calls)
	}
}

func TestDirectTranslatorFlagsPlainTextBatches(t *testing.T) {
	// A document whose only translatable spans are bare text must not ask for
	// markup handling.
	plain := `<html><head><title>titre</title></head><body><div>du texte</div></body></html>`
	p := &fakeDirect{answer: func(_ int, req llm.SegmentRequest) ([]string, error) {
		return upper(req.Segments), nil
	}}
	opts := Options{TargetLanguage: "fr", TargetCode: "fr", Attempts: 1}
	if _, err := Document(context.Background(), p, opts, DocMeta{}, "c.xhtml", []byte(plain), "", nil); err != nil {
		t.Fatal(err)
	}
	for i, req := range p.requests {
		if req.Markup {
			t.Errorf("request %d asked for markup handling on plain text", i)
		}
	}
}

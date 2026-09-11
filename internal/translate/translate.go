// Package translate turns the prose of an EPUB into another language, one
// document and one bounded chunk at a time.
//
// The unit of work is deliberately small. A chapter is split into chunks of a
// few thousand characters, and each chunk is a single, self-contained request:
// the context window never has to hold the book, or even a whole chapter.
package translate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blkmlo/tulipe/internal/epub"
	"github.com/blkmlo/tulipe/internal/i18n"
	"github.com/blkmlo/tulipe/internal/llm"
)

// Options drives a translation run.
type Options struct {
	// TargetLanguage is the language to translate into, written the way a
	// human would name it ("français", "brazilian portuguese").
	TargetLanguage string
	// TargetCode is the BCP 47 tag written into the book metadata ("fr").
	// Empty leaves the original metadata untouched.
	TargetCode string
	// SourceLanguage is optional; empty lets the model detect it.
	SourceLanguage string
	// SourceCode is the BCP 47 tag of the source language, used by backends
	// that take codes rather than prose. Empty asks for detection.
	SourceCode string
	// Glossary pins terminology, one "source = target" rule per line.
	Glossary string
	// StyleNotes are free-form instructions appended to the system prompt.
	StyleNotes string
	// KeepOriginalTitles leaves headings and the table of contents in the
	// source language. Some readers want the titles untouched so the book
	// still matches its reviews and its index.
	//
	// The field is worded the negative way round on purpose: the zero value
	// has to mean "translate everything", which is both the older behaviour
	// and the one a caller that forgot the setting should get. config.Config
	// carries the positive TranslateTitles, which is what the reader is shown.
	KeepOriginalTitles bool
	// About describes the book in one sentence — its genre, period, subject.
	// It settles register and the sense of ambiguous words. Empty disables it
	// entirely: no sentence is added to the prompt.
	About string

	// ChunkChars caps the source characters sent in one request.
	ChunkChars int
	// MaxSegments caps how many segments travel in one request.
	MaxSegments int
	// MaxTokens caps the model's answer.
	MaxTokens int64
	// Attempts is how many times a failing request is retried.
	Attempts int
	// RetryBase is the first backoff delay; it doubles on each retry.
	RetryBase time.Duration
	// RequestTimeout caps one call to the backend. Without it, a service that
	// accepts a connection and then says nothing would hold the run for as
	// long as the HTTP client allows. Zero uses the default; negative removes
	// the cap.
	RequestTimeout time.Duration
	// ContextChars is how much of the previous translation is shown to the
	// model for continuity of tone and terminology.
	//
	// Zero means "not set", and Defaults fills in the usual amount. A negative
	// value means "none at all", which is a real answer and has to be
	// distinguishable from an unset field — config.Config carries the reader's
	// choice, where zero does mean none, and converts.
	ContextChars int
	// StopAfterFailures aborts the run once this many requests in a row have
	// failed without a single success, so a dead or misconfigured service
	// cannot grind through a whole book. Zero uses the default; a negative
	// value disables the guard.
	StopAfterFailures int

	// Salvage marks the second pass at passages the first one could not
	// translate. It changes the prompt rather than the rules: the model is
	// told the batch already came back unusable once, and the checks in
	// accept are exactly the same. Callers set it through salvageOptions.
	Salvage bool

	// failures is shared by every document of one book, so the guard counts a
	// broken service once rather than once per chapter. Book sets it; Document
	// allocates its own when it is nil.
	failures *failureCounter
}

// messageError is a sentinel whose wording is read when it is printed rather
// than when the package is initialised, so it follows the interface language.
type messageError struct{ key string }

func (e messageError) Error() string { return i18n.T(e.key) }

// ErrServiceUnusable means the translation backend failed often enough that
// continuing would be pointless. The run stops instead of burning through the
// rest of the book.
var ErrServiceUnusable error = messageError{"translate.err.service-unusable"}

type failureCounter struct {
	consecutive int
	limit       int
}

func (f *failureCounter) success() { f.consecutive = 0 }

// trip records a failure and reports whether the guard has been reached.
func (f *failureCounter) trip() bool {
	f.consecutive++
	return f.limit > 0 && f.consecutive >= f.limit
}

// defaultTargetLanguage is what an Options with no target at all falls back
// to. It is the language of the default interface, not a preference of the
// pipeline: callers that mean something else always say so.
const defaultTargetLanguage = "english"

// Defaults fills in every unset field with a usable value.
func (o Options) Defaults() Options {
	if o.TargetLanguage == "" {
		o.TargetLanguage = defaultTargetLanguage
	}
	if o.ChunkChars <= 0 {
		o.ChunkChars = 4000
	}
	if o.MaxSegments <= 0 {
		o.MaxSegments = 40
	}
	if o.MaxTokens <= 0 {
		o.MaxTokens = 16000
	}
	if o.Attempts <= 0 {
		o.Attempts = 4
	}
	if o.RetryBase <= 0 {
		o.RetryBase = 2 * time.Second
	}
	if o.ContextChars == 0 {
		// Zero is "not set". A negative value is "none at all" and is left
		// alone: Defaults is applied by Book and again by Document, so any
		// repair here happens twice. Turning none into zero and zero into the
		// default put four hundred characters back on the second pass.
		o.ContextChars = 400
	}
	if o.RequestTimeout == 0 {
		o.RequestTimeout = 5 * time.Minute
	}
	if o.StopAfterFailures == 0 {
		o.StopAfterFailures = 6
	}
	if o.failures == nil {
		o.failures = &failureCounter{limit: o.StopAfterFailures}
	}
	return o
}

// DocMeta is what the model is told about the passage it is translating.
type DocMeta struct {
	BookTitle string
	Title     string
	Index     int // 1-based position in the book
	Total     int
	// Navigation marks the table of contents (nav or NCX). It holds nothing
	// but titles, so the "translate titles" setting governs it entirely.
	Navigation bool
}

// Note is a problem worth showing the user. Nothing is ever silently dropped:
// a segment that could not be translated keeps its source text and produces a
// note saying so.
type Note struct {
	Document string
	// Segment is the paragraph the note is about, or -1 when it concerns the
	// document as a whole. Zero is a real segment number, so the two cases
	// cannot share it: docNote builds the second kind.
	Segment int
	Message string
}

// docNote is a note about a whole document rather than one of its paragraphs.
func docNote(document, message string) Note {
	return Note{Document: document, Segment: -1, Message: message}
}

func (n Note) String() string {
	if n.Segment < 0 {
		return i18n.T("translate.note.format-doc", n.Document, n.Message)
	}
	return i18n.T("translate.note.format", n.Document, n.Segment, n.Message)
}

// DocResult is the outcome of translating one document.
type DocResult struct {
	Output []byte
	Usage  llm.Usage
	// Attempted counts every call made to the backend, Requests only the ones
	// that came back usable. The gap is what tells a failing run from a run
	// that never had to ask.
	Attempted  int
	Requests   int
	Segments   int
	Translated int
	// Pending lists the segments left in the source language, by index. A
	// later pass can retry exactly those instead of paying for the chapter
	// again.
	Pending []int
	Notes   []Note
	Tail    string // end of the translation, for continuity with the next document
}

// Event reports progress inside a document.
type Event struct {
	Document      string
	DoneSegments  int
	TotalSegments int
	Message       string
	Retry         *llm.RetryNotice
	// DocUsage and DocRequests are what this document has consumed so far, so
	// a display can show spend accumulating rather than jumping once per
	// chapter.
	DocUsage     llm.Usage
	DocRequests  int
	DocAttempted int
}

// Document translates one XHTML (or NCX) document and returns the rewritten
// bytes. onEvent may be nil.
func Document(ctx context.Context, p llm.Provider, opts Options, meta DocMeta, name string, doc []byte, tail string, onEvent func(Event)) (*DocResult, error) {
	opts = opts.Defaults()
	segs, err := epub.Extract(doc)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	subset, numbers := translatable(segs, !opts.KeepOriginalTitles, meta.Navigation)
	res := &DocResult{Segments: len(subset), Tail: tail}
	if len(subset) == 0 {
		res.Output = doc
		return res, nil
	}

	t := &translator{
		ctx: ctx, provider: p, opts: opts, meta: meta,
		name: name, segs: subset, numbers: numbers, res: res,
		onEvent: onEvent, tail: tail,
	}
	translations := t.run()
	if t.abortErr != nil {
		return res, t.abortErr
	}

	// Segments left out keep an empty translation, which epub.Apply reads as
	// "leave this span alone".
	full := make([]string, len(segs))
	for k, idx := range numbers {
		full[idx] = translations[k]
	}

	out, err := epub.Apply(doc, segs, full)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	res.Output = out
	res.Tail = t.tail
	return res, ctx.Err()
}

// translatable narrows a document to the segments the settings say to send.
// The indices of the whole document travel alongside, so notes and the pending
// list still name the right paragraph.
//
// Nothing is dropped from the document itself: a segment left out simply keeps
// its source text, which is also what a failed translation does.
func translatable(segs []epub.Segment, titles, navigation bool) (subset []epub.Segment, numbers []int) {
	if titles {
		numbers = make([]int, len(segs))
		for i := range segs {
			numbers[i] = i
		}
		return segs, numbers
	}
	for i, s := range segs {
		if s.Title || navigation {
			continue
		}
		subset = append(subset, s)
		numbers = append(numbers, i)
	}
	return subset, numbers
}

type translator struct {
	ctx      context.Context
	provider llm.Provider
	opts     Options
	meta     DocMeta
	name     string
	segs     []epub.Segment
	res      *DocResult
	onEvent  func(Event)

	tail  string // running end of the translation, for continuity
	done  int
	parts int
	part  int
	// numbers maps each entry of segs to its index in the whole document. It
	// is nil for a full pass, where the two coincide.
	numbers  []int
	abortErr error // set when the guard trips; stops the whole document
}

func (t *translator) emit(msg string, retry *llm.RetryNotice) {
	if t.onEvent == nil {
		return
	}
	t.onEvent(Event{
		Document:      t.name,
		DoneSegments:  t.done,
		TotalSegments: len(t.segs),
		Message:       msg,
		Retry:         retry,
		DocUsage:      t.res.Usage,
		DocRequests:   t.res.Requests,
		DocAttempted:  t.res.Attempted,
	})
}

// note records a message already written in the interface language: callers
// pass i18n.T(key, ...), not a format string, so that nothing here has to know
// which language the sentence came out in.
func (t *translator) note(idx int, msg string) {
	if t.opts.Salvage {
		// The first pass already reported this passage. Repeating the same
		// sentence would read as a second, different problem: the report
		// would count six where three paragraphs are stuck, and contradict
		// the pending line right above it.
		msg = i18n.T("translate.note.second-attempt", msg)
	}
	t.res.Notes = append(t.res.Notes, Note{
		Document: t.name,
		Segment:  t.segmentNumber(idx),
		Message:  msg,
	})
}

// keep records a segment that stayed in the source language: it is both worth
// telling the user about and worth offering to retry later.
func (t *translator) keep(idx int, msg string) {
	t.note(idx, msg)
	t.res.Pending = append(t.res.Pending, t.segmentNumber(idx))
}

// segmentNumber maps an index in t.segs back to its position in the whole
// document. They differ during a retry pass, which works on a subset.
func (t *translator) segmentNumber(idx int) int {
	if t.numbers == nil {
		return idx
	}
	if idx < 0 || idx >= len(t.numbers) {
		return idx
	}
	return t.numbers[idx]
}

// run translates every segment, returning one string per segment. An empty
// string means "keep the source", which epub.Apply honours.
func (t *translator) run() []string {
	out := make([]string, len(t.segs))
	chunks := t.chunk()
	t.parts = len(chunks)
	for _, c := range chunks {
		if t.ctx.Err() != nil || t.abortErr != nil {
			return out
		}
		t.part++
		t.translateRange(c[0], c[1], out)
	}
	return out
}

// chunk groups consecutive segments into requests bounded by ChunkChars and
// MaxSegments. Each returned pair is a half-open [start, end) range.
func (t *translator) chunk() [][2]int {
	var chunks [][2]int
	start, size := 0, 0
	for i, s := range t.segs {
		n := len(s.Source)
		if i > start && (size+n > t.opts.ChunkChars || i-start >= t.opts.MaxSegments) {
			chunks = append(chunks, [2]int{start, i})
			start, size = i, 0
		}
		size += n
	}
	if start < len(t.segs) {
		chunks = append(chunks, [2]int{start, len(t.segs)})
	}
	return chunks
}

// translateRange translates segs[lo:lo+n]. When the model returns something
// unusable, the range is halved and retried, down to a single segment; a
// segment that still fails keeps its source text and produces a note.
func (t *translator) translateRange(lo, hi int, out []string) {
	if t.ctx.Err() != nil || t.abortErr != nil || lo >= hi {
		return
	}
	got, err := t.request(lo, hi)
	if err == nil {
		t.accept(lo, hi, got, out)
		return
	}
	if t.ctx.Err() != nil || t.abortErr != nil {
		return
	}
	if hi-lo == 1 {
		t.keep(lo, i18n.T("translate.note.kept-source", err))
		t.done++
		t.emit("", nil)
		return
	}
	mid := lo + (hi-lo)/2
	t.emit(i18n.T("translate.msg.batch-split", hi-lo, err), nil)
	t.translateRange(lo, mid, out)
	t.translateRange(mid, hi, out)
}

// accept validates each translation before keeping it.
func (t *translator) accept(lo, hi int, got []string, out []string) {
	var plain strings.Builder
	for i := lo; i < hi; i++ {
		seg := t.segs[i]
		tr := got[i-lo]
		if seg.Kind == epub.KindBlock {
			// A lone "&" is the one markup slip with an unambiguous fix.
			// Mending it saves a paragraph that would otherwise be dropped
			// whole for a "Marks & Spencer".
			tr = epub.RepairAmpersands(tr)
		}

		switch {
		case strings.TrimSpace(tr) == "":
			t.keep(i, i18n.T("translate.note.empty-translation"))
		case !epub.SafeForXML(tr):
			// Invalid UTF-8, or control characters: the book produced would
			// not open in any reader.
			t.keep(i, i18n.T("translate.note.unsafe-characters"))
		case seg.Kind == epub.KindBlock && epub.WellFormed(seg.Source) == nil && epub.WellFormed(tr) != nil:
			t.keep(i, i18n.T("translate.note.broken-markup"))
		case seg.Kind == epub.KindText && !strings.ContainsAny(seg.Source, "<>") && strings.ContainsAny(tr, "<>"):
			// A plain-text span gets escaped on the way back in, so markup the
			// model invented would show up as literal angle brackets to the
			// reader. Keep the source instead.
			t.keep(i, i18n.T("translate.note.markup-in-text"))
		case len(tr) > 6*len(seg.Source)+200:
			t.keep(i, i18n.T("translate.note.too-long", len(tr)/max(1, len(seg.Source))))
		default:
			out[i] = tr
			t.res.Translated++
			if seg.Kind == epub.KindBlock {
				if a, b := epub.Tags(seg.Source), epub.Tags(tr); !sameTags(a, b) {
					t.note(i, i18n.T("translate.note.markup-changed", a, b))
				}
			}
			plain.WriteString(stripTags(tr))
			plain.WriteByte(' ')
		}
		t.done++
	}
	if s := strings.TrimSpace(plain.String()); s != "" && t.opts.ContextChars > 0 {
		t.tail = lastRunes(s, t.opts.ContextChars)
	}
	t.emit("", nil)
}

// answerAttempts is how many times one batch is re-sent when the model answers
// with something unusable. A malformed answer is not a passing outage: waiting
// does not help, and halving the batch usually does. So the model gets one
// immediate second chance, then the caller splits.
const answerAttempts = 2

// request sends one chunk and returns exactly hi-lo translations.
func (t *translator) request(lo, hi int) ([]string, error) {
	sources := make([]string, 0, hi-lo)
	for _, s := range t.segs[lo:hi] {
		sources = append(sources, s.Source)
	}
	splittable := hi-lo > 1

	if direct, ok := t.provider.(llm.DirectTranslator); ok {
		return t.requestDirect(direct, lo, hi, sources, splittable)
	}
	return t.requestPrompt(sources, splittable)
}

// requestDirect uses a backend that translates segments natively. There is no
// prompt to ignore and no JSON to mangle, so the only answer-level failure left
// is a wrong number of translations.
func (t *translator) requestDirect(direct llm.DirectTranslator, lo, hi int, sources []string, splittable bool) ([]string, error) {
	markup := false
	for _, s := range t.segs[lo:hi] {
		if s.Kind == epub.KindBlock {
			markup = true
			break
		}
	}
	req := llm.SegmentRequest{
		Segments:   sources,
		TargetCode: t.opts.TargetCode,
		SourceCode: t.opts.SourceCode,
		Markup:     markup,
	}

	var out []string
	var answerErr error
	err := llm.Retry(t.ctx, t.opts.Attempts, t.opts.RetryBase, func(n llm.RetryNotice) {
		t.emit("", &n)
	}, func() error {
		t.res.Attempted++
		ctx, cancel := t.requestContext()
		resp, err := direct.TranslateSegments(ctx, req)
		cancel()
		if err != nil {
			return t.timeoutError(err)
		}
		t.res.Usage.Add(resp.Usage)
		if len(resp.Translations) != len(sources) {
			// The service answered, so this is not an outage; record it and
			// stop retrying, the caller will split.
			answerErr = fmt.Errorf(i18n.T("translate.err.count"),
				len(resp.Translations), len(sources))
			return nil
		}
		out = resp.Translations
		return nil
	})
	if err != nil {
		return nil, t.guard(err)
	}
	if answerErr != nil {
		if splittable {
			return nil, answerErr
		}
		return nil, t.guard(answerErr)
	}
	t.res.Requests++
	t.opts.failures.success()
	t.emit("", nil)
	return out, nil
}

// requestPrompt instructs a chat model and reads the JSON it answers with.
func (t *translator) requestPrompt(sources []string, splittable bool) ([]string, error) {
	payload, err := encodeSegments(sources)
	if err != nil {
		return nil, err
	}

	req := llm.Request{
		System:    t.systemPrompt(),
		User:      t.userPrompt(payload, len(sources)),
		MaxTokens: t.opts.MaxTokens,
		Schema:    responseSchema,
	}

	var lastErr error
	for attempt := 1; attempt <= answerAttempts; attempt++ {
		resp, err := t.call(req)
		if err != nil {
			// Transport and API failures are the backend's, not the answer's:
			// they always count towards the guard.
			return nil, t.guard(err)
		}
		parsed, err := parseTranslations(resp.Text)
		if err == nil && len(parsed) != len(sources) {
			err = fmt.Errorf(i18n.T("translate.err.count"), len(parsed), len(sources))
		}
		if err == nil {
			t.res.Requests++
			t.opts.failures.success()
			t.emit("", nil)
			return parsed, nil
		}
		lastErr = err
		if attempt < answerAttempts {
			t.emit(i18n.T("translate.msg.unusable-answer", err), nil)
		}
	}
	if splittable {
		// The batch can still be halved, and halving is what usually rescues a
		// malformed answer. Counting these towards the guard would abort a
		// document the split was about to save.
		return nil, lastErr
	}
	return nil, t.guard(lastErr)
}

// call runs one request, retrying transport failures, rate limits and server
// errors with a growing wait.
func (t *translator) call(req llm.Request) (*llm.Response, error) {
	var resp *llm.Response
	err := llm.Retry(t.ctx, t.opts.Attempts, t.opts.RetryBase, func(n llm.RetryNotice) {
		t.emit("", &n)
	}, func() error {
		t.res.Attempted++
		ctx, cancel := t.requestContext()
		r, err := t.provider.Complete(ctx, req)
		cancel()
		if err != nil {
			return t.timeoutError(err)
		}
		// The tokens are spent whether or not the answer turns out usable.
		t.res.Usage.Add(r.Usage)
		resp = r
		return nil
	})
	return resp, err
}

// requestContext bounds one call to the backend.
func (t *translator) requestContext() (context.Context, context.CancelFunc) {
	if t.opts.RequestTimeout <= 0 {
		return t.ctx, func() {}
	}
	return context.WithTimeout(t.ctx, t.opts.RequestTimeout)
}

// timeoutError turns the per-request deadline into a plain, retryable error.
// Handing back the context error unchanged would look like a cancellation, and
// cancellations are deliberately never retried.
func (t *translator) timeoutError(err error) error {
	if t.ctx.Err() != nil || !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf(i18n.T("translate.err.no-answer-in"), t.opts.RequestTimeout)
}

// guard turns a request failure into an abort when the backend looks hopeless,
// so that a wrong key or a dead endpoint costs a handful of calls instead of
// one per paragraph of the book.
func (t *translator) guard(err error) error {
	switch {
	case t.ctx.Err() != nil:
		return err
	case llm.Fatal(err):
		t.abortErr = fmt.Errorf("%w : %w", ErrServiceUnusable, err)
	case t.opts.failures.trip():
		t.abortErr = fmt.Errorf(i18n.T("translate.err.consecutive"),
			ErrServiceUnusable, t.opts.failures.consecutive, err)
	default:
		return err
	}
	return t.abortErr
}

var responseSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"translations": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "string"},
		},
	},
	"required":             []string{"translations"},
	"additionalProperties": false,
}

func sameTags(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func stripTags(s string) string {
	var out strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '<':
			depth++
		case r == '>':
			if depth > 0 {
				depth--
			}
		case depth == 0:
			out.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

func lastRunes(s string, n int) string {
	if n <= 0 {
		// Not "the last nothing of it", which an ellipsis on its own would
		// claim: no continuity at all.
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n:])
}

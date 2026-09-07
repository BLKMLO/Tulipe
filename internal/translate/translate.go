// Package translate turns the prose of an EPUB into another language, one
// document and one bounded chunk at a time.
//
// The unit of work is deliberately small. A chapter is split into chunks of a
// few thousand characters, and each chunk is a single, self-contained request:
// the context window never has to hold the book, or even a whole chapter.
package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/blkmlo/tulipe/internal/epub"
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
	// Glossary pins terminology, one "source = target" rule per line.
	Glossary string
	// StyleNotes are free-form instructions appended to the system prompt.
	StyleNotes string

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
	// ContextChars is how much of the previous translation is shown to the
	// model for continuity of tone and terminology.
	ContextChars int
}

// Defaults fills in every unset field with a usable value.
func (o Options) Defaults() Options {
	if o.TargetLanguage == "" {
		o.TargetLanguage = "français"
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
	if o.ContextChars < 0 {
		o.ContextChars = 0
	}
	if o.ContextChars == 0 {
		o.ContextChars = 400
	}
	return o
}

// DocMeta is what the model is told about the passage it is translating.
type DocMeta struct {
	BookTitle string
	Title     string
	Index     int // 1-based position in the book
	Total     int
}

// Note is a problem worth showing the user. Nothing is ever silently dropped:
// a segment that could not be translated keeps its source text and produces a
// note saying so.
type Note struct {
	Document string
	Segment  int
	Message  string
}

func (n Note) String() string {
	return fmt.Sprintf("%s [segment %d] %s", n.Document, n.Segment, n.Message)
}

// DocResult is the outcome of translating one document.
type DocResult struct {
	Output     []byte
	Usage      llm.Usage
	Requests   int
	Segments   int
	Translated int
	Notes      []Note
	Tail       string // end of the translation, for continuity with the next document
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
	DocUsage    llm.Usage
	DocRequests int
}

// Document translates one XHTML (or NCX) document and returns the rewritten
// bytes. onEvent may be nil.
func Document(ctx context.Context, p llm.Provider, opts Options, meta DocMeta, name string, doc []byte, tail string, onEvent func(Event)) (*DocResult, error) {
	opts = opts.Defaults()
	segs, err := epub.Extract(doc)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	res := &DocResult{Segments: len(segs), Tail: tail}
	if len(segs) == 0 {
		res.Output = doc
		return res, nil
	}

	t := &translator{
		ctx: ctx, provider: p, opts: opts, meta: meta,
		name: name, segs: segs, res: res, onEvent: onEvent,
		tail: tail,
	}
	translations := t.run()

	out, err := epub.Apply(doc, segs, translations)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	res.Output = out
	res.Tail = t.tail
	return res, ctx.Err()
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
	})
}

func (t *translator) note(idx int, format string, args ...any) {
	t.res.Notes = append(t.res.Notes, Note{
		Document: t.name,
		Segment:  idx,
		Message:  fmt.Sprintf(format, args...),
	})
}

// run translates every segment, returning one string per segment. An empty
// string means "keep the source", which epub.Apply honours.
func (t *translator) run() []string {
	out := make([]string, len(t.segs))
	chunks := t.chunk()
	t.parts = len(chunks)
	for _, c := range chunks {
		if t.ctx.Err() != nil {
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
	if t.ctx.Err() != nil || lo >= hi {
		return
	}
	got, err := t.request(lo, hi)
	if err == nil {
		t.accept(lo, hi, got, out)
		return
	}
	if t.ctx.Err() != nil {
		return
	}
	if hi-lo == 1 {
		t.note(lo, "laissé en langue source : %v", err)
		t.done++
		t.emit("", nil)
		return
	}
	mid := lo + (hi-lo)/2
	t.emit(fmt.Sprintf("lot de %d segments redécoupé après : %v", hi-lo, err), nil)
	t.translateRange(lo, mid, out)
	t.translateRange(mid, hi, out)
}

// accept validates each translation before keeping it.
func (t *translator) accept(lo, hi int, got []string, out []string) {
	var plain strings.Builder
	for i := lo; i < hi; i++ {
		seg := t.segs[i]
		tr := got[i-lo]

		switch {
		case strings.TrimSpace(tr) == "":
			t.note(i, "traduction vide renvoyée par le modèle ; source conservée")
		case seg.Kind == epub.KindBlock && epub.WellFormed(seg.Source) == nil && epub.WellFormed(tr) != nil:
			t.note(i, "balisage mal formé dans la traduction ; source conservée")
		case seg.Kind == epub.KindText && !strings.ContainsAny(seg.Source, "<>") && strings.ContainsAny(tr, "<>"):
			// A plain-text span gets escaped on the way back in, so markup the
			// model invented would show up as literal angle brackets to the
			// reader. Keep the source instead.
			t.note(i, "du balisage a été introduit dans du texte brut ; source conservée")
		case len(tr) > 6*len(seg.Source)+200:
			t.note(i, "traduction %d fois plus longue que la source ; source conservée", len(tr)/max(1, len(seg.Source)))
		default:
			out[i] = tr
			t.res.Translated++
			if seg.Kind == epub.KindBlock {
				if a, b := epub.Tags(seg.Source), epub.Tags(tr); !sameTags(a, b) {
					t.note(i, "balisage modifié : source %v, traduction %v", a, b)
				}
			}
			plain.WriteString(stripTags(tr))
			plain.WriteByte(' ')
		}
		t.done++
	}
	if s := strings.TrimSpace(plain.String()); s != "" {
		t.tail = lastRunes(s, t.opts.ContextChars)
	}
	t.emit("", nil)
}

// request sends one chunk and returns exactly hi-lo translations.
func (t *translator) request(lo, hi int) ([]string, error) {
	sources := make([]string, 0, hi-lo)
	for _, s := range t.segs[lo:hi] {
		sources = append(sources, s.Source)
	}
	payload, err := json.Marshal(sources)
	if err != nil {
		return nil, err
	}

	req := llm.Request{
		System:    t.systemPrompt(),
		User:      t.userPrompt(string(payload), len(sources)),
		MaxTokens: t.opts.MaxTokens,
		Schema:    responseSchema,
	}

	var out []string
	err = llm.Retry(t.ctx, t.opts.Attempts, t.opts.RetryBase, func(n llm.RetryNotice) {
		t.emit("", &n)
	}, func() error {
		resp, err := t.provider.Complete(t.ctx, req)
		if err != nil {
			return err
		}
		t.res.Requests++
		t.res.Usage.Add(resp.Usage)
		t.emit("", nil)
		parsed, err := parseTranslations(resp.Text)
		if err != nil {
			return err
		}
		if len(parsed) != len(sources) {
			return fmt.Errorf("%d traductions renvoyées pour %d segments", len(parsed), len(sources))
		}
		out = parsed
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
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
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n:])
}

package translate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/blkmlo/tulipe/internal/epub"
	"github.com/blkmlo/tulipe/internal/llm"
)

// Status is where one document stands in a run.
type Status string

const (
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusCached  Status = "cached"
	StatusFailed  Status = "failed"
)

// DocState is the live state of one document of the book.
type DocState struct {
	Name          string
	Title         string
	Status        Status
	TotalSegments int
	DoneSegments  int
	// Translated is how many segments actually came back in the target
	// language. A document where this stays at zero has not been translated,
	// whatever else happened.
	Translated int
	Notes      int
	Err        error
	Duration   time.Duration
}

// Progress is emitted whenever anything moves. Documents holds the state of
// every document, so a display can simply redraw from it.
type Progress struct {
	Documents []DocState
	Current   int
	Message   string
	Retry     *llm.RetryNotice
	Usage     llm.Usage
	Requests  int
	Attempted int
}

// Result is the outcome of translating a whole book.
type Result struct {
	Book      *epub.Book
	Documents []DocState
	Usage     llm.Usage
	Requests  int
	Attempted int
	Notes     []Note
	Duration  time.Duration
}

// Segments totals the translatable segments of the run and how many of them
// came back translated.
func (r *Result) Segments() (translated, total int) {
	for _, d := range r.Documents {
		translated += d.Translated
		total += d.TotalSegments
	}
	return translated, total
}

// Failed lists the documents that could not be translated.
func (r *Result) Failed() []DocState {
	var out []DocState
	for _, d := range r.Documents {
		if d.Status == StatusFailed {
			out = append(out, d)
		}
	}
	return out
}

// Cache stores documents already translated so an interrupted run can be
// resumed without paying for the same chapters twice.
type Cache interface {
	Get(key string) ([]byte, bool)
	Put(key string, data []byte) error
}

// BookOptions extends Options with run-level choices.
type BookOptions struct {
	Options
	// Only restricts the run to these document names. Empty means every
	// translatable document.
	Only map[string]bool
	// Cache is optional; nil disables resume.
	Cache Cache
	// StopOnError aborts the run at the first failing document instead of
	// carrying on with the rest of the book.
	StopOnError bool
}

// Book translates every document of the book in place. The book is modified,
// not written: the caller decides where the result goes.
func Book(ctx context.Context, p llm.Provider, book *epub.Book, opts BookOptions, onProgress func(Progress)) (*Result, error) {
	opts.Options = opts.Options.Defaults()
	started := time.Now()

	titles := map[string]string{}
	for _, ch := range book.Chapters {
		if _, seen := titles[ch.Path]; !seen {
			titles[ch.Path] = ch.Title
		}
	}

	var names []string
	for _, name := range book.TranslatableDocuments() {
		if len(opts.Only) > 0 && !opts.Only[name] {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, errors.New("aucun document sélectionné pour la traduction")
	}

	res := &Result{Book: book}
	for _, name := range names {
		title := titles[name]
		if title == "" {
			title = "Navigation"
		}
		res.Documents = append(res.Documents, DocState{Name: name, Title: title, Status: StatusPending})
	}

	// live carries what the document in flight has consumed; res.Usage only
	// grows once a document is finished.
	var liveUsage llm.Usage
	var liveRequests, liveAttempted int

	report := func(current int, msg string, retry *llm.RetryNotice) {
		if onProgress == nil {
			return
		}
		snapshot := make([]DocState, len(res.Documents))
		copy(snapshot, res.Documents)
		total := res.Usage
		total.Add(liveUsage)
		onProgress(Progress{
			Documents: snapshot, Current: current, Message: msg, Retry: retry,
			Usage: total, Requests: res.Requests + liveRequests,
			Attempted: res.Attempted + liveAttempted,
		})
	}

	var tail string
	for i, name := range names {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		doc, ok := book.Read(name)
		if !ok {
			res.Documents[i].Status = StatusFailed
			res.Documents[i].Err = fmt.Errorf("document absent de l'archive")
			report(i, "", nil)
			continue
		}

		if opts.Cache != nil {
			if cached, ok := opts.Cache.Get(name); ok {
				book.Replace(name, cached)
				res.Documents[i].Status = StatusCached
				report(i, "already translated, reused from the cache", nil)
				continue
			}
		}

		res.Documents[i].Status = StatusRunning
		if segs, err := epub.Extract(doc); err == nil {
			res.Documents[i].TotalSegments = len(segs)
		}
		report(i, "", nil)

		docStart := time.Now()
		meta := DocMeta{BookTitle: book.Title, Title: res.Documents[i].Title, Index: i + 1, Total: len(names)}
		liveUsage, liveRequests, liveAttempted = llm.Usage{}, 0, 0
		out, err := Document(ctx, p, opts.Options, meta, name, doc, tail, func(e Event) {
			res.Documents[i].DoneSegments = e.DoneSegments
			res.Documents[i].TotalSegments = e.TotalSegments
			liveUsage, liveRequests, liveAttempted = e.DocUsage, e.DocRequests, e.DocAttempted
			report(i, e.Message, e.Retry)
		})
		liveUsage, liveRequests, liveAttempted = llm.Usage{}, 0, 0
		res.Documents[i].Duration = time.Since(docStart)

		if out != nil {
			res.Usage.Add(out.Usage)
			res.Requests += out.Requests
			res.Attempted += out.Attempted
			res.Notes = append(res.Notes, out.Notes...)
			res.Documents[i].Notes = len(out.Notes)
		}

		if err != nil {
			res.Documents[i].Status = StatusFailed
			res.Documents[i].Err = err
			report(i, "", nil)
			if ctx.Err() != nil {
				return res, ctx.Err()
			}
			// A backend that has stopped answering will not start again on the
			// next chapter; carrying on would only cost time and money.
			if errors.Is(err, ErrServiceUnusable) || opts.StopOnError {
				return res, fmt.Errorf("%s : %w", name, err)
			}
			continue
		}

		// A document where nothing at all came back translated is a failure,
		// not a success with notes. Replacing it with its own source text and
		// reporting a tick would hand the reader an untranslated book.
		if out.Segments > 0 && out.Translated == 0 {
			res.Documents[i].Status = StatusFailed
			res.Documents[i].Err = fmt.Errorf("aucun des %d segments n'a pu être traduit", out.Segments)
			report(i, "", nil)
			continue
		}

		book.Replace(name, epub.SetDocumentLanguage(out.Output, opts.TargetCode))
		tail = out.Tail
		res.Documents[i].Status = StatusDone
		res.Documents[i].Translated = out.Translated
		res.Documents[i].DoneSegments = out.Segments
		res.Documents[i].TotalSegments = out.Segments

		if opts.Cache != nil {
			final, _ := book.Read(name)
			if err := opts.Cache.Put(name, final); err != nil {
				res.Notes = append(res.Notes, Note{Document: name, Message: "n'a pas pu être mis en cache : " + err.Error()})
			}
		}
		report(i, "", nil)
	}

	if err := book.SetLanguage(opts.TargetCode); err != nil {
		res.Notes = append(res.Notes, Note{Document: book.OPFPath, Message: "langue du livre laissée inchangée : " + err.Error()})
	}
	res.Duration = time.Since(started)
	return res, nil
}

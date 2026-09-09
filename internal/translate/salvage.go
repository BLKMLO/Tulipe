package translate

import (
	"context"
	"errors"

	"github.com/blkmlo/tulipe/internal/i18n"
	"github.com/blkmlo/tulipe/internal/llm"
)

// The salvage pass is the second attempt at the passages a book came out of
// its first pass without. It runs once the whole book is done rather than in
// the middle of a chapter, for two reasons: a service having a bad minute is
// usually over it by the end of the book, and the reader gets a complete book
// to look at either way.
//
// It is not the same request sent twice. Three things differ:
//
//   - the batches are small, because a batch that came back unusable is one a
//     smaller batch often survives;
//   - the prompt says so, and restates the rules that fail most often
//     (Options.Salvage, read by systemPrompt);
//   - the provider may be a different one entirely.
//
// Only passages are retried, never a whole chapter: a document that failed
// outright was never replaced, so there is nothing to splice a translation
// into, and re-running it would mean paying for the chapter a second time.
const (
	// salvageMaxSegments and salvageChunkChars cap a salvage batch. A run
	// already using smaller values keeps its own: the point is to go down,
	// never up.
	salvageMaxSegments = 4
	salvageChunkChars  = 800
)

// salvageOptions turns the options of the first pass into those of the second.
func salvageOptions(o Options) Options {
	o.Salvage = true
	if o.MaxSegments > salvageMaxSegments {
		o.MaxSegments = salvageMaxSegments
	}
	if o.ChunkChars > salvageChunkChars {
		o.ChunkChars = salvageChunkChars
	}
	// A fresh counter: the guard is there to catch a service that has stopped
	// answering, and the one that just translated a book plainly has not. It
	// can still trip inside the pass if the service dies now.
	o.failures = &failureCounter{limit: o.StopAfterFailures}
	return o
}

// salvageJob is one document with passages left to retry, and the source it
// needs to retry them from. The source is kept aside during the first pass
// because book.Replace has since put the translation in its place.
type salvageJob struct {
	index   int
	name    string
	meta    DocMeta
	source  []byte
	pending []int
}

// salvage runs the second pass over the documents the first one flagged. It
// reports what it changed through res and onProgress; it returns an error only
// when the whole run must stop.
func salvage(ctx context.Context, p llm.Provider, book replacer, jobs []salvageJob,
	opts BookOptions, res *Result, report func(int, string, *llm.RetryNotice),
	live func(llm.Usage, int, int)) error {

	if len(jobs) == 0 {
		return nil
	}
	// The fallback provider is the whole point of the seam: a service that
	// could not answer for a passage may well be the wrong one to ask again.
	// Until a fallback is configured, the same provider gets the second try.
	if opts.Fallback != nil {
		p = opts.Fallback
	}
	sub := salvageOptions(opts.Options)

	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		previous, ok := book.Read(job.name)
		if !ok {
			continue
		}

		i := job.index
		report(i, i18n.T("translate.msg.salvage", len(job.pending)), nil)

		// The document's own progress counters are left alone: it is finished,
		// and rewinding its bar to "2 of 3" would read as work being undone.
		out, err := RetryPending(ctx, p, sub, job.meta, job.name,
			job.source, previous, job.pending, "", func(e Event) {
				live(e.DocUsage, e.DocRequests, e.DocAttempted)
				report(i, e.Message, e.Retry)
			})
		live(llm.Usage{}, 0, 0)

		if out != nil {
			res.Usage.Add(out.Usage)
			res.Requests += out.Requests
			res.Attempted += out.Attempted
			res.Notes = append(res.Notes, out.Notes...)
			res.Documents[i].Notes += len(out.Notes)
		}
		if err != nil {
			// The first pass produced a readable chapter. Losing it over a
			// touch-up would be a poor trade, so it stays exactly as it was.
			res.Notes = append(res.Notes, docNote(job.name, i18n.T("translate.note.salvage-failed", err)))
			res.Documents[i].Notes++
			report(i, "", nil)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, ErrServiceUnusable) {
				// Nothing is lost: every document keeps what the first pass
				// gave it. Stopping only means the remaining passages stay
				// flagged, which is where they already were.
				return nil
			}
			continue
		}

		book.Replace(job.name, out.Output)
		if out.Translated > 0 {
			// The first pass left a note saying the passage was kept in the
			// source language, and notes are never taken back. Saying what
			// became of it is how the record stays readable.
			res.Notes = append(res.Notes, docNote(job.name,
				i18n.T("translate.note.salvaged", out.Translated, len(job.pending))))
			res.Documents[i].Notes++
		}
		res.Documents[i].Translated += out.Translated
		res.Documents[i].Pending = len(out.Pending)
		if opts.Cache != nil {
			final, _ := book.Read(job.name)
			storePending(opts.Cache, job.name, out.Pending)
			if err := opts.Cache.Put(job.name, final); err != nil {
				res.Notes = append(res.Notes, docNote(job.name, i18n.T("translate.note.not-cached", err)))
			}
		}
		report(i, "", nil)
	}
	return nil
}

// replacer is the little of *epub.Book that the salvage pass needs. Naming it
// keeps the pass testable without an archive on disk.
type replacer interface {
	Read(name string) ([]byte, bool)
	Replace(name string, data []byte) bool
}

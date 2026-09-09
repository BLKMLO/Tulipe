package translate

import (
	"context"
	"fmt"

	"github.com/blkmlo/tulipe/internal/epub"
	"github.com/blkmlo/tulipe/internal/i18n"
	"github.com/blkmlo/tulipe/internal/llm"
)

// RetryPending re-translates only the segments an earlier pass left in the
// source language, and splices the results into the document that pass
// produced. A chapter where three paragraphs failed costs three paragraphs to
// finish, not a chapter.
//
// source is the original document, previous the translated one, and pending
// the segment indices the earlier pass reported. The two documents must still
// have the same structure — they do, since a translation only ever replaces
// the inner text of spans it found in the source.
func RetryPending(ctx context.Context, p llm.Provider, opts Options, meta DocMeta,
	name string, source, previous []byte, pending []int, tail string, onEvent func(Event)) (*DocResult, error) {

	opts = opts.Defaults()

	srcSegs, err := epub.Extract(source)
	if err != nil {
		return nil, fmt.Errorf("%s : %w", name, err)
	}
	prevSegs, err := epub.Extract(previous)
	if err != nil {
		return nil, fmt.Errorf("%s : %w", name, err)
	}
	if len(srcSegs) != len(prevSegs) {
		// Refusing here is the honest answer: splicing against a structure
		// that has drifted would put translations in the wrong places.
		return nil, fmt.Errorf(i18n.T("translate.err.structure-changed"),
			name, len(prevSegs), len(srcSegs))
	}

	subset, numbers := selectSegments(srcSegs, pending)
	if opts.KeepOriginalTitles {
		// A pending list written before the setting was turned off can name
		// headings. Sending them now would translate what the reader asked to
		// keep.
		subset, numbers = dropTitles(subset, numbers, meta.Navigation)
	}
	res := &DocResult{Segments: len(subset), Tail: tail}
	if len(subset) == 0 {
		res.Output = previous
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

	// Everything outside the retried segments keeps what the earlier pass
	// produced: an empty translation means "leave this span alone".
	full := make([]string, len(prevSegs))
	for k, idx := range numbers {
		full[idx] = translations[k]
	}
	out, err := epub.Apply(previous, prevSegs, full)
	if err != nil {
		return nil, fmt.Errorf("%s : %w", name, err)
	}
	res.Output = out
	res.Tail = t.tail
	return res, ctx.Err()
}

// dropTitles removes the headings from a retry subset, keeping the two slices
// in step.
func dropTitles(subset []epub.Segment, numbers []int, navigation bool) ([]epub.Segment, []int) {
	keptSegs := subset[:0:0]
	keptNums := numbers[:0:0]
	for i, s := range subset {
		if s.Title || navigation {
			continue
		}
		keptSegs = append(keptSegs, s)
		keptNums = append(keptNums, numbers[i])
	}
	return keptSegs, keptNums
}

// selectSegments picks the segments named by pending, ignoring indices that no
// longer exist and never returning the same one twice.
func selectSegments(segs []epub.Segment, pending []int) (subset []epub.Segment, numbers []int) {
	seen := make(map[int]bool, len(pending))
	for _, i := range pending {
		if i < 0 || i >= len(segs) || seen[i] {
			continue
		}
		seen[i] = true
		subset = append(subset, segs[i])
		numbers = append(numbers, i)
	}
	return subset, numbers
}

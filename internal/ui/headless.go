package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/blkmlo/tulipe/internal/config"
	"github.com/blkmlo/tulipe/internal/epub"
	"github.com/blkmlo/tulipe/internal/i18n"
	"github.com/blkmlo/tulipe/internal/llm"
	"github.com/blkmlo/tulipe/internal/translate"
)

// RunHeadless translates a book without the interactive interface. Progress is
// written to standard error so that standard output carries only the path of
// the finished book, which keeps the command usable in a pipeline.
func RunHeadless(ctx context.Context, cfg config.Config, source, output string, retry, quiet bool) error {
	book, err := epub.Open(source)
	if err != nil {
		return err
	}
	provider, err := cfg.NewProvider()
	if err != nil {
		return err
	}

	if output == "" {
		output = outputPath(cfg, source)
	}
	// Prove the destination is writable now, not after paying for the book.
	if err := checkWritable(output); err != nil {
		return err
	}

	opts := translate.BookOptions{Options: cfg.TranslateOptions(), RetryPending: retry}
	if cfg.Resume {
		if fp, err := translate.Fingerprint(source); err == nil {
			title := book.Title
			if title == "" {
				title = source
			}
			cache, err := translate.OpenCache(translate.CacheRoot(), fp, cfg.Recipe(), translate.RunInfo{
				Source: source, BookTitle: title, TargetLanguage: cfg.TargetLanguage, Model: cfg.Model,
			})
			if err == nil {
				opts.Cache = cache
			}
		}
	}

	log := func(format string, args ...any) {
		if !quiet {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		}
	}
	log(i18n.T("cli.headless.header"), orDash(book.Title), cfg.TargetLanguage, cfg.Model, cfg.Provider)

	seen := map[int]translate.Status{}
	res, err := translate.Book(ctx, provider, book, opts, func(p translate.Progress) {
		for i, d := range p.Documents {
			if seen[i] == d.Status {
				continue
			}
			seen[i] = d.Status
			switch d.Status {
			case translate.StatusRunning:
				log(i18n.T("cli.headless.running"), d.Title, d.TotalSegments)
			case translate.StatusDone:
				log(i18n.T("cli.headless.done"), d.Title, d.TotalSegments, d.Duration.Round(time.Second))
			case translate.StatusCached:
				log(i18n.T("cli.headless.cached"), d.Title)
			case translate.StatusFailed:
				log(i18n.T("cli.headless.failed"), d.Title, d.Err)
			}
		}
		if p.Retry != nil {
			log(i18n.T("cli.headless.retry"), p.Retry.Attempt, p.Retry.Wait.Round(time.Second), p.Retry.Err)
		}
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			log(i18n.T("cli.headless.cancelled"))
		}
		return err
	}

	final, err := writeBook(book, output, cfg.Format)
	if err != nil {
		return err
	}

	translated, total := res.Segments()
	failed := res.Failed()

	log("")
	log(i18n.T("cli.headless.segments"), segmentsPlain(translated, total))
	if p := res.Pending(); p > 0 {
		log(i18n.T("cli.headless.pending"), p)
	}
	log(i18n.T("cli.headless.requests"), requestsPlain(res.Requests, res.Attempted))
	log(i18n.T("cli.headless.tokens"), usagePlain(res.Usage, res.Attempted))
	log(i18n.T("cli.headless.duration"), res.Duration.Round(time.Second))
	if n := len(res.Notes); n > 0 {
		log(i18n.T("cli.headless.notes"), n)
		for i, note := range res.Notes {
			if i == 20 {
				log(i18n.T("cli.headless.notes.more"), n-20)
				break
			}
			log("          %s", note)
		}
	}

	// The file is always written — partial work is worth keeping — but the
	// exit status must not claim success for a book that is not translated.
	fmt.Println(final)
	if len(failed) > 0 {
		var names []string
		for _, d := range failed {
			names = append(names, fmt.Sprintf("%s (%v)", d.Title, d.Err))
		}
		return fmt.Errorf(i18n.T("cli.headless.err.failed"),
			len(failed), strings.Join(names, " ; "))
	}
	if total > 0 && translated == 0 {
		return errors.New(i18n.T("cli.headless.err.none"))
	}
	return nil
}

// segmentsPlain states what was actually translated. A run served entirely
// from the cache has nothing to count, and saying "0 sur 0" would read as a
// failure.
func segmentsPlain(translated, total int) string {
	if total == 0 {
		return i18n.T("ui.segments.none")
	}
	return i18n.T("ui.segments.count", translated, total)
}

func requestsPlain(requests, attempted int) string {
	if attempted == 0 {
		return i18n.T("ui.requests.none")
	}
	if attempted == requests {
		return fmt.Sprintf("%d", requests)
	}
	return i18n.T("ui.requests.mixed", requests, attempted)
}

func usagePlain(u llm.Usage, attempted int) string {
	if attempted == 0 {
		return i18n.T("ui.usage.no-call")
	}
	if !u.Reported {
		return i18n.T("ui.usage.not-reported")
	}
	return i18n.T("ui.usage.in-out.plain", u.InputTokens, u.OutputTokens)
}

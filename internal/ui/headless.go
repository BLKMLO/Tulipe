package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/blkmlo/tulipe/internal/config"
	"github.com/blkmlo/tulipe/internal/epub"
	"github.com/blkmlo/tulipe/internal/llm"
	"github.com/blkmlo/tulipe/internal/translate"
)

// RunHeadless translates a book without the interactive interface. Progress is
// written to standard error so that standard output carries only the path of
// the finished book, which keeps the command usable in a pipeline.
func RunHeadless(ctx context.Context, cfg config.Config, source, output string, quiet bool) error {
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

	opts := translate.BookOptions{Options: cfg.TranslateOptions()}
	if cfg.Resume {
		if fp, err := translate.Fingerprint(source); err == nil {
			title := book.Title
			if title == "" {
				title = source
			}
			cache, err := translate.OpenCache(translate.CacheRoot(), fp, cfg.TargetLanguage, cfg.Model, translate.RunInfo{
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
	log("tulipe — %s → %s (%s via %s)", orDash(book.Title), cfg.TargetLanguage, cfg.Model, cfg.Provider)

	seen := map[int]translate.Status{}
	res, err := translate.Book(ctx, provider, book, opts, func(p translate.Progress) {
		for i, d := range p.Documents {
			if seen[i] == d.Status {
				continue
			}
			seen[i] = d.Status
			switch d.Status {
			case translate.StatusRunning:
				log("  … %s (%d segments)", d.Title, d.TotalSegments)
			case translate.StatusDone:
				log("  ✓ %s — %d segments en %s", d.Title, d.TotalSegments, d.Duration.Round(time.Second))
			case translate.StatusCached:
				log("  ◆ %s — réutilisé du cache", d.Title)
			case translate.StatusFailed:
				log("  ✗ %s — %v", d.Title, d.Err)
			}
		}
		if p.Retry != nil {
			log("    nouvelle tentative %d dans %s — %v", p.Retry.Attempt, p.Retry.Wait.Round(time.Second), p.Retry.Err)
		}
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			log("interrompu — les documents déjà traduits sont conservés pour une reprise")
		}
		return err
	}

	final, err := writeBook(book, output)
	if err != nil {
		return err
	}

	log("")
	log("requêtes  %d", res.Requests)
	log("jetons    %s", usagePlain(res.Usage, res.Requests))
	log("durée     %s", res.Duration.Round(time.Second))
	if n := len(res.Notes); n > 0 {
		log("signalés  %d passage(s) laissés en langue source :", n)
		for i, note := range res.Notes {
			if i == 20 {
				log("          … et %d autres", n-20)
				break
			}
			log("          %s", note)
		}
	}
	fmt.Println(final)
	return nil
}

func usagePlain(u llm.Usage, requests int) string {
	if requests == 0 {
		return "aucune requête — tout venait du cache de reprise"
	}
	if !u.Reported {
		return "non communiqués par le fournisseur"
	}
	return fmt.Sprintf("%d entrants, %d sortants", u.InputTokens, u.OutputTokens)
}

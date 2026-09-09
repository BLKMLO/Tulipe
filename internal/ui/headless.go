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

	final, err := writeBook(book, output, cfg.Format)
	if err != nil {
		return err
	}

	translated, total := res.Segments()
	failed := res.Failed()

	log("")
	log("segments  %s", segmentsPlain(translated, total))
	if p := res.Pending(); p > 0 {
		log("en attente %d passage(s) laissés en langue source — « tulipe translate --retry » pour les reprendre", p)
	}
	log("requêtes  %s", requestsPlain(res.Requests, res.Attempted))
	log("jetons    %s", usagePlain(res.Usage, res.Attempted))
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

	// The file is always written — partial work is worth keeping — but the
	// exit status must not claim success for a book that is not translated.
	fmt.Println(final)
	if len(failed) > 0 {
		var names []string
		for _, d := range failed {
			names = append(names, fmt.Sprintf("%s (%v)", d.Title, d.Err))
		}
		return fmt.Errorf("%d document(s) non traduits, le fichier produit les contient en langue source : %s",
			len(failed), strings.Join(names, " ; "))
	}
	if total > 0 && translated == 0 {
		return errors.New("aucun segment n'a été traduit ; le fichier produit est une copie de la source")
	}
	return nil
}

// segmentsPlain states what was actually translated. A run served entirely
// from the cache has nothing to count, and saying "0 sur 0" would read as a
// failure.
func segmentsPlain(translated, total int) string {
	if total == 0 {
		return "aucun à traduire lors de cette passe"
	}
	return fmt.Sprintf("%d traduits sur %d", translated, total)
}

func requestsPlain(requests, attempted int) string {
	if attempted == 0 {
		return "aucune — tout venait du cache de reprise"
	}
	if attempted == requests {
		return fmt.Sprintf("%d", requests)
	}
	return fmt.Sprintf("%d réussies sur %d appels", requests, attempted)
}

func usagePlain(u llm.Usage, attempted int) string {
	if attempted == 0 {
		return "aucun appel"
	}
	if !u.Reported {
		return "non communiqués par le fournisseur"
	}
	return fmt.Sprintf("%d entrants, %d sortants", u.InputTokens, u.OutputTokens)
}

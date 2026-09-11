// Command tulipe translates EPUB books with a configurable AI model, one
// chapter at a time.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/blkmlo/tulipe/internal/config"
	"github.com/blkmlo/tulipe/internal/i18n"
	"github.com/blkmlo/tulipe/internal/llm"
	"github.com/blkmlo/tulipe/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "tulipe: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	// The interface language is read before anything is printed, so that even
	// a usage message or a configuration error comes out in the right one.
	if cfg, err := config.Load(); err == nil {
		i18n.SetLocale(cfg.Language)
	}
	if len(args) > 0 {
		switch args[0] {
		case "translate":
			return runTranslate(args[1:])
		case "config":
			return showConfig(takeLang(args[1:]))
		case "providers":
			return showProviders(takeLang(args[1:]))
		case "models":
			return showModels(takeLang(args[1:]))
		case "version", "--version", "-version":
			fmt.Println("tulipe " + version)
			return nil
		case "help", "--help", "-h":
			fmt.Print(i18n.T("cli.usage"))
			translateFlags(&config.Config{}, new(cliOptions)).PrintDefaults()
			return nil
		}
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.T("cli.config-ignored")+err.Error())
	}

	var start string
	if len(args) > 0 {
		start, err = filepath.Abs(args[0])
		if err != nil {
			return err
		}
		if _, err := os.Stat(start); err != nil {
			return err
		}
	}

	p := tea.NewProgram(ui.New(cfg, start), tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func showConfig(_ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	line("cli.config.file", config.Path())
	name := cfg.Provider
	if p, ok := cfg.Preset(); ok {
		name = fmt.Sprintf("%s (%s)", p.ID, p.DisplayName())
	}
	line("cli.config.provider", name)
	line("cli.config.model", cfg.Model)
	if e := cfg.Endpoint(); e != "" {
		line("cli.config.base-url", e)
	}
	if cfg.Kind() == llm.KindAnthropic && cfg.Effort != "" {
		line("cli.config.effort", cfg.Effort)
	}
	// The key itself is never printed.
	line("cli.config.api-key", cfg.KeyStatus())
	line("cli.config.interface", i18n.LocaleName(cfg.Language))
	line("cli.config.target", fmt.Sprintf("%s (%s)", cfg.TargetLanguage, orNone(cfg.TargetCode)))
	line("cli.config.source", orNone(cfg.SourceLanguage))
	line("cli.config.chunking", i18n.T("cli.config.chunking.value", cfg.ChunkChars, cfg.MaxSegments))
	line("cli.config.max-tokens", fmt.Sprintf("%d", cfg.MaxTokens))
	line("cli.config.attempts", fmt.Sprintf("%d", cfg.Attempts))
	line("cli.config.timeout", i18n.T("cli.config.timeout.value", cfg.TimeoutSeconds))
	line("cli.config.context", i18n.T("cli.config.context.value", cfg.ContextChars))
	line("cli.config.resume", fmt.Sprintf("%v", cfg.Resume))
	line("cli.config.salvage", fmt.Sprintf("%v", cfg.SalvagePass))
	line("cli.config.titles", fmt.Sprintf("%v", cfg.TranslateTitles))
	line("cli.config.about", orNone(cfg.About))
	line("cli.config.format", cfg.Format)
	line("cli.config.output-dir", orNone(cfg.OutputDir))
	return nil
}

// configLabel is the width of the label column of "tulipe config". Labels are
// padded here rather than written with their own spaces: they are not the same
// length in every language.
const configLabel = 22

func line(key, value string) {
	label := i18n.T(key)
	for len([]rune(label)) < configLabel {
		label += " "
	}
	fmt.Println(label + value)
}

// showProviders prints the catalogue. It deliberately carries no quota or
// pricing figures: those change often, and a stale number printed as fact would
// mislead. Each entry points at the service's own page instead.
func showProviders(_ []string) error {
	cfg, _ := config.Load()
	fmt.Println(i18n.T("cli.providers.title"))
	fmt.Println()
	for _, p := range llm.Presets() {
		mark := "  "
		if p.ID == cfg.Provider {
			mark = "▸ "
		}
		tag := ""
		switch {
		case p.FreeTier:
			tag = i18n.T("cli.providers.free")
		case p.NoKey:
			tag = i18n.T("cli.providers.local")
		}
		fmt.Printf("%s%-14s %s%s\n", mark, p.ID, p.DisplayName(), tag)
		if p.BaseURL != "" {
			fmt.Println(i18n.T("cli.providers.url", p.BaseURL))
		}
		switch {
		case p.NoKey:
			fmt.Println(i18n.T("cli.providers.key-none"))
		case len(p.KeyEnv) > 0:
			fmt.Println(i18n.T("cli.providers.key", strings.Join(p.KeyEnv, i18n.T("cli.providers.or"))))
		}
		if note := p.NoteText(); note != "" {
			fmt.Println(i18n.T("cli.providers.note", note))
		}
		if p.Docs != "" {
			fmt.Println(i18n.T("cli.providers.docs", p.Docs))
		}
		fmt.Println()
	}
	fmt.Println(i18n.T("cli.providers.footer"))
	return nil
}

// showModels asks the configured service which models it serves, rather than
// shipping a list that would go stale.
func showModels(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.T("cli.config-ignored")+err.Error())
	}
	fs := flag.NewFlagSet("models", flag.ContinueOnError)
	fs.StringVar(&cfg.Provider, "provider", cfg.Provider, i18n.T("cli.flag.models.provider"))
	fs.StringVar(&cfg.BaseURL, "base-url", cfg.BaseURL, i18n.T("cli.flag.models.base-url"))
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, i18n.T("cli.models.usage"))
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.Model == "" {
		// Listing needs no model, but Validate does; any value will do.
		cfg.Model = "-"
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	provider, err := cfg.NewProvider()
	if err != nil {
		return err
	}
	lister, ok := provider.(llm.ModelLister)
	if !ok {
		return fmt.Errorf(i18n.T("cli.models.cannot"), cfg.Provider)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	models, err := lister.ListModels(ctx)
	if err != nil {
		return err
	}
	for _, m := range models {
		fmt.Println(m)
	}
	return nil
}

// takeLang honours --lang on the commands that only print. They have no flag
// set of their own, and silently ignoring the flag would answer in the wrong
// language without saying why.
func takeLang(args []string) []string {
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--lang" || a == "-lang":
			if i+1 < len(args) {
				i++
				i18n.SetLocale(args[i])
			}
		case strings.HasPrefix(a, "--lang="):
			i18n.SetLocale(strings.TrimPrefix(a, "--lang="))
		case strings.HasPrefix(a, "-lang="):
			i18n.SetLocale(strings.TrimPrefix(a, "-lang="))
		default:
			rest = append(rest, a)
		}
	}
	return rest
}

func orNone(s string) string {
	if s == "" {
		return i18n.T("ui.dash")
	}
	return s
}

type cliOptions struct {
	output       string
	chapters     string
	glossaryFile string
	noResume     bool
	retry        bool
	quiet        bool
	yes          bool
}

func translateFlags(cfg *config.Config, o *cliOptions) *flag.FlagSet {
	fs := flag.NewFlagSet("translate", flag.ContinueOnError)
	fs.StringVar(&cfg.Language, "lang", cfg.Language, i18n.T("cli.flag.lang"))
	fs.StringVar(&cfg.TargetLanguage, "to", cfg.TargetLanguage, i18n.T("cli.flag.to"))
	fs.StringVar(&cfg.TargetCode, "code", cfg.TargetCode, i18n.T("cli.flag.code"))
	fs.StringVar(&cfg.SourceLanguage, "from", cfg.SourceLanguage, i18n.T("cli.flag.from"))
	fs.StringVar(&cfg.Provider, "provider", cfg.Provider, i18n.T("cli.flag.provider"))
	fs.StringVar(&cfg.Model, "model", cfg.Model, i18n.T("cli.flag.model"))
	fs.StringVar(&cfg.BaseURL, "base-url", cfg.BaseURL, i18n.T("cli.flag.base-url"))
	fs.StringVar(&cfg.Effort, "effort", cfg.Effort, i18n.T("cli.flag.effort"))
	fs.IntVar(&cfg.ChunkChars, "chunk", cfg.ChunkChars, i18n.T("cli.flag.chunk"))
	fs.IntVar(&cfg.MaxSegments, "max-segments", cfg.MaxSegments, i18n.T("cli.flag.max-segments"))
	fs.Int64Var(&cfg.MaxTokens, "max-tokens", cfg.MaxTokens, i18n.T("cli.flag.max-tokens"))
	fs.IntVar(&cfg.Attempts, "attempts", cfg.Attempts, i18n.T("cli.flag.attempts"))
	fs.IntVar(&cfg.ContextChars, "context", cfg.ContextChars, i18n.T("cli.flag.context"))
	fs.IntVar(&cfg.TimeoutSeconds, "timeout", cfg.TimeoutSeconds, i18n.T("cli.flag.timeout"))
	fs.StringVar(&cfg.StyleNotes, "style", cfg.StyleNotes, i18n.T("cli.flag.style"))
	fs.StringVar(&cfg.About, "about", cfg.About, i18n.T("cli.flag.about"))
	fs.BoolVar(&cfg.TranslateTitles, "titles", cfg.TranslateTitles, i18n.T("cli.flag.titles"))
	fs.BoolVar(&cfg.SalvagePass, "salvage", cfg.SalvagePass, i18n.T("cli.flag.salvage"))
	fs.StringVar(&cfg.SourceCode, "from-code", cfg.SourceCode, i18n.T("cli.flag.from-code"))
	fs.StringVar(&cfg.Format, "format", cfg.Format, i18n.T("cli.flag.format"))
	fs.StringVar(&o.output, "o", "", i18n.T("cli.flag.output"))
	fs.StringVar(&o.chapters, "chapters", "", i18n.T("cli.flag.chapters"))
	fs.StringVar(&o.glossaryFile, "glossary-file", "", i18n.T("cli.flag.glossary-file"))
	fs.BoolVar(&o.noResume, "no-resume", false, i18n.T("cli.flag.no-resume"))
	fs.BoolVar(&o.retry, "retry", false, i18n.T("cli.flag.retry"))
	fs.BoolVar(&o.quiet, "quiet", false, i18n.T("cli.flag.quiet"))
	return fs
}

func runTranslate(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.T("cli.config-ignored")+err.Error())
	}
	var o cliOptions
	fs := translateFlags(&cfg, &o)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, i18n.T("cli.translate.usage"))
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New(i18n.T("cli.translate.one"))
	}
	if o.glossaryFile != "" {
		data, err := os.ReadFile(o.glossaryFile)
		if err != nil {
			return err
		}
		cfg.Glossary = strings.TrimSpace(string(data))
	}
	if o.noResume {
		cfg.Resume = false
	}

	// A --lang given on the command line applies to what follows, including
	// the validation errors just below.
	i18n.SetLocale(cfg.Language)

	if err := cfg.Validate(); err != nil {
		return err
	}

	source, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf(i18n.T("cli.err.is-a-folder"), source)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return ui.RunHeadless(ctx, cfg, source, ui.HeadlessOptions{
		Output:   o.output,
		Chapters: o.chapters,
		Retry:    o.retry,
		Quiet:    o.quiet,
	})
}

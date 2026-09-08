// Command tulipe translates EPUB books with a configurable AI model, one
// chapter at a time.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/blkmlo/tulipe/internal/config"
	"github.com/blkmlo/tulipe/internal/llm"
	"github.com/blkmlo/tulipe/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `tulipe — traduction d'EPUB, chapitre par chapitre

  tulipe                        ouvre le menu interactif
  tulipe livre.epub             ouvre le menu sur ce livre
  tulipe translate livre.epub   traduit sans interface (scripts, lots)
  tulipe config                 affiche la configuration courante
  tulipe providers              liste les services connus et leur clé attendue
  tulipe models                 demande au service la liste de ses modèles
  tulipe version

Options de « translate » :
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "tulipe: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "translate":
			return runTranslate(args[1:])
		case "config":
			return showConfig()
		case "providers":
			return showProviders()
		case "models":
			return showModels(args[1:])
		case "version", "--version", "-version":
			fmt.Println("tulipe " + version)
			return nil
		case "help", "--help", "-h":
			fmt.Print(usage)
			translateFlags(&config.Config{}, new(cliOptions)).PrintDefaults()
			return nil
		}
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tulipe: configuration ignorée: "+err.Error())
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

func showConfig() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fmt.Println("fichier            " + config.Path())
	name := cfg.Provider
	if p, ok := cfg.Preset(); ok {
		name = fmt.Sprintf("%s (%s)", p.ID, p.Name)
	}
	fmt.Println("fournisseur        " + name)
	fmt.Println("modèle             " + cfg.Model)
	if e := cfg.Endpoint(); e != "" {
		fmt.Println("url de base        " + e)
	}
	if cfg.Kind() == llm.KindAnthropic && cfg.Effort != "" {
		fmt.Println("effort             " + cfg.Effort)
	}
	// The key itself is never printed.
	fmt.Println("clé API            " + cfg.KeyStatus())
	fmt.Printf("langue cible       %s (%s)\n", cfg.TargetLanguage, orNone(cfg.TargetCode))
	fmt.Println("langue source      " + orNone(cfg.SourceLanguage))
	fmt.Printf("découpage          %d caractères / %d segments par requête\n", cfg.ChunkChars, cfg.MaxSegments)
	fmt.Printf("jetons de réponse  %d\n", cfg.MaxTokens)
	fmt.Printf("tentatives         %d\n", cfg.Attempts)
	fmt.Printf("délai par appel    %d s\n", cfg.TimeoutSeconds)
	fmt.Printf("continuité         %d caractères\n", cfg.ContextChars)
	fmt.Printf("reprise            %v\n", cfg.Resume)
	fmt.Println("format de sortie   " + cfg.Format)
	fmt.Println("dossier de sortie  " + orNone(cfg.OutputDir))
	return nil
}

// showProviders prints the catalogue. It deliberately carries no quota or
// pricing figures: those change often, and a stale number printed as fact would
// mislead. Each entry points at the service's own page instead.
func showProviders() error {
	cfg, _ := config.Load()
	fmt.Println("Services connus (« ▸ » : celui qui est configuré)")
	fmt.Println()
	for _, p := range llm.Presets() {
		mark := "  "
		if p.ID == cfg.Provider {
			mark = "▸ "
		}
		tag := ""
		switch {
		case p.FreeTier:
			tag = "  [offre gratuite annoncée]"
		case p.NoKey:
			tag = "  [sur votre machine]"
		}
		fmt.Printf("%s%-14s %s%s\n", mark, p.ID, p.Name, tag)
		if p.BaseURL != "" {
			fmt.Printf("    url    %s\n", p.BaseURL)
		}
		switch {
		case p.NoKey:
			fmt.Printf("    clé    inutile\n")
		case len(p.KeyEnv) > 0:
			fmt.Printf("    clé    %s\n", strings.Join(p.KeyEnv, " ou "))
		}
		if p.Note != "" {
			fmt.Printf("    note   %s\n", p.Note)
		}
		if p.Docs != "" {
			fmt.Printf("    voir   %s\n", p.Docs)
		}
		fmt.Println()
	}
	fmt.Println("Les conditions de chaque offre gratuite sont sur le site du service ;")
	fmt.Println("Tulipe n'en garde aucune copie, elles changent trop souvent.")
	return nil
}

// showModels asks the configured service which models it serves, rather than
// shipping a list that would go stale.
func showModels(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tulipe: configuration ignorée: "+err.Error())
	}
	fs := flag.NewFlagSet("models", flag.ContinueOnError)
	fs.StringVar(&cfg.Provider, "provider", cfg.Provider, "service à interroger")
	fs.StringVar(&cfg.BaseURL, "base-url", cfg.BaseURL, "URL de base")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "tulipe models [--provider <service>] [--base-url <url>]")
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
		return fmt.Errorf("%s ne sait pas lister ses modèles", cfg.Provider)
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

func orNone(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

type cliOptions struct {
	output       string
	glossaryFile string
	noResume     bool
	quiet        bool
	yes          bool
}

func translateFlags(cfg *config.Config, o *cliOptions) *flag.FlagSet {
	fs := flag.NewFlagSet("translate", flag.ContinueOnError)
	fs.StringVar(&cfg.TargetLanguage, "to", cfg.TargetLanguage, "langue cible, écrite comme un humain l'écrirait")
	fs.StringVar(&cfg.TargetCode, "code", cfg.TargetCode, "étiquette BCP 47 inscrite dans les métadonnées (vide : inchangée)")
	fs.StringVar(&cfg.SourceLanguage, "from", cfg.SourceLanguage, "langue source (vide : détectée par le modèle)")
	fs.StringVar(&cfg.Provider, "provider", cfg.Provider, "fournisseur : anthropic ou openai-compatible")
	fs.StringVar(&cfg.Model, "model", cfg.Model, "identifiant du modèle")
	fs.StringVar(&cfg.BaseURL, "base-url", cfg.BaseURL, "URL de base d'un service compatible OpenAI")
	fs.StringVar(&cfg.Effort, "effort", cfg.Effort, "effort du modèle : low, medium, high, xhigh, max")
	fs.IntVar(&cfg.ChunkChars, "chunk", cfg.ChunkChars, "caractères source par requête")
	fs.IntVar(&cfg.MaxSegments, "max-segments", cfg.MaxSegments, "segments par requête")
	fs.Int64Var(&cfg.MaxTokens, "max-tokens", cfg.MaxTokens, "jetons de réponse par requête")
	fs.IntVar(&cfg.Attempts, "attempts", cfg.Attempts, "tentatives avant redécoupage d'un lot")
	fs.IntVar(&cfg.ContextChars, "context", cfg.ContextChars, "caractères de continuité montrés au modèle")
	fs.IntVar(&cfg.TimeoutSeconds, "timeout", cfg.TimeoutSeconds, "secondes accordées à un appel au modèle")
	fs.StringVar(&cfg.StyleNotes, "style", cfg.StyleNotes, "consignes de style ajoutées aux instructions")
	fs.StringVar(&cfg.SourceCode, "from-code", cfg.SourceCode, "étiquette BCP 47 de la langue source (utile à DeepL)")
	fs.StringVar(&cfg.Format, "format", cfg.Format, "format de sortie : epub ou txt")
	fs.StringVar(&o.output, "o", "", "fichier de sortie (défaut : <livre>.<code>.epub, jamais écrasé)")
	fs.StringVar(&o.glossaryFile, "glossary-file", "", "fichier de glossaire, une règle « source = cible » par ligne")
	fs.BoolVar(&o.noResume, "no-resume", false, "ne pas réutiliser les chapitres déjà traduits")
	fs.BoolVar(&o.quiet, "quiet", false, "n'afficher que le résultat final")
	return fs
}

func runTranslate(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tulipe: configuration ignorée: "+err.Error())
	}
	var o cliOptions
	fs := translateFlags(&cfg, &o)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "tulipe translate [options] livre.epub")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("un fichier EPUB et un seul est attendu")
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
		return fmt.Errorf("%s est un dossier, pas un fichier EPUB", source)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return ui.RunHeadless(ctx, cfg, source, o.output, o.quiet)
}

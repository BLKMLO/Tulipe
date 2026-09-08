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

	"github.com/blkmlo/tulipe/internal/config"
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
	fmt.Println("fournisseur        " + cfg.Provider)
	fmt.Println("modèle             " + cfg.Model)
	if cfg.BaseURL != "" {
		fmt.Println("url de base        " + cfg.BaseURL)
	}
	if cfg.Provider == config.ProviderAnthropic && cfg.Effort != "" {
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
	fmt.Println("dossier de sortie  " + orNone(cfg.OutputDir))
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

// Package config holds Tulipe's persistent settings.
//
// The file lives in the user's configuration directory with 0600 permissions
// because it may hold an API key. Environment variables always win over the
// file, so a key never has to be written to disk at all.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/blkmlo/tulipe/internal/llm"
	"github.com/blkmlo/tulipe/internal/translate"
)

// Provider identifiers.
const (
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai-compatible"
)

// Config is everything Tulipe remembers between runs.
type Config struct {
	Provider         string   `json:"provider"`
	Model            string   `json:"model"`
	BaseURL          string   `json:"base_url,omitempty"`
	APIKey           string   `json:"api_key,omitempty"`
	Effort           string   `json:"effort,omitempty"`
	Temperature      *float64 `json:"temperature,omitempty"`
	StructuredOutput bool     `json:"structured_output"`

	TargetLanguage string `json:"target_language"`
	TargetCode     string `json:"target_code"`
	SourceLanguage string `json:"source_language,omitempty"`
	Glossary       string `json:"glossary,omitempty"`
	StyleNotes     string `json:"style_notes,omitempty"`

	ChunkChars   int   `json:"chunk_chars"`
	MaxSegments  int   `json:"max_segments"`
	MaxTokens    int64 `json:"max_tokens"`
	Attempts     int   `json:"attempts"`
	ContextChars int   `json:"context_chars"`
	// TimeoutSeconds caps one call to the model.
	TimeoutSeconds int `json:"timeout_seconds"`

	OutputDir string `json:"output_dir,omitempty"`
	Resume    bool   `json:"resume"`
}

// Default is the configuration a first run starts from.
func Default() Config {
	return Config{
		Provider:         ProviderAnthropic,
		Model:            llm.DefaultAnthropicModel,
		Effort:           "medium",
		StructuredOutput: true,
		TargetLanguage:   "français",
		TargetCode:       "fr",
		ChunkChars:       4000,
		MaxSegments:      40,
		MaxTokens:        16000,
		Attempts:         4,
		ContextChars:     400,
		TimeoutSeconds:   300,
		Resume:           true,
	}
}

// Path is where the configuration file lives.
func Path() string {
	if p := os.Getenv("TULIPE_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".", "tulipe.json")
	}
	return filepath.Join(dir, "tulipe", "config.json")
}

// Load reads the configuration, falling back to the defaults when no file
// exists yet. Unknown or missing fields keep their default value.
func Load() (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(Path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("%s: %w", Path(), err)
	}
	return cfg.normalise(), nil
}

// Save writes the configuration back to disk.
func (c Config) Save() error {
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c.normalise(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func (c Config) normalise() Config {
	if c.Provider == "" {
		c.Provider = ProviderAnthropic
	}
	if c.Model == "" && c.Provider == ProviderAnthropic {
		c.Model = llm.DefaultAnthropicModel
	}
	d := Default()
	if c.TargetLanguage == "" {
		c.TargetLanguage, c.TargetCode = d.TargetLanguage, d.TargetCode
	}
	if c.ChunkChars <= 0 {
		c.ChunkChars = d.ChunkChars
	}
	if c.MaxSegments <= 0 {
		c.MaxSegments = d.MaxSegments
	}
	if c.MaxTokens <= 0 {
		c.MaxTokens = d.MaxTokens
	}
	if c.Attempts <= 0 {
		c.Attempts = d.Attempts
	}
	if c.ContextChars <= 0 {
		c.ContextChars = d.ContextChars
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = d.TimeoutSeconds
	}
	return c
}

// Validate refuses a configuration that cannot do what it says. Silently
// falling back to a default would leave the user believing a setting took
// effect when it did not.
func (c Config) Validate() error {
	switch c.Provider {
	case ProviderAnthropic, ProviderOpenAI:
	default:
		return fmt.Errorf("fournisseur inconnu %q ; attendu %q ou %q", c.Provider, ProviderAnthropic, ProviderOpenAI)
	}
	if strings.TrimSpace(c.Model) == "" {
		return errors.New("aucun modèle indiqué")
	}
	if strings.TrimSpace(c.TargetLanguage) == "" {
		return errors.New("aucune langue cible indiquée")
	}
	if c.Provider == ProviderOpenAI && strings.TrimSpace(c.BaseURL) == "" {
		return errors.New("un service compatible OpenAI exige une URL de base (par exemple http://localhost:11434/v1)")
	}
	if c.BaseURL != "" {
		u, err := url.Parse(c.BaseURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("URL de base invalide %q ; attendu une adresse complète comme http://localhost:11434/v1", c.BaseURL)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("URL de base en %q ; seuls http et https sont acceptés", u.Scheme)
		}
	}
	if c.Effort != "" && c.Provider == ProviderAnthropic && !slices.Contains(Efforts, c.Effort) {
		return fmt.Errorf("effort inconnu %q ; attendu %s", c.Effort, strings.Join(Efforts, ", "))
	}
	for _, f := range []struct {
		name string
		v    int
		min  int
	}{
		{"--chunk", c.ChunkChars, 1},
		{"--max-segments", c.MaxSegments, 1},
		{"--max-tokens", int(c.MaxTokens), 1},
		{"--attempts", c.Attempts, 1},
		{"--context", c.ContextChars, 0},
		{"--timeout", c.TimeoutSeconds, 1},
	} {
		if f.v < f.min {
			return fmt.Errorf("%s vaut %d ; le minimum est %d", f.name, f.v, f.min)
		}
	}
	if c.OutputDir != "" {
		info, err := os.Stat(c.OutputDir)
		if err != nil {
			return fmt.Errorf("dossier de sortie inutilisable : %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("le dossier de sortie %q n'est pas un dossier", c.OutputDir)
		}
	}
	return nil
}

// ResolveAPIKey returns the key to use and where it came from. Environment
// variables take precedence so the key never needs to touch the disk.
func (c Config) ResolveAPIKey() (key, source string) {
	if v := os.Getenv("TULIPE_API_KEY"); v != "" {
		return v, "TULIPE_API_KEY"
	}
	switch c.Provider {
	case ProviderAnthropic:
		if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
			return v, "ANTHROPIC_API_KEY"
		}
	default:
		if v := os.Getenv("OPENAI_API_KEY"); v != "" {
			return v, "OPENAI_API_KEY"
		}
	}
	if c.APIKey != "" {
		return c.APIKey, Path()
	}
	return "", ""
}

// KeyStatus describes the credential situation in words fit for the interface,
// without ever revealing the key itself.
func (c Config) KeyStatus() string {
	key, source := c.ResolveAPIKey()
	switch {
	case key != "":
		return "définie (" + source + ")"
	case c.Provider == ProviderAnthropic:
		// The SDK also accepts a profile created by `ant auth login`, so an
		// empty key is not necessarily a problem.
		return "absente ici — le SDK Anthropic cherchera ses propres identifiants"
	default:
		return "absente (beaucoup de services locaux n'en demandent pas)"
	}
}

// NewProvider builds the translation backend described by the configuration.
func (c Config) NewProvider() (llm.Provider, error) {
	key, _ := c.ResolveAPIKey()
	switch c.Provider {
	case ProviderAnthropic:
		return llm.NewAnthropic(llm.AnthropicOptions{
			APIKey:     key,
			BaseURL:    c.BaseURL,
			Model:      c.Model,
			Effort:     c.Effort,
			Structured: c.StructuredOutput,
		}), nil
	case ProviderOpenAI:
		return llm.NewOpenAICompat(llm.OpenAICompatOptions{
			BaseURL:     c.BaseURL,
			APIKey:      key,
			Model:       c.Model,
			Temperature: c.Temperature,
			JSONMode:    c.StructuredOutput,
		})
	default:
		return nil, fmt.Errorf("fournisseur inconnu %q ; attendu %q ou %q", c.Provider, ProviderAnthropic, ProviderOpenAI)
	}
}

// Recipe lists the settings that change what a translation comes out as. It is
// what the resume cache is keyed on, so that changing a glossary, a language or
// a model never reuses work done under the previous settings.
func (c Config) Recipe() translate.Recipe {
	effort := c.Effort
	if c.Provider != ProviderAnthropic {
		// Effort is meaningless for the other backend; including it would
		// invalidate caches for no reason.
		effort = ""
	}
	return translate.Recipe{
		Provider:       c.Provider,
		Model:          c.Model,
		Effort:         effort,
		TargetLanguage: c.TargetLanguage,
		TargetCode:     c.TargetCode,
		SourceLanguage: c.SourceLanguage,
		Glossary:       c.Glossary,
		StyleNotes:     c.StyleNotes,
	}
}

// TranslateOptions converts the configuration into translator options.
func (c Config) TranslateOptions() translate.Options {
	return translate.Options{
		TargetLanguage: c.TargetLanguage,
		TargetCode:     c.TargetCode,
		SourceLanguage: c.SourceLanguage,
		Glossary:       c.Glossary,
		StyleNotes:     c.StyleNotes,
		ChunkChars:     c.ChunkChars,
		MaxSegments:    c.MaxSegments,
		MaxTokens:      c.MaxTokens,
		Attempts:       c.Attempts,
		RetryBase:      2 * time.Second,
		ContextChars:   c.ContextChars,
		RequestTimeout: time.Duration(c.TimeoutSeconds) * time.Second,
	}
}

// Language is an entry of the target-language picker.
type Language struct {
	Name string
	Code string
}

// Languages are the ready-made choices offered by the interface. Any other
// language can be typed in by hand.
var Languages = []Language{
	{"français", "fr"},
	{"english", "en"},
	{"español", "es"},
	{"deutsch", "de"},
	{"italiano", "it"},
	{"português (Brasil)", "pt-BR"},
	{"nederlands", "nl"},
	{"polski", "pl"},
	{"русский", "ru"},
	{"日本語", "ja"},
	{"中文 (简体)", "zh-Hans"},
	{"العربية", "ar"},
}

// LookupLanguage finds the BCP 47 tag of a language named in the picker.
func LookupLanguage(name string) (Language, bool) {
	for _, l := range Languages {
		if strings.EqualFold(l.Name, name) {
			return l, true
		}
	}
	return Language{}, false
}

// Efforts are the effort levels accepted by the Anthropic backend, cheapest
// first.
var Efforts = []string{"low", "medium", "high", "xhigh", "max"}

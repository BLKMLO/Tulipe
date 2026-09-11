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
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/blkmlo/tulipe/internal/i18n"
	"github.com/blkmlo/tulipe/internal/llm"
	"github.com/blkmlo/tulipe/internal/translate"
)

// Provider identifiers. A provider is a preset from llm.Presets; these two are
// named because the rest of the code branches on them.
const (
	ProviderAnthropic = llm.KindAnthropic
	ProviderOpenAI    = llm.KindOpenAI
)

// Format is how the translated book is written out.
const (
	FormatEPUB = "epub"
	FormatText = "txt"
)

// Formats are the accepted output formats.
var Formats = []string{FormatEPUB, FormatText}

// Preset returns the catalogue entry for the configured provider.
func (c Config) Preset() (llm.Preset, bool) { return llm.LookupPreset(c.Provider) }

// Kind is the protocol the configured provider speaks.
func (c Config) Kind() string {
	if p, ok := c.Preset(); ok {
		return p.Kind
	}
	return ""
}

// Endpoint is the base URL actually used: the one configured, or the preset's
// when none was set.
func (c Config) Endpoint() string {
	if strings.TrimSpace(c.BaseURL) != "" {
		return strings.TrimSpace(c.BaseURL)
	}
	if p, ok := c.Preset(); ok {
		return p.BaseURL
	}
	return ""
}

// Config is everything Tulipe remembers between runs.
type Config struct {
	// Language is the interface language: a code from i18n.Locales. It has
	// nothing to do with TargetLanguage, which is what books are translated
	// into; someone may well read a French interface while translating into
	// Japanese.
	Language string `json:"language,omitempty"`

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
	SourceCode     string `json:"source_code,omitempty"`
	Glossary       string `json:"glossary,omitempty"`
	StyleNotes     string `json:"style_notes,omitempty"`
	// About describes the book in one sentence, to calibrate the translation.
	About string `json:"about,omitempty"`
	// TranslateTitles decides whether chapter headings and the table of
	// contents are translated along with the prose. Some readers keep the
	// original titles so the book still matches its reviews and its index.
	//
	// It is stored the positive way round, which is how it is shown; the
	// translator takes the opposite, KeepOriginalTitles, so that its own zero
	// value keeps translating everything. Load starts from Default(), so a
	// configuration file written before this setting existed keeps the
	// behaviour it had.
	TranslateTitles bool `json:"translate_titles"`

	ChunkChars  int   `json:"chunk_chars"`
	MaxSegments int   `json:"max_segments"`
	MaxTokens   int64 `json:"max_tokens"`
	Attempts    int   `json:"attempts"`
	// ContextChars is how much of the previous passage the model is shown for
	// continuity of tone and vocabulary. Zero means none — unlike the
	// translator's own field, where zero means "not set", this one carries
	// what the reader chose, and "none" is one of the answers.
	ContextChars int `json:"context_chars"`
	// TimeoutSeconds caps one call to the model.
	TimeoutSeconds int `json:"timeout_seconds"`

	// SalvagePass retries, once the book is finished, the passages the first
	// pass could not translate: smaller batches, a prompt that says as much.
	// It is not part of Recipe — it changes how many passages come back, not
	// what any one of them says, so turning it on must not throw away a cache.
	SalvagePass bool `json:"salvage_pass"`

	OutputDir string `json:"output_dir,omitempty"`
	// Format is "epub" or "txt".
	Format string `json:"format"`
	Resume bool   `json:"resume"`
}

// Default is the configuration a first run starts from.
func Default() Config {
	target := defaultTarget()
	return Config{
		Language:         i18n.DefaultLocale,
		Provider:         ProviderAnthropic,
		Model:            llm.DefaultAnthropicModel,
		Effort:           "medium",
		StructuredOutput: true,
		TranslateTitles:  true,
		SalvagePass:      true,
		TargetLanguage:   target.Name,
		TargetCode:       target.Code,
		ChunkChars:       4000,
		MaxSegments:      40,
		MaxTokens:        16000,
		Attempts:         4,
		ContextChars:     400,
		TimeoutSeconds:   300,
		Format:           FormatEPUB,
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
	if !i18n.Known(c.Language) {
		c.Language = i18n.DefaultLocale
	}
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
	if c.ContextChars < 0 {
		// Only a nonsensical value is repaired. Zero is a choice, and
		// overwriting it was why the setting accepted "0" and went on showing
		// the model four hundred characters anyway.
		c.ContextChars = d.ContextChars
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = d.TimeoutSeconds
	}
	if c.Format == "" {
		c.Format = FormatEPUB
	}
	return c
}

// languageTag matches a BCP 47 tag closely enough to keep nonsense out of the
// book's metadata, without pretending to be a full parser.
var languageTag = regexp.MustCompile(`^[A-Za-z0-9]{1,8}(-[A-Za-z0-9]{1,8})*$`)

// Validate refuses a configuration that cannot do what it says. Silently
// falling back to a default would leave the user believing a setting took
// effect when it did not.
func (c Config) Validate() error {
	preset, known := c.Preset()
	if !known {
		return fmt.Errorf(i18n.T("config.err.unknown-provider"), c.Provider, strings.Join(llm.PresetIDs(), ", "))
	}
	if preset.Kind != llm.KindDeepL && strings.TrimSpace(c.Model) == "" {
		return errors.New(i18n.T("config.err.no-model"))
	}
	if strings.TrimSpace(c.TargetLanguage) == "" {
		return errors.New(i18n.T("config.err.no-target-language"))
	}
	endpoint := c.Endpoint()
	if preset.Kind == llm.KindOpenAI && strings.TrimSpace(endpoint) == "" {
		return errors.New(i18n.T("config.err.needs-base-url"))
	}
	if strings.Contains(endpoint, "{") {
		return fmt.Errorf(i18n.T("config.err.base-url-blank"), endpoint)
	}
	if preset.Kind == llm.KindDeepL && strings.TrimSpace(c.TargetCode) == "" {
		return errors.New(i18n.T("config.err.deepl-needs-code"))
	}
	if endpoint != "" {
		u, err := url.Parse(endpoint)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf(i18n.T("config.err.invalid-base-url"), endpoint)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf(i18n.T("config.err.bad-scheme"), u.Scheme)
		}
	}
	if c.Effort != "" && preset.Kind == llm.KindAnthropic && !slices.Contains(Efforts, c.Effort) {
		return fmt.Errorf(i18n.T("config.err.unknown-effort"), c.Effort, strings.Join(Efforts, ", "))
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
			return fmt.Errorf(i18n.T("config.err.below-minimum"), f.name, f.v, f.min)
		}
	}
	// These go straight into every request. A caller is free to be generous,
	// but an absurd value deserves a clear refusal here rather than a cryptic
	// error from the service half a book later.
	for _, field := range []struct {
		name  string
		value string
		max   int
	}{
		{"--to", c.TargetLanguage, 100},
		{"--from", c.SourceLanguage, 100},
		{"--about", c.About, 2000},
		{"--style", c.StyleNotes, 10000},
		{i18n.T("config.field.glossary"), c.Glossary, 200000},
	} {
		if n := len([]rune(field.value)); n > field.max {
			return fmt.Errorf(i18n.T("config.err.too-long"), field.name, n, field.max)
		}
	}
	for _, tag := range []struct{ name, value string }{
		{"--code", c.TargetCode},
		{"--from-code", c.SourceCode},
	} {
		if tag.value != "" && !languageTag.MatchString(tag.value) {
			return fmt.Errorf(i18n.T("config.err.bad-language-tag"), tag.name, tag.value)
		}
	}
	if c.Language != "" && !i18n.Known(c.Language) {
		return fmt.Errorf(i18n.T("config.err.unknown-locale"), c.Language, strings.Join(i18n.Locales, ", "))
	}
	if !slices.Contains(Formats, c.Format) {
		return fmt.Errorf(i18n.T("config.err.unknown-format"), c.Format, strings.Join(Formats, ", "))
	}
	if c.OutputDir != "" {
		info, err := os.Stat(c.OutputDir)
		if err != nil {
			return fmt.Errorf(i18n.T("config.err.output-dir-unusable"), err)
		}
		if !info.IsDir() {
			return fmt.Errorf(i18n.T("config.err.output-dir-not-dir"), c.OutputDir)
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
	if preset, ok := c.Preset(); ok {
		for _, name := range preset.KeyEnv {
			if v := os.Getenv(name); v != "" {
				return v, name
			}
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
	if key != "" {
		return i18n.T("config.key.set", source)
	}
	preset, ok := c.Preset()
	switch {
	case ok && preset.Kind == llm.KindAnthropic:
		// The SDK also accepts a profile created by `ant auth login`, so an
		// empty key is not necessarily a problem.
		return i18n.T("config.key.anthropic-sdk")
	case ok && preset.NoKey:
		return i18n.T("config.key.not-needed")
	case ok && len(preset.KeyEnv) > 0:
		return i18n.T("config.key.expected-in", strings.Join(preset.KeyEnv, i18n.T("cli.providers.or")))
	default:
		return i18n.T("config.key.missing")
	}
}

// NewProvider builds the translation backend described by the configuration.
func (c Config) NewProvider() (llm.Provider, error) {
	key, _ := c.ResolveAPIKey()
	preset, ok := c.Preset()
	if !ok {
		return nil, fmt.Errorf(i18n.T("config.err.unknown-provider"), c.Provider, strings.Join(llm.PresetIDs(), ", "))
	}
	timeout := time.Duration(c.TimeoutSeconds) * time.Second
	switch preset.Kind {
	case llm.KindAnthropic:
		return llm.NewAnthropic(llm.AnthropicOptions{
			APIKey:     key,
			BaseURL:    c.BaseURL,
			Model:      c.Model,
			Effort:     c.Effort,
			Structured: c.StructuredOutput,
		}), nil
	case llm.KindOpenAI:
		return llm.NewOpenAICompat(llm.OpenAICompatOptions{
			BaseURL:     c.Endpoint(),
			APIKey:      key,
			Model:       c.Model,
			Temperature: c.Temperature,
			JSONMode:    c.StructuredOutput,
			Timeout:     timeout,
		})
	case llm.KindDeepL:
		return llm.NewDeepL(llm.DeepLOptions{
			APIKey:  key,
			BaseURL: c.BaseURL,
			Timeout: timeout,
		})
	default:
		return nil, fmt.Errorf(i18n.T("config.err.unknown-protocol"), preset.Kind, c.Provider)
	}
}

// Recipe lists the settings that change what a translation comes out as. It is
// what the resume cache is keyed on, so that changing a glossary, a language or
// a model never reuses work done under the previous settings.
func (c Config) Recipe() translate.Recipe {
	effort := c.Effort
	if c.Kind() != llm.KindAnthropic {
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
		SourceCode:     c.SourceCode,
		Glossary:       c.Glossary,
		StyleNotes:     c.StyleNotes,
		About:          c.About,

		KeepOriginalTitles: !c.TranslateTitles,
		ContextChars:       c.ContextChars,
	}
}

// continuity converts the reader's answer into the translator's convention.
// The two disagree about zero on purpose: in Config it means "none", in
// Options it means "nothing was set, use the default". Negative is how Options
// spells none.
func continuity(configured int) int {
	if configured <= 0 {
		return -1
	}
	return configured
}

// TranslateOptions converts the configuration into translator options.
func (c Config) TranslateOptions() translate.Options {
	return translate.Options{
		TargetLanguage: c.TargetLanguage,
		TargetCode:     c.TargetCode,
		SourceLanguage: c.SourceLanguage,
		Glossary:       c.Glossary,
		StyleNotes:     c.StyleNotes,
		About:          c.About,
		ChunkChars:     c.ChunkChars,

		KeepOriginalTitles: !c.TranslateTitles,
		MaxSegments:        c.MaxSegments,
		MaxTokens:          c.MaxTokens,
		Attempts:           c.Attempts,
		RetryBase:          2 * time.Second,
		ContextChars:       continuity(c.ContextChars),
		RequestTimeout:     time.Duration(c.TimeoutSeconds) * time.Second,
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

// defaultTarget is the language a first run translates into. It follows the
// language the interface starts in — someone reading an English interface most
// likely wants an English book — and is a setting of its own from then on:
// changing the interface language never touches it again.
func defaultTarget() Language {
	for _, l := range Languages {
		if l.Code == i18n.DefaultLocale {
			return l
		}
	}
	return Languages[0]
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

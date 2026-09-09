package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blkmlo/tulipe/internal/i18n"
	"github.com/blkmlo/tulipe/internal/llm"
)

func TestValidateRejectsWhatItCannotDo(t *testing.T) {
	cases := []struct {
		name  string
		tweak func(*Config)
		want  string
	}{
		{"unknown provider", func(c *Config) { c.Provider = "telepathie" }, "unknown provider"},
		{"unknown format", func(c *Config) { c.Format = "pdf" }, "unknown output format"},
		{"url to complete", func(c *Config) { c.Provider = "cloudflare" }, "field to fill in"},
		{"deepl without a code", func(c *Config) { c.Provider = "deepl"; c.TargetCode = "" }, "target language code"},
		{"empty model", func(c *Config) { c.Model = "  " }, "no model"},
		{"empty language", func(c *Config) { c.TargetLanguage = "" }, "no target language"},
		{"invented effort", func(c *Config) { c.Effort = "enorme" }, "unknown effort"},
		{"negative chunk", func(c *Config) { c.ChunkChars = -50 }, "--chunk"},
		{"zero chunk", func(c *Config) { c.ChunkChars = 0 }, "--chunk"},
		{"zero segments", func(c *Config) { c.MaxSegments = 0 }, "--max-segments"},
		{"zero tokens", func(c *Config) { c.MaxTokens = 0 }, "--max-tokens"},
		{"zero attempts", func(c *Config) { c.Attempts = 0 }, "--attempts"},
		{"negative continuity", func(c *Config) { c.ContextChars = -1 }, "--context"},
		{"url without a scheme", func(c *Config) { c.BaseURL = "localhost:11434" }, "invalid base URL"},
		{"ftp url", func(c *Config) { c.BaseURL = "ftp://x/v1" }, "only http and https"},
		{"openai without a url", func(c *Config) { c.Provider = ProviderOpenAI; c.BaseURL = "" }, "base URL"},
		{"missing output folder", func(c *Config) { c.OutputDir = "/nexiste/pas/du/tout" }, "output folder"},
		{"unknown interface language", func(c *Config) { c.Language = "klingon" }, "unknown interface language"},
		{"unknown interface language", func(c *Config) { c.Language = "kl" }, "unknown interface language"},
	}
	for _, c := range cases {
		cfg := Default()
		c.tweak(&cfg)
		err := cfg.Validate()
		if err == nil {
			t.Errorf("%s: Validate() = nil, want an error", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: Validate() = %q, want it to mention %q", c.name, err, c.want)
		}
	}
}

func TestValidateAcceptsSaneConfigurations(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Errorf("the default configuration must be valid: %v", err)
	}
	local := Default()
	local.Provider = ProviderOpenAI
	local.BaseURL = "http://localhost:11434/v1"
	local.Model = "qwen"
	local.Effort = "enorme" // meaningless here, so not checked
	if err := local.Validate(); err != nil {
		t.Errorf("a local OpenAI-compatible configuration must be valid: %v", err)
	}
	withDir := Default()
	withDir.OutputDir = t.TempDir()
	if err := withDir.Validate(); err != nil {
		t.Errorf("an existing output directory must be accepted: %v", err)
	}
}

func TestValidateRejectsAFileAsOutputDirectory(t *testing.T) {
	f := filepath.Join(t.TempDir(), "pas-un-dossier")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	cfg.OutputDir = f
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "is not a folder") {
		t.Errorf("Validate() = %v, want a complaint that it is not a directory", err)
	}
}

func TestAPIKeyIsNeverRevealed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TULIPE_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("TULIPE_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	cfg := Default()
	cfg.APIKey = "sk-ant-secret-value"
	if status := cfg.KeyStatus(); strings.Contains(status, "secret") {
		t.Errorf("KeyStatus leaks the key: %q", status)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("the configuration file is %o, want 600 — it may hold a key", perm)
	}
}

func TestEnvironmentKeyWinsOverTheFile(t *testing.T) {
	t.Setenv("TULIPE_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "depuis-env")
	cfg := Default()
	cfg.APIKey = "depuis-fichier"
	key, source := cfg.ResolveAPIKey()
	if key != "depuis-env" || source != "ANTHROPIC_API_KEY" {
		t.Errorf("ResolveAPIKey = %q from %q, want the environment to win", key, source)
	}
	t.Setenv("TULIPE_API_KEY", "prioritaire")
	if key, source := cfg.ResolveAPIKey(); key != "prioritaire" || source != "TULIPE_API_KEY" {
		t.Errorf("ResolveAPIKey = %q from %q, want TULIPE_API_KEY to win", key, source)
	}
}

func TestRecipeIgnoresEffortForNonAnthropicProviders(t *testing.T) {
	cfg := Default()
	cfg.Provider = ProviderOpenAI
	cfg.BaseURL = "http://localhost:1234/v1"
	cfg.Effort = "max"
	if r := cfg.Recipe(); r.Effort != "" {
		t.Errorf("Recipe().Effort = %q, want it dropped for a provider that ignores it", r.Effort)
	}
	cfg.Provider = ProviderAnthropic
	if r := cfg.Recipe(); r.Effort != "max" {
		t.Errorf("Recipe().Effort = %q, want max", r.Effort)
	}
}

func TestLoadSurvivesACorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv("TULIPE_CONFIG", path)
	if err := os.WriteFile(path, []byte("{ ceci n'est pas du json"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err == nil {
		t.Error("Load() must report that the file could not be read")
	}
	if cfg.Model != Default().Model {
		t.Errorf("Load() returned %q, want the defaults so the run can continue", cfg.Model)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("the fallback configuration must be usable: %v", err)
	}
}

func TestLoadFillsMissingFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv("TULIPE_CONFIG", path)
	if err := os.WriteFile(path, []byte(`{"provider":"anthropic","model":"m"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChunkChars != Default().ChunkChars || cfg.TargetLanguage == "" {
		t.Errorf("a partial file must be completed with the defaults: %+v", cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("the completed configuration must be valid: %v", err)
	}
}

func TestEveryPresetIsUsableOutOfTheBox(t *testing.T) {
	t.Setenv("TULIPE_API_KEY", "")
	for _, p := range llm.Presets() {
		cfg := Default()
		cfg.Provider = p.ID
		cfg.Model = "un-modele"
		err := cfg.Validate()

		switch {
		case p.NeedsBaseURL() && p.Kind == llm.KindOpenAI:
			// These two ask the user for the endpoint, and must say so.
			if err == nil {
				t.Errorf("%s: Validate() = nil, want a complaint about the base URL", p.ID)
			}
		default:
			if err != nil {
				t.Errorf("%s: Validate() = %v, want nil", p.ID, err)
			}
		}
		if cfg.Kind() != p.Kind {
			t.Errorf("%s: Kind() = %q, want %q", p.ID, cfg.Kind(), p.Kind)
		}
	}
}

func TestPresetSuppliesTheEndpointAndKeyVariable(t *testing.T) {
	t.Setenv("TULIPE_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "clef-groq")

	cfg := Default()
	cfg.Provider = "groq"
	cfg.Model = "un-modele"
	if got := cfg.Endpoint(); got == "" || !strings.HasPrefix(got, "https://") {
		t.Errorf("Endpoint() = %q, want the preset's URL", got)
	}
	key, source := cfg.ResolveAPIKey()
	if key != "clef-groq" || source != "GROQ_API_KEY" {
		t.Errorf("ResolveAPIKey = %q from %q, want the preset's variable", key, source)
	}
	if status := cfg.KeyStatus(); strings.Contains(status, "clef-groq") {
		t.Errorf("KeyStatus leaks the key: %q", status)
	}

	// A hand-set URL overrides the preset's.
	cfg.BaseURL = "http://localhost:9999/v1"
	if cfg.Endpoint() != "http://localhost:9999/v1" {
		t.Errorf("Endpoint() = %q, want the configured URL to win", cfg.Endpoint())
	}
}

func TestKeyStatusNamesTheExpectedVariable(t *testing.T) {
	t.Setenv("TULIPE_API_KEY", "")
	t.Setenv("MISTRAL_API_KEY", "")
	cfg := Default()
	cfg.Provider = "mistral"
	if status := cfg.KeyStatus(); !strings.Contains(status, "MISTRAL_API_KEY") {
		t.Errorf("KeyStatus = %q, want it to name the variable to set", status)
	}
	cfg.Provider = "ollama"
	if status := cfg.KeyStatus(); !strings.Contains(status, "not needed") {
		t.Errorf("KeyStatus = %q, want it to say no key is needed locally", status)
	}
}

func TestNewProviderBuildsEachKind(t *testing.T) {
	t.Setenv("TULIPE_API_KEY", "une-clef")
	for _, id := range []string{"anthropic", "groq", "deepl", "ollama"} {
		cfg := Default()
		cfg.Provider = id
		cfg.Model = "un-modele"
		p, err := cfg.NewProvider()
		if err != nil {
			t.Errorf("%s: NewProvider = %v", id, err)
			continue
		}
		if p.ID() == "" {
			t.Errorf("%s: the provider reports no identifier", id)
		}
	}
}

func TestDeepLIsRecognisedAsADirectTranslator(t *testing.T) {
	t.Setenv("TULIPE_API_KEY", "une-clef:fx")
	cfg := Default()
	cfg.Provider = "deepl"
	p, err := cfg.NewProvider()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(llm.DirectTranslator); !ok {
		t.Error("DeepL must be reached through the direct-translation path, not through prompting")
	}
}

func TestValidateRejectsMalformedLanguageTags(t *testing.T) {
	// The code lands in the book's metadata; nonsense there makes a file no
	// reader will open.
	for _, bad := range []string{"fr<script>", "fr&amp;", "f r", "fr_FR!", "-fr", "fr-", "fr--FR", "«fr»"} {
		cfg := Default()
		cfg.TargetCode = bad
		if err := cfg.Validate(); err == nil {
			t.Errorf("--code %q accepté, want a refusal", bad)
		}
	}
	for _, good := range []string{"fr", "en-GB", "pt-BR", "zh-Hans", "sr-Latn-RS", ""} {
		cfg := Default()
		cfg.TargetCode = good
		if err := cfg.Validate(); err != nil {
			t.Errorf("--code %q refusé : %v", good, err)
		}
	}
	// The source side is checked the same way.
	cfg := Default()
	cfg.SourceCode = "en<>"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "--from-code") {
		t.Errorf("Validate = %v, want a complaint about --from-code", err)
	}
}

// TestValidateNeverPanics throws every awkward value at the validator at once:
// a user can combine them freely, and no combination may bring the program
// down.
func TestValidateNeverPanics(t *testing.T) {
	odd := []string{"", " ", "\n", "\x00", strings.Repeat("x", 5000), "«»", "../../etc/passwd",
		"http://", "://x", "%%", "-1", "0", "NaN", "🌷"}
	numbers := []int{-1 << 40, -1, 0, 1, 1 << 40}

	for _, s := range odd {
		for _, n := range numbers {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("chaîne %q, nombre %d : %v", s, n, r)
					}
				}()
				cfg := Default()
				cfg.Provider, cfg.Model, cfg.BaseURL = s, s, s
				cfg.Effort, cfg.Format, cfg.OutputDir = s, s, s
				cfg.TargetLanguage, cfg.TargetCode, cfg.SourceCode = s, s, s
				cfg.Glossary, cfg.StyleNotes, cfg.About, cfg.APIKey = s, s, s, s
				cfg.ChunkChars, cfg.MaxSegments, cfg.Attempts = n, n, n
				cfg.ContextChars, cfg.TimeoutSeconds = n, n
				cfg.MaxTokens = int64(n)

				_ = cfg.Validate()
				_ = cfg.Kind()
				_ = cfg.Endpoint()
				_ = cfg.KeyStatus()
				_ = cfg.Recipe()
				_ = cfg.TranslateOptions()
				_, _ = cfg.NewProvider()
				_ = cfg.normalise()
			}()
		}
	}
}

func TestNormaliseAlwaysProducesAValidConfiguration(t *testing.T) {
	// Whatever a hand-edited file holds, the defaults must fill the gaps.
	for _, n := range []int{-1 << 40, -1, 0} {
		cfg := Config{ChunkChars: n, MaxSegments: n, Attempts: n, ContextChars: n,
			TimeoutSeconds: n, MaxTokens: int64(n)}.normalise()
		if err := cfg.Validate(); err != nil {
			t.Errorf("une configuration normalisée depuis %d reste invalide : %v", n, err)
		}
	}
}

func TestValidateCapsFreeTextFields(t *testing.T) {
	// Every one of these is copied into each request; an absurd length must be
	// named here rather than surface as a service error mid-book.
	cases := map[string]func(*Config, string){
		"--to":         func(c *Config, v string) { c.TargetLanguage = v },
		"--from":       func(c *Config, v string) { c.SourceLanguage = v },
		"--about":      func(c *Config, v string) { c.About = v },
		"--style":      func(c *Config, v string) { c.StyleNotes = v },
		"the glossary": func(c *Config, v string) { c.Glossary = v },
	}
	for name, set := range cases {
		cfg := Default()
		set(&cfg, strings.Repeat("é", 300000))
		err := cfg.Validate()
		if err == nil {
			t.Errorf("%s: a 300,000-character value is accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), name) {
			t.Errorf("%s: the message does not name the field: %v", name, err)
		}
	}

	// Ordinary values stay accepted.
	cfg := Default()
	cfg.About = "un roman noir new-yorkais des années 1950"
	cfg.Glossary = strings.Repeat("Victory Mansions = Maison de la Victoire\n", 500)
	if err := cfg.Validate(); err != nil {
		t.Errorf("a reasonable configuration is refused: %v", err)
	}
}

// TestInterfaceLanguageDefaultsToEnglishAndKeepsFrench is the whole point of
// the setting: English out of the box, French one choice away, nothing lost.
func TestInterfaceLanguageDefaultsToEnglishAndKeepsFrench(t *testing.T) {
	t.Cleanup(func() { i18n.SetLocale(i18n.DefaultLocale) })

	if got := Default().Language; got != i18n.English {
		t.Errorf("Default().Language = %q, want %q", got, i18n.English)
	}

	dir := t.TempDir()
	t.Setenv("TULIPE_CONFIG", filepath.Join(dir, "config.json"))
	cfg := Default()
	cfg.Language = i18n.French
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	back, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if back.Language != i18n.French {
		t.Fatalf("the saved interface language came back as %q", back.Language)
	}

	i18n.SetLocale(back.Language)
	if err := back.Validate(); err != nil {
		t.Fatalf("a French configuration must stay valid: %v", err)
	}
	if got := (Config{Provider: "inconnu"}).Validate(); got == nil ||
		!strings.Contains(got.Error(), "fournisseur inconnu") {
		t.Errorf("with French chosen, errors must come out in French: %v", got)
	}
}

// TestInterfaceLanguageIsNotTheTargetLanguage guards the separation the two
// settings are built on: reading the menus in one language and translating
// into another has to work, and neither may drag the other along.
func TestInterfaceLanguageIsNotTheTargetLanguage(t *testing.T) {
	cfg := Default()
	cfg.Language = i18n.French
	cfg.TargetLanguage, cfg.TargetCode = "日本語", "ja"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a French interface translating into Japanese must be valid: %v", err)
	}
	if cfg.Recipe().TargetLanguage != "日本語" {
		t.Error("the recipe must carry the target language, not the interface one")
	}

	// The interface language must stay out of the cache key: changing it does
	// not change one word of the translation, and throwing the cache away
	// would make the user pay for the book again.
	other := cfg
	other.Language = i18n.English
	if cfg.Recipe() != other.Recipe() {
		t.Error("changing the interface language changed the resume-cache recipe")
	}
}

// TestAnUnknownInterfaceLanguageFallsBack keeps a hand-edited configuration
// file from leaving the program with no strings at all.
func TestAnUnknownInterfaceLanguageFallsBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv("TULIPE_CONFIG", path)
	if err := os.WriteFile(path, []byte(`{"language":"klingon"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Language != i18n.DefaultLocale {
		t.Errorf("Language = %q, want a fallback to %q", cfg.Language, i18n.DefaultLocale)
	}
}

func TestTitlesAreTranslatedUnlessAsked(t *testing.T) {
	cfg := Default()
	if !cfg.TranslateTitles {
		t.Fatal("a first run must translate titles like everything else")
	}
	if cfg.TranslateOptions().KeepOriginalTitles {
		t.Error("the two settings are opposites; translating titles means not keeping the originals")
	}
	if cfg.Recipe().KeepOriginalTitles {
		t.Error("the recipe must carry the same answer as the options")
	}

	cfg.TranslateTitles = false
	if !cfg.TranslateOptions().KeepOriginalTitles || !cfg.Recipe().KeepOriginalTitles {
		t.Error("turning the setting off must reach both the options and the cache key")
	}
}

func TestConfigWrittenBeforeTheTitleSettingStillTranslatesTitles(t *testing.T) {
	// A file written by an earlier version has no "translate_titles" key.
	// Load starts from Default(), so the absent key must leave it on rather
	// than silently stop translating headings.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"provider":"anthropic","model":"m"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TULIPE_CONFIG", path)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.TranslateTitles {
		t.Error("an older configuration file must keep the behaviour it had")
	}
}

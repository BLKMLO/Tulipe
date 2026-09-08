package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRejectsWhatItCannotDo(t *testing.T) {
	cases := []struct {
		name  string
		tweak func(*Config)
		want  string
	}{
		{"fournisseur inconnu", func(c *Config) { c.Provider = "mistral" }, "fournisseur inconnu"},
		{"modèle vide", func(c *Config) { c.Model = "  " }, "aucun modèle"},
		{"langue vide", func(c *Config) { c.TargetLanguage = "" }, "aucune langue"},
		{"effort inventé", func(c *Config) { c.Effort = "enorme" }, "effort inconnu"},
		{"chunk négatif", func(c *Config) { c.ChunkChars = -50 }, "--chunk"},
		{"chunk nul", func(c *Config) { c.ChunkChars = 0 }, "--chunk"},
		{"segments nuls", func(c *Config) { c.MaxSegments = 0 }, "--max-segments"},
		{"jetons nuls", func(c *Config) { c.MaxTokens = 0 }, "--max-tokens"},
		{"tentatives nulles", func(c *Config) { c.Attempts = 0 }, "--attempts"},
		{"continuité négative", func(c *Config) { c.ContextChars = -1 }, "--context"},
		{"url sans schéma", func(c *Config) { c.BaseURL = "localhost:11434" }, "URL de base invalide"},
		{"url en ftp", func(c *Config) { c.BaseURL = "ftp://x/v1" }, "seuls http et https"},
		{"openai sans url", func(c *Config) { c.Provider = ProviderOpenAI; c.BaseURL = "" }, "URL de base"},
		{"sortie inexistante", func(c *Config) { c.OutputDir = "/nexiste/pas/du/tout" }, "dossier de sortie"},
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
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "n'est pas un dossier") {
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

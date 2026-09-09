package llm

import (
	"sort"
	"strings"

	"github.com/blkmlo/tulipe/internal/i18n"
)

// Kind is the protocol a backend speaks. Several services share one kind, which
// is why a preset carries both.
const (
	// KindAnthropic is the Claude Messages API, through the official SDK.
	KindAnthropic = "anthropic"
	// KindOpenAI is the /chat/completions protocol, spoken by most services.
	KindOpenAI = "openai-compatible"
	// KindDeepL is DeepL's translation API, which takes segments directly
	// instead of being prompted.
	KindDeepL = "deepl"
)

// Preset describes a service Tulipe knows how to reach. It carries no quota or
// pricing figures on purpose: those change often, and a stale number printed as
// fact would be worse than none. What a service grants is on its own site.
type Preset struct {
	// ID is what the configuration stores.
	ID string
	// Name is what the interface shows. Service names are proper nouns and
	// stay as they are; the one entry that names no particular service holds
	// an i18n key instead, which DisplayName resolves.
	Name string
	// Kind is the protocol, one of the Kind constants.
	Kind string
	// BaseURL is the endpoint. A preset whose URL contains a {placeholder}
	// needs the user to fill it in.
	BaseURL string
	// KeyEnv lists the environment variables checked for a key, in order.
	KeyEnv []string
	// NoKey marks a service that usually needs no credentials, such as a model
	// running on the machine.
	NoKey bool
	// FreeTier marks a service that advertises a tier reachable without a
	// payment card. What that tier allows is for its own site to say.
	FreeTier bool
	// Note is an i18n key for the short line shown next to the entry. It is a
	// key rather than a sentence so that the catalogue carries no language of
	// its own.
	Note string
	// Docs is where the user goes to get a key or check the terms.
	Docs string
}

// DisplayName is the name to print, in the interface language. A name that is
// not a known key — every real service — comes back untouched.
func (p Preset) DisplayName() string { return i18n.T(p.Name) }

// NoteText is the note to print, in the interface language.
func (p Preset) NoteText() string {
	if p.Note == "" {
		return ""
	}
	return i18n.T(p.Note)
}

// NeedsBaseURL reports whether the user still has to complete the endpoint.
func (p Preset) NeedsBaseURL() bool {
	return p.BaseURL == "" || strings.Contains(p.BaseURL, "{")
}

// presets is the catalogue. Endpoints are the ones each service documents for
// its public API; they are editable in the settings, so a service that moves
// its URL is a one-field fix rather than a new release.
var presets = []Preset{
	{
		ID: KindAnthropic, Name: "Anthropic (Claude)", Kind: KindAnthropic,
		KeyEnv: []string{"ANTHROPIC_API_KEY"},
		Note:   "preset.note.anthropic",
		Docs:   "https://platform.claude.com/",
	},
	{
		ID: "gemini", Name: "Google AI Studio (Gemini)", Kind: KindOpenAI,
		BaseURL:  "https://generativelanguage.googleapis.com/v1beta/openai/",
		KeyEnv:   []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"},
		FreeTier: true,
		Note:     "preset.note.gemini",
		Docs:     "https://aistudio.google.com/apikey",
	},
	{
		ID: "mistral", Name: "Mistral", Kind: KindOpenAI,
		BaseURL:  "https://api.mistral.ai/v1",
		KeyEnv:   []string{"MISTRAL_API_KEY"},
		FreeTier: true,
		Docs:     "https://console.mistral.ai/",
	},
	{
		ID: "groq", Name: "Groq", Kind: KindOpenAI,
		BaseURL:  "https://api.groq.com/openai/v1",
		KeyEnv:   []string{"GROQ_API_KEY"},
		FreeTier: true,
		Note:     "preset.note.groq",
		Docs:     "https://console.groq.com/keys",
	},
	{
		ID: "cerebras", Name: "Cerebras", Kind: KindOpenAI,
		BaseURL:  "https://api.cerebras.ai/v1",
		KeyEnv:   []string{"CEREBRAS_API_KEY"},
		FreeTier: true,
		Note:     "preset.note.cerebras",
		Docs:     "https://cloud.cerebras.ai/",
	},
	{
		ID: "nvidia", Name: "NVIDIA NIM", Kind: KindOpenAI,
		BaseURL:  "https://integrate.api.nvidia.com/v1",
		KeyEnv:   []string{"NVIDIA_API_KEY"},
		FreeTier: true,
		Docs:     "https://build.nvidia.com/",
	},
	{
		ID: "cohere", Name: "Cohere", Kind: KindOpenAI,
		BaseURL:  "https://api.cohere.ai/compatibility/v1",
		KeyEnv:   []string{"COHERE_API_KEY"},
		FreeTier: true,
		Note:     "preset.note.cohere",
		Docs:     "https://dashboard.cohere.com/api-keys",
	},
	{
		ID: "cloudflare", Name: "Cloudflare Workers AI", Kind: KindOpenAI,
		BaseURL:  "https://api.cloudflare.com/client/v4/accounts/{account_id}/ai/v1",
		KeyEnv:   []string{"CLOUDFLARE_API_TOKEN", "CLOUDFLARE_API_KEY"},
		FreeTier: true,
		Note:     "preset.note.cloudflare",
		Docs:     "https://dash.cloudflare.com/",
	},
	{
		ID: "deepl", Name: "DeepL", Kind: KindDeepL,
		BaseURL: "https://api-free.deepl.com/v2",
		KeyEnv:  []string{"DEEPL_API_KEY", "DEEPL_AUTH_KEY"},
		Note:    "preset.note.deepl",
		Docs:    "https://www.deepl.com/pro-api",
	},
	{
		ID: "openai", Name: "OpenAI", Kind: KindOpenAI,
		BaseURL: "https://api.openai.com/v1",
		KeyEnv:  []string{"OPENAI_API_KEY"},
		Docs:    "https://platform.openai.com/api-keys",
	},
	{
		ID: "openrouter", Name: "OpenRouter", Kind: KindOpenAI,
		BaseURL: "https://openrouter.ai/api/v1",
		KeyEnv:  []string{"OPENROUTER_API_KEY"},
		Note:    "preset.note.openrouter",
		Docs:    "https://openrouter.ai/keys",
	},
	{
		ID: "ollama", Name: "Ollama (local)", Kind: KindOpenAI,
		BaseURL: "http://localhost:11434/v1",
		NoKey:   true,
		Note:    "preset.note.local",
		Docs:    "https://ollama.com/",
	},
	{
		ID: "lmstudio", Name: "LM Studio (local)", Kind: KindOpenAI,
		BaseURL: "http://localhost:1234/v1",
		NoKey:   true,
		Note:    "preset.note.local",
		Docs:    "https://lmstudio.ai/",
	},
	{
		ID: KindOpenAI, Name: "preset.name.custom", Kind: KindOpenAI,
		KeyEnv: []string{"OPENAI_API_KEY"},
		Note:   "preset.note.custom",
	},
}

// Presets returns the catalogue in display order: services with a free tier
// first, then the rest.
func Presets() []Preset {
	out := make([]Preset, len(presets))
	copy(out, presets)
	sort.SliceStable(out, func(i, j int) bool {
		return rank(out[i]) < rank(out[j])
	})
	return out
}

func rank(p Preset) int {
	switch {
	case p.ID == KindAnthropic:
		return 0
	case p.FreeTier:
		return 1
	case p.NoKey:
		return 2
	default:
		return 3
	}
}

// LookupPreset finds a service by identifier.
func LookupPreset(id string) (Preset, bool) {
	for _, p := range presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// PresetIDs lists every known identifier, for error messages.
func PresetIDs() []string {
	out := make([]string, 0, len(presets))
	for _, p := range Presets() {
		out = append(out, p.ID)
	}
	return out
}

package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListModelsReadsTheServicesOwnList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer clef" {
			t.Errorf("Authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"beta"},{"id":"alpha"},{"id":"alpha"}]}`))
	}))
	defer srv.Close()

	p, err := NewOpenAICompat(OpenAICompatOptions{BaseURL: srv.URL + "/v1", APIKey: "clef", Model: "x"})
	if err != nil {
		t.Fatal(err)
	}
	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if strings.Join(models, ",") != "alpha,beta" {
		t.Errorf("models = %q, want them sorted and deduplicated", models)
	}
}

func TestListModelsAcceptsTheOllamaShape(t *testing.T) {
	// Some local runtimes answer with "models" instead of "data".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen:7b"},{"id":"llama:8b"}]}`))
	}))
	defer srv.Close()
	p, _ := NewOpenAICompat(OpenAICompatOptions{BaseURL: srv.URL + "/v1", Model: "x"})
	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("models = %q, want both entries", models)
	}
}

func TestListModelsReportsFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":{"message":"clé invalide"}}`))
	}))
	defer srv.Close()
	p, _ := NewOpenAICompat(OpenAICompatOptions{BaseURL: srv.URL + "/v1", Model: "x"})
	_, err := p.ListModels(context.Background())
	if !Fatal(err) {
		t.Errorf("err = %v, want a fatal 401", err)
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer empty.Close()
	p2, _ := NewOpenAICompat(OpenAICompatOptions{BaseURL: empty.URL + "/v1", Model: "x"})
	if _, err := p2.ListModels(context.Background()); err == nil {
		t.Error("an empty list must be reported rather than passed off as success")
	}
}

func TestJSONModeIsDroppedWhenTheServiceRefusesIt(t *testing.T) {
	var sawFormat, calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["response_format"]; ok {
			sawFormat++
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"response_format is not supported"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	p, _ := NewOpenAICompat(OpenAICompatOptions{BaseURL: srv.URL + "/v1", Model: "x", JSONMode: true})
	req := Request{User: "salut", Schema: map[string]any{"type": "object"}}
	if _, err := p.Complete(context.Background(), req); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if sawFormat != 1 || calls != 2 {
		t.Fatalf("calls=%d withFormat=%d, want one refused attempt then one without", calls, sawFormat)
	}
	// The refusal must be remembered, not rediscovered on every batch.
	if _, err := p.Complete(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if sawFormat != 1 {
		t.Errorf("response_format was sent again after being refused (%d times)", sawFormat)
	}
}

func TestOpenAICompatNeedsAnEndpointAndAModel(t *testing.T) {
	if _, err := NewOpenAICompat(OpenAICompatOptions{Model: "x"}); err == nil {
		t.Error("a missing base URL must be refused")
	}
	if _, err := NewOpenAICompat(OpenAICompatOptions{BaseURL: "http://x/v1"}); err == nil {
		t.Error("a missing model must be refused")
	}
}

func TestPresetCatalogueIsCoherent(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Presets() {
		if p.ID == "" || p.Name == "" {
			t.Errorf("incomplete preset: %+v", p)
		}
		if seen[p.ID] {
			t.Errorf("duplicate identifier %q", p.ID)
		}
		seen[p.ID] = true
		switch p.Kind {
		case KindAnthropic, KindOpenAI, KindDeepL:
		default:
			t.Errorf("%s: unknown kind %q", p.ID, p.Kind)
		}
		if p.BaseURL != "" && !strings.HasPrefix(p.BaseURL, "http") {
			t.Errorf("%s: base URL %q is not an address", p.ID, p.BaseURL)
		}
		if !p.NoKey && p.Kind != KindAnthropic && len(p.KeyEnv) == 0 && p.ID != KindOpenAI {
			t.Errorf("%s: no environment variable named for its key", p.ID)
		}
		if p.NoKey && len(p.KeyEnv) > 0 {
			t.Errorf("%s: marked as needing no key yet naming one", p.ID)
		}
	}
	if _, ok := LookupPreset("groq"); !ok {
		t.Error("groq is missing from the catalogue")
	}
	if _, ok := LookupPreset("inexistant"); ok {
		t.Error("LookupPreset invented an entry")
	}
	// Anthropic comes first, then the services with a free tier.
	order := PresetIDs()
	if order[0] != KindAnthropic {
		t.Errorf("first entry = %q, want anthropic", order[0])
	}
}

func TestPresetsNeedingAnEndpointAreMarked(t *testing.T) {
	for _, p := range Presets() {
		want := p.BaseURL == "" || strings.Contains(p.BaseURL, "{")
		if p.NeedsBaseURL() != want {
			t.Errorf("%s: NeedsBaseURL() = %v, want %v", p.ID, p.NeedsBaseURL(), want)
		}
	}
	cf, _ := LookupPreset("cloudflare")
	if !cf.NeedsBaseURL() {
		t.Error("the Cloudflare endpoint carries a placeholder and must be flagged")
	}
}

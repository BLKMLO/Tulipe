package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The Anthropic client is the default one, and until now it was the only
// backend with no test at all: the SDK talks a streaming protocol, which looks
// harder to fake than a single JSON body. It is not — AnthropicOptions.BaseURL
// points the SDK anywhere, and the event sequence below is the documented one.

// sse writes one Server-Sent Event in the shape the Messages API uses.
func sse(w http.ResponseWriter, event string, data any) {
	body, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, body)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// streamAnswer plays a complete, well-formed answer: one text block, a stop
// reason, and a token count.
func streamAnswer(w http.ResponseWriter, text, stopReason string) {
	w.Header().Set("Content-Type", "text/event-stream")
	sse(w, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": "msg_test", "type": "message", "role": "assistant",
			"model": "claude-opus-5", "content": []any{},
			"stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens": 120, "output_tokens": 1,
				"cache_read_input_tokens": 40, "cache_creation_input_tokens": 7,
			},
		},
	})
	sse(w, "content_block_start", map[string]any{
		"type": "content_block_start", "index": 0,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
	sse(w, "content_block_delta", map[string]any{
		"type": "content_block_delta", "index": 0,
		"delta": map[string]any{"type": "text_delta", "text": text},
	})
	sse(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	sse(w, "message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": 64},
	})
	sse(w, "message_stop", map[string]any{"type": "message_stop"})
}

// streamRefusal plays the answer a safety classifier produces: HTTP 200, no
// usable content, and a stop reason that says why.
func streamRefusal(w http.ResponseWriter, category, explanation string) {
	w.Header().Set("Content-Type", "text/event-stream")
	sse(w, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": "msg_test", "type": "message", "role": "assistant",
			"model": "claude-opus-5", "content": []any{},
			"stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": 10, "output_tokens": 1},
		},
	})
	sse(w, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":  "refusal",
			"stop_details": map[string]any{"type": "refusal", "category": category, "explanation": explanation},
		},
		"usage": map[string]any{"output_tokens": 2},
	})
	sse(w, "message_stop", map[string]any{"type": "message_stop"})
}

func apiError(w http.ResponseWriter, status int, kind, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"type":"error","error":{"type":%q,"message":%q}}`, kind, message)
}

// anthropicServer runs handler on /v1/messages and returns a client pointed at
// it. Every request body seen is recorded, which is how the invariants below
// are checked.
func anthropicServer(t *testing.T, opts AnthropicOptions, handler func(w http.ResponseWriter, body map[string]any)) (*Anthropic, *[]map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var seen []map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("unreadable request body: %v", err)
		}
		mu.Lock()
		seen = append(seen, body)
		mu.Unlock()
		handler(w, body)
	}))
	t.Cleanup(srv.Close)

	opts.BaseURL = srv.URL
	if opts.APIKey == "" {
		opts.APIKey = "clef-de-test"
	}
	return NewAnthropic(opts), &seen
}

func TestAnthropicReadsAStreamedAnswer(t *testing.T) {
	p, seen := anthropicServer(t, AnthropicOptions{Model: "claude-opus-5"},
		func(w http.ResponseWriter, _ map[string]any) {
			streamAnswer(w, `{"translations":["bonjour"]}`, "end_turn")
		})

	resp, err := p.Complete(context.Background(), Request{User: "traduis", MaxTokens: 500})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != `{"translations":["bonjour"]}` {
		t.Errorf("Text = %q", resp.Text)
	}
	if resp.Model != "claude-opus-5" {
		t.Errorf("Model = %q", resp.Model)
	}
	// The counters the interface prints come from here. A provider that does
	// not report them leaves Reported false and the interface says so rather
	// than printing zero.
	if !resp.Usage.Reported || resp.Usage.InputTokens != 120 || resp.Usage.OutputTokens != 64 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
	if resp.Usage.CacheReadTokens != 40 || resp.Usage.CacheWriteTokens != 7 {
		t.Errorf("cache counters = %d read, %d written", resp.Usage.CacheReadTokens, resp.Usage.CacheWriteTokens)
	}
	if len(*seen) != 1 {
		t.Errorf("%d request(s) sent for one answer", len(*seen))
	}
}

func TestAnthropicSendsOnlyWhatTheCurrentAPIAccepts(t *testing.T) {
	// These are the invariants CLAUDE.md lists for this file. temperature and
	// budget_tokens were removed from the current models and now answer 400,
	// so their absence is not a style preference: it is what keeps the backend
	// working at all. Streaming is here for the same reason — a long chapter
	// would otherwise outlast the HTTP timeout.
	p, seen := anthropicServer(t, AnthropicOptions{Model: "claude-opus-5", Effort: "xhigh"},
		func(w http.ResponseWriter, _ map[string]any) { streamAnswer(w, "ok", "end_turn") })

	if _, err := p.Complete(context.Background(), Request{
		System: "tu traduis", User: "un paragraphe", MaxTokens: 4321,
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	body := (*seen)[0]

	for _, banned := range []string{"temperature", "top_p", "top_k", "budget_tokens"} {
		if _, present := body[banned]; present {
			t.Errorf("%q was sent; the current models answer 400 to it", banned)
		}
	}
	if thinking, ok := body["thinking"].(map[string]any); ok {
		if _, present := thinking["budget_tokens"]; present {
			t.Error("thinking.budget_tokens was sent; it is removed from the current models")
		}
	}
	if body["stream"] != true {
		t.Error("the request was not streamed; a long chapter would outlast the HTTP timeout")
	}
	if body["max_tokens"] != float64(4321) {
		t.Errorf("max_tokens = %v", body["max_tokens"])
	}
	if body["model"] != "claude-opus-5" {
		t.Errorf("model = %v", body["model"])
	}

	// Effort is the dosage setting that replaced the removed ones, and it
	// lives inside output_config rather than at the top level.
	if _, present := body["effort"]; present {
		t.Error("effort was sent at the top level; it belongs inside output_config")
	}
	out, ok := body["output_config"].(map[string]any)
	if !ok {
		t.Fatalf("output_config missing: %v", body)
	}
	if out["effort"] != "xhigh" {
		t.Errorf("output_config.effort = %v", out["effort"])
	}
}

func TestAnthropicDefaultsToTheCurrentModel(t *testing.T) {
	p, seen := anthropicServer(t, AnthropicOptions{},
		func(w http.ResponseWriter, _ map[string]any) { streamAnswer(w, "ok", "end_turn") })
	if p.Model() != DefaultAnthropicModel {
		t.Errorf("Model() = %q, want %q", p.Model(), DefaultAnthropicModel)
	}
	if _, err := p.Complete(context.Background(), Request{User: "x"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if (*seen)[0]["model"] != DefaultAnthropicModel {
		t.Errorf("model sent = %v", (*seen)[0]["model"])
	}
}

func TestAnthropicRefusalIsAnErrorAndNotAnEmptyAnswer(t *testing.T) {
	// A refusal comes back as HTTP 200 with no usable content. Reading the
	// content without checking the stop reason would hand the pipeline an
	// empty string, which it would splice into the book as "keep the source"
	// — a silent hole instead of a reported one.
	p, _ := anthropicServer(t, AnthropicOptions{},
		func(w http.ResponseWriter, _ map[string]any) {
			streamRefusal(w, "cyber", "cette demande est refusée")
		})

	_, err := p.Complete(context.Background(), Request{User: "x"})
	var refusal *RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("err = %v (%T), want a RefusalError", err, err)
	}
	if refusal.Category != "cyber" {
		t.Errorf("Category = %q", refusal.Category)
	}
	if !strings.Contains(refusal.Explanation, "refusée") {
		t.Errorf("Explanation = %q", refusal.Explanation)
	}
	if Retryable(err) {
		t.Error("a refusal is not worth retrying: the same request would be refused again")
	}
}

func TestAnthropicTruncationIsAnErrorAndNotHalfAnAnswer(t *testing.T) {
	for _, stop := range []string{"max_tokens", "model_context_window_exceeded"} {
		t.Run(stop, func(t *testing.T) {
			p, _ := anthropicServer(t, AnthropicOptions{},
				func(w http.ResponseWriter, _ map[string]any) {
					streamAnswer(w, `{"translations":["la moitié d`, stop)
				})
			_, err := p.Complete(context.Background(), Request{User: "x"})
			if !errors.Is(err, ErrTruncated) {
				t.Fatalf("err = %v, want ErrTruncated", err)
			}
			if Retryable(err) {
				t.Error("a truncated answer is the caller's to split, not to retry unchanged")
			}
		})
	}
}

func TestAnthropicStopsAskingForStructuredOutputOnceRefused(t *testing.T) {
	// Some accounts and models do not serve structured outputs. The first 400
	// disables it for the rest of the run — asking again on every chunk of a
	// three-hundred-page book would double the request count for nothing.
	var mu sync.Mutex
	withSchema, without := 0, 0
	p, _ := anthropicServer(t, AnthropicOptions{Structured: true},
		func(w http.ResponseWriter, body map[string]any) {
			out, _ := body["output_config"].(map[string]any)
			_, asked := out["format"]
			mu.Lock()
			if asked {
				withSchema++
			} else {
				without++
			}
			mu.Unlock()
			if asked {
				apiError(w, 400, "invalid_request_error", "output_config.format is not supported for this model")
				return
			}
			streamAnswer(w, "ok", "end_turn")
		})

	schema := map[string]any{"type": "object"}
	for i := 0; i < 3; i++ {
		resp, err := p.Complete(context.Background(), Request{User: "x", Schema: schema})
		if err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
		if resp.Text != "ok" {
			t.Errorf("call %d: Text = %q", i+1, resp.Text)
		}
	}
	if withSchema != 1 {
		t.Errorf("the schema was sent %d time(s); it must be dropped after the first refusal", withSchema)
	}
	if without != 3 {
		t.Errorf("%d call(s) went out without a schema, want 3", without)
	}
}

func TestAnthropicKeepsStructuredOutputWhenItWorks(t *testing.T) {
	p, seen := anthropicServer(t, AnthropicOptions{Structured: true},
		func(w http.ResponseWriter, _ map[string]any) { streamAnswer(w, "ok", "end_turn") })
	schema := map[string]any{"type": "object", "properties": map[string]any{}}
	for i := 0; i < 2; i++ {
		if _, err := p.Complete(context.Background(), Request{User: "x", Schema: schema}); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	for i, body := range *seen {
		out, _ := body["output_config"].(map[string]any)
		if _, asked := out["format"]; !asked {
			t.Errorf("call %d went out without the schema although the service accepts it", i+1)
		}
	}
}

func TestAnthropicErrorsKeepTheirStatus(t *testing.T) {
	// The circuit breaker reads these: 401 is hopeless and must stop the book
	// at once, 429 is a bad minute and must be waited out.
	cases := []struct {
		status           int
		kind             string
		fatal, retryable bool
	}{
		{401, "authentication_error", true, false},
		{403, "permission_error", true, false},
		{404, "not_found_error", true, false},
		{429, "rate_limit_error", false, true},
		{500, "api_error", false, true},
		{529, "overloaded_error", false, true},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.status), func(t *testing.T) {
			p, _ := anthropicServer(t, AnthropicOptions{},
				func(w http.ResponseWriter, _ map[string]any) {
					apiError(w, c.status, c.kind, "refusé")
				})
			_, err := p.Complete(context.Background(), Request{User: "x"})
			var api *APIError
			if !errors.As(err, &api) {
				t.Fatalf("err = %v (%T), want an APIError", err, err)
			}
			if api.Status != c.status {
				t.Errorf("Status = %d, want %d", api.Status, c.status)
			}
			if api.Provider != "anthropic" {
				t.Errorf("Provider = %q", api.Provider)
			}
			if Fatal(err) != c.fatal {
				t.Errorf("Fatal = %v, want %v", Fatal(err), c.fatal)
			}
			if Retryable(err) != c.retryable {
				t.Errorf("Retryable = %v, want %v", Retryable(err), c.retryable)
			}
		})
	}
}

func TestAnthropicCancellationIsNotRetried(t *testing.T) {
	p, _ := anthropicServer(t, AnthropicOptions{},
		func(w http.ResponseWriter, _ map[string]any) { streamAnswer(w, "ok", "end_turn") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Complete(ctx, Request{User: "x"})
	if err == nil {
		t.Fatal("a cancelled context produced an answer")
	}
	if Retryable(err) {
		t.Error("a cancellation is deliberate; retrying it would ignore the user")
	}
}

func TestAnthropicListsTheModelsTheServiceReports(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/v1/models") {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-opus-5","type":"model"},{"id":"claude-haiku-4-5","type":"model"}],"has_more":false}`))
	}))
	defer srv.Close()

	p := NewAnthropic(AnthropicOptions{BaseURL: srv.URL, APIKey: "clef"})
	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if strings.Join(models, ",") != "claude-opus-5,claude-haiku-4-5" {
		t.Errorf("models = %q", models)
	}
}

func TestAnthropicSaysSoWhenTheServiceListsNothing(t *testing.T) {
	// An empty list is not an empty answer to show the user: it means the
	// name has to be typed by hand, and the message has to say that.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"has_more":false}`))
	}))
	defer srv.Close()

	p := NewAnthropic(AnthropicOptions{BaseURL: srv.URL, APIKey: "clef"})
	if _, err := p.ListModels(context.Background()); err == nil {
		t.Fatal("an empty list came back as a success")
	}
}

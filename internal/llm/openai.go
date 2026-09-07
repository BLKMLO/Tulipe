package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// OpenAICompatOptions configures a backend that speaks the OpenAI
// /chat/completions protocol. It covers local runtimes (Ollama, LM Studio,
// llama.cpp) and aggregators, so Tulipe can translate without any network call
// leaving the machine.
type OpenAICompatOptions struct {
	BaseURL     string // e.g. http://localhost:11434/v1
	APIKey      string // optional for local runtimes
	Model       string
	Temperature *float64 // nil leaves the server default
	JSONMode    bool     // ask for response_format json_object when a schema is wanted
	Timeout     time.Duration
}

// OpenAICompat is an OpenAI-protocol backend.
type OpenAICompat struct {
	baseURL     string
	apiKey      string
	model       string
	temperature *float64
	jsonMode    atomic.Bool
	http        *http.Client
}

// NewOpenAICompat builds an OpenAI-protocol backend.
func NewOpenAICompat(o OpenAICompatOptions) (*OpenAICompat, error) {
	base := strings.TrimRight(strings.TrimSpace(o.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("un service compatible OpenAI exige une URL de base (par exemple http://localhost:11434/v1)")
	}
	if strings.TrimSpace(o.Model) == "" {
		return nil, fmt.Errorf("un service compatible OpenAI exige un nom de modèle")
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	p := &OpenAICompat{
		baseURL:     base,
		apiKey:      strings.TrimSpace(o.APIKey),
		model:       strings.TrimSpace(o.Model),
		temperature: o.Temperature,
		http:        &http.Client{Timeout: timeout},
	}
	p.jsonMode.Store(o.JSONMode)
	return p, nil
}

// ID implements Provider.
func (p *OpenAICompat) ID() string { return "openai-compatible" }

// Model implements Provider.
func (p *OpenAICompat) Model() string { return p.model }

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	MaxTokens      int64          `json:"max_tokens,omitempty"`
	Temperature    *float64       `json:"temperature,omitempty"`
	ResponseFormat map[string]any `json:"response_format,omitempty"`
	Stream         bool           `json:"stream"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		FinishReason string      `json:"finish_reason"`
		Message      chatMessage `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Complete implements Provider.
func (p *OpenAICompat) Complete(ctx context.Context, req Request) (*Response, error) {
	wantJSON := req.Schema != nil && p.jsonMode.Load()
	resp, err := p.call(ctx, req, wantJSON)
	if err != nil && wantJSON && rejectsResponseFormat(err) {
		// Plenty of local servers ignore or reject response_format. Give up on
		// it for the rest of the run rather than failing every chunk.
		p.jsonMode.Store(false)
		return p.call(ctx, req, false)
	}
	return resp, err
}

func (p *OpenAICompat) call(ctx context.Context, req Request, wantJSON bool) (*Response, error) {
	body := chatRequest{
		Model:       p.model,
		Temperature: p.temperature,
		MaxTokens:   req.MaxTokens,
	}
	if req.System != "" {
		body.Messages = append(body.Messages, chatMessage{Role: "system", Content: req.System})
	}
	body.Messages = append(body.Messages, chatMessage{Role: "user", Content: req.User})
	if wantJSON {
		body.ResponseFormat = map[string]any{"type": "json_object"}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	httpResp, err := p.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(httpResp.Body, 32<<20))
	if err != nil {
		return nil, err
	}

	var parsed chatResponse
	_ = json.Unmarshal(raw, &parsed)

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		e := &APIError{Provider: "openai-compatible", Status: httpResp.StatusCode, Message: summarise(string(raw))}
		if parsed.Error != nil {
			e.Type = parsed.Error.Type
			if parsed.Error.Message != "" {
				e.Message = summarise(parsed.Error.Message)
			}
		}
		return nil, e
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("le service n'a renvoyé aucune réponse : %s", summarise(string(raw)))
	}
	choice := parsed.Choices[0]
	if choice.FinishReason == "length" {
		return nil, ErrTruncated
	}

	out := &Response{Text: choice.Message.Content, Model: parsed.Model}
	if out.Model == "" {
		out.Model = p.model
	}
	// Usage is optional in this protocol. When the server does not report it,
	// leave the counters unreported instead of showing zeros.
	if parsed.Usage != nil {
		out.Usage = Usage{
			InputTokens:  parsed.Usage.PromptTokens,
			OutputTokens: parsed.Usage.CompletionTokens,
			Reported:     true,
		}
	}
	return out, nil
}

func rejectsResponseFormat(err error) bool {
	var api *APIError
	if !errorsAs(err, &api) {
		return false
	}
	if api.Status != 400 && api.Status != 404 && api.Status != 422 {
		return false
	}
	msg := strings.ToLower(api.Message)
	return strings.Contains(msg, "response_format") || strings.Contains(msg, "json_object") ||
		strings.Contains(msg, "not supported")
}

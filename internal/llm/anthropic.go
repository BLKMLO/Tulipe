package llm

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultAnthropicModel is the model used unless the configuration names
// another one.
const DefaultAnthropicModel = "claude-opus-5"

// AnthropicOptions configures the Claude backend.
type AnthropicOptions struct {
	APIKey  string
	BaseURL string
	Model   string
	// Effort maps to output_config.effort: "low", "medium", "high", "xhigh",
	// "max". It trades translation care against token spend. Empty means the
	// API default.
	Effort string
	// Structured asks for a schema-constrained answer. It is disabled
	// automatically, for the rest of the run, if the API rejects it.
	Structured bool
}

// Anthropic talks to the Claude Messages API through the official Go SDK.
type Anthropic struct {
	client     anthropic.Client
	model      string
	effort     anthropic.OutputConfigEffort
	structured atomic.Bool
}

// NewAnthropic builds a Claude backend. An empty API key lets the SDK resolve
// credentials itself, from ANTHROPIC_API_KEY or a profile created by `ant auth
// login`.
func NewAnthropic(o AnthropicOptions) *Anthropic {
	var opts []option.RequestOption
	if o.APIKey != "" {
		opts = append(opts, option.WithAPIKey(o.APIKey))
	}
	if o.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(o.BaseURL))
	}
	// Retries are driven by the translator, which knows how to shrink a chunk
	// as well as wait; leaving them on here would multiply the two.
	opts = append(opts, option.WithMaxRetries(0))

	p := &Anthropic{
		client: anthropic.NewClient(opts...),
		model:  strings.TrimSpace(o.Model),
	}
	if p.model == "" {
		p.model = DefaultAnthropicModel
	}
	switch strings.ToLower(strings.TrimSpace(o.Effort)) {
	case "low":
		p.effort = anthropic.OutputConfigEffortLow
	case "medium":
		p.effort = anthropic.OutputConfigEffortMedium
	case "high":
		p.effort = anthropic.OutputConfigEffortHigh
	case "xhigh":
		p.effort = anthropic.OutputConfigEffortXhigh
	case "max":
		p.effort = anthropic.OutputConfigEffortMax
	}
	p.structured.Store(o.Structured)
	return p
}

// ID implements Provider.
func (p *Anthropic) ID() string { return "anthropic" }

// Model implements Provider.
func (p *Anthropic) Model() string { return p.model }

// Complete implements Provider.
func (p *Anthropic) Complete(ctx context.Context, req Request) (*Response, error) {
	useSchema := req.Schema != nil && p.structured.Load()
	resp, err := p.call(ctx, req, useSchema)
	if err != nil && useSchema && rejectsOutputConfig(err) {
		// The account or the model does not serve structured outputs. Drop it
		// for the rest of the run and parse the JSON ourselves.
		p.structured.Store(false)
		return p.call(ctx, req, false)
	}
	return resp, err
}

func (p *Anthropic) call(ctx context.Context, req Request, useSchema bool) (*Response, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 16000
	}
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(p.model),
		MaxTokens: maxTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(req.User)),
		},
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	if p.effort != "" {
		params.OutputConfig.Effort = p.effort
	}
	if useSchema {
		params.OutputConfig.Format = anthropic.JSONOutputFormatParam{Schema: req.Schema}
	}

	// Streaming keeps long chapters from tripping the HTTP request timeout.
	stream := p.client.Messages.NewStreaming(ctx, params)
	message := anthropic.Message{}
	for stream.Next() {
		if err := message.Accumulate(stream.Current()); err != nil {
			return nil, err
		}
	}
	if err := stream.Err(); err != nil {
		return nil, wrapAnthropicError(err)
	}

	switch message.StopReason {
	case anthropic.StopReasonRefusal:
		return nil, &RefusalError{
			Category:    string(message.StopDetails.Category),
			Explanation: message.StopDetails.Explanation,
		}
	case anthropic.StopReasonMaxTokens, anthropic.StopReasonModelContextWindowExceeded:
		return nil, ErrTruncated
	}

	var text strings.Builder
	for _, block := range message.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(tb.Text)
		}
	}
	return &Response{
		Text:  text.String(),
		Model: string(message.Model),
		Usage: Usage{
			InputTokens:      message.Usage.InputTokens,
			OutputTokens:     message.Usage.OutputTokens,
			CacheReadTokens:  message.Usage.CacheReadInputTokens,
			CacheWriteTokens: message.Usage.CacheCreationInputTokens,
			Reported:         true,
		},
	}, nil
}

func wrapAnthropicError(err error) error {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	return &APIError{
		Provider: "anthropic",
		Status:   apiErr.StatusCode,
		Type:     string(apiErr.Type()),
		Message:  summarise(apiErr.RawJSON()),
	}
}

// rejectsOutputConfig recognises the 400 returned when structured outputs are
// not available for this request.
func rejectsOutputConfig(err error) bool {
	var api *APIError
	if !errors.As(err, &api) || api.Status != 400 {
		return false
	}
	msg := strings.ToLower(api.Message)
	return strings.Contains(msg, "output_config") || strings.Contains(msg, "output config") ||
		strings.Contains(msg, "schema")
}

// summarise trims an API error body down to something a terminal can show.
func summarise(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\n", " "))
	const max = 400
	if len(raw) > max {
		return raw[:max] + "…"
	}
	return raw
}

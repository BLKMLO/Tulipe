package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/blkmlo/tulipe/internal/i18n"
)

// DeepLOptions configures the DeepL backend.
type DeepLOptions struct {
	APIKey  string
	BaseURL string
	// Formality is DeepL's tone setting: "default", "more", "less". Empty
	// leaves the service's own default, and DeepL ignores it for the language
	// pairs that do not support it.
	Formality string
	Timeout   time.Duration
}

// DeepL translates through DeepL's own API. Unlike a chat model it is given the
// segments themselves, so it cannot answer with the wrong number of them, with
// malformed JSON, or with a commentary — a whole class of failures simply does
// not arise. In exchange it takes no glossary text and no style instructions.
type DeepL struct {
	baseURL   string
	apiKey    string
	formality string
	http      *http.Client
}

// FreeKeySuffix marks a DeepL key issued for the free tier, which is served by
// a different host.
const FreeKeySuffix = ":fx"

// NewDeepL builds the DeepL backend. When no base URL is given, the host is
// chosen from the key: a free-tier key ends in ":fx" and is served by
// api-free.deepl.com.
func NewDeepL(o DeepLOptions) (*DeepL, error) {
	key := strings.TrimSpace(o.APIKey)
	if key == "" {
		return nil, errors.New(i18n.T("llm.err.deepl-needs-key"))
	}
	base := strings.TrimRight(strings.TrimSpace(o.BaseURL), "/")
	if base == "" {
		if strings.HasSuffix(key, FreeKeySuffix) {
			base = "https://api-free.deepl.com/v2"
		} else {
			base = "https://api.deepl.com/v2"
		}
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return &DeepL{
		baseURL:   base,
		apiKey:    key,
		formality: strings.TrimSpace(o.Formality),
		http:      &http.Client{Timeout: timeout},
	}, nil
}

// ID implements Provider.
func (d *DeepL) ID() string { return KindDeepL }

// Model implements Provider. DeepL exposes no model identifier of its own.
func (d *DeepL) Model() string { return "deepl" }

// Complete implements Provider. DeepL is not a chat service, so the prompt path
// is refused outright rather than faked.
func (d *DeepL) Complete(context.Context, Request) (*Response, error) {
	return nil, errors.New(i18n.T("llm.err.deepl-no-prompting"))
}

type deeplRequest struct {
	Text               []string `json:"text"`
	TargetLang         string   `json:"target_lang"`
	SourceLang         string   `json:"source_lang,omitempty"`
	TagHandling        string   `json:"tag_handling,omitempty"`
	PreserveFormatting bool     `json:"preserve_formatting"`
	Formality          string   `json:"formality,omitempty"`
}

type deeplResponse struct {
	Translations []struct {
		Text                   string `json:"text"`
		DetectedSourceLanguage string `json:"detected_source_language"`
	} `json:"translations"`
	Message string `json:"message"`
}

// TranslateSegments implements DirectTranslator.
func (d *DeepL) TranslateSegments(ctx context.Context, req SegmentRequest) (*SegmentResponse, error) {
	if len(req.Segments) == 0 {
		return &SegmentResponse{Model: d.Model()}, nil
	}
	target := DeepLLanguage(req.TargetCode)
	if target == "" {
		return nil, errors.New(i18n.T("llm.err.deepl-needs-code"))
	}

	body := deeplRequest{
		Text:               req.Segments,
		TargetLang:         target,
		SourceLang:         DeepLSourceLanguage(req.SourceCode),
		PreserveFormatting: true,
		Formality:          d.formality,
	}
	if req.Markup {
		// Segments carry the book's inline tags; DeepL moves them with the
		// words instead of translating or dropping them.
		body.TagHandling = "xml"
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/translate", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "DeepL-Auth-Key "+d.apiKey)

	resp, err := d.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}

	var parsed deeplResponse
	_ = json.Unmarshal(raw, &parsed)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := parsed.Message
		if msg == "" {
			msg = summarise(string(raw))
		}
		// 456 is DeepL's own code for an exhausted character quota; it is not
		// going to clear on a retry within this run.
		if resp.StatusCode == 456 {
			return nil, &APIError{Provider: KindDeepL, Status: 403,
				Message: i18n.T("llm.err.deepl-quota", msg)}
		}
		return nil, &APIError{Provider: KindDeepL, Status: resp.StatusCode, Message: summarise(msg)}
	}
	if len(parsed.Translations) != len(req.Segments) {
		return nil, fmt.Errorf(i18n.T("llm.err.deepl-count"),
			len(parsed.Translations), len(req.Segments))
	}

	out := &SegmentResponse{Model: d.Model()}
	for _, t := range parsed.Translations {
		out.Translations = append(out.Translations, t.Text)
	}
	// DeepL bills characters, not tokens, and reports them on a separate
	// endpoint. Leaving the counters unreported keeps the interface honest.
	return out, nil
}

// DeepLLanguage turns a BCP 47 tag into a DeepL target language. DeepL accepts
// a small, fixed set of codes; only the regional variants it distinguishes are
// kept, the rest fall back to the base language. An unknown code is passed
// through in upper case so that DeepL's own error names the valid values
// instead of Tulipe guessing them.
func DeepLLanguage(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}
	normalised := strings.ToUpper(strings.ReplaceAll(code, "_", "-"))
	switch normalised {
	case "EN-GB", "EN-US", "PT-BR", "PT-PT", "ZH-HANS", "ZH-HANT":
		if normalised == "ZH-HANS" {
			return "ZH"
		}
		if normalised == "ZH-HANT" {
			return "ZH-HANT"
		}
		return normalised
	}
	if base, _, found := strings.Cut(normalised, "-"); found {
		return base
	}
	return normalised
}

// DeepLSourceLanguage is the same mapping for the source side, where DeepL
// takes no regional variant.
func DeepLSourceLanguage(code string) string {
	lang := DeepLLanguage(code)
	if base, _, found := strings.Cut(lang, "-"); found {
		return base
	}
	return lang
}

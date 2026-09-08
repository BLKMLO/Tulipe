package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeepLSendsSegmentsAndReadsThemBack(t *testing.T) {
	var got deeplRequest
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/translate" {
			t.Errorf("path = %q, want /v2/translate", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		out := map[string]any{"translations": []map[string]string{}}
		var list []map[string]string
		for _, s := range got.Text {
			list = append(list, map[string]string{"text": strings.ToUpper(s), "detected_source_language": "EN"})
		}
		out["translations"] = list
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()

	d, err := NewDeepL(DeepLOptions{APIKey: "clef", BaseURL: srv.URL + "/v2"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := d.TranslateSegments(context.Background(), SegmentRequest{
		Segments:   []string{"hello <em>world</em>", "second"},
		TargetCode: "fr", SourceCode: "en", Markup: true,
	})
	if err != nil {
		t.Fatalf("TranslateSegments: %v", err)
	}
	if len(resp.Translations) != 2 || resp.Translations[0] != "HELLO <EM>WORLD</EM>" {
		t.Errorf("translations = %q", resp.Translations)
	}
	if auth != "DeepL-Auth-Key clef" {
		t.Errorf("Authorization = %q", auth)
	}
	if got.TargetLang != "FR" || got.SourceLang != "EN" {
		t.Errorf("languages = %q/%q, want FR/EN", got.TargetLang, got.SourceLang)
	}
	if got.TagHandling != "xml" {
		t.Errorf("TagHandling = %q; segments carrying markup need xml handling", got.TagHandling)
	}
	if !got.PreserveFormatting {
		t.Error("PreserveFormatting must be on, or spacing drifts")
	}
	// DeepL bills characters, not tokens: the counters must stay unreported
	// rather than show zeros.
	if resp.Usage.Reported {
		t.Error("usage must not be reported for a service that does not send it")
	}
}

func TestDeepLLeavesTagHandlingOffForPlainText(t *testing.T) {
	var got deeplRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"translations":[{"text":"bonjour"}]}`))
	}))
	defer srv.Close()

	d, _ := NewDeepL(DeepLOptions{APIKey: "k", BaseURL: srv.URL + "/v2"})
	if _, err := d.TranslateSegments(context.Background(), SegmentRequest{
		Segments: []string{"hello"}, TargetCode: "fr",
	}); err != nil {
		t.Fatal(err)
	}
	if got.TagHandling != "" {
		t.Errorf("TagHandling = %q, want it left off for plain text", got.TagHandling)
	}
}

func TestDeepLReportsACountMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"translations":[{"text":"un"}]}`))
	}))
	defer srv.Close()
	d, _ := NewDeepL(DeepLOptions{APIKey: "k", BaseURL: srv.URL + "/v2"})
	_, err := d.TranslateSegments(context.Background(), SegmentRequest{
		Segments: []string{"a", "b"}, TargetCode: "fr",
	})
	if err == nil || !strings.Contains(err.Error(), "1 traductions") {
		t.Errorf("err = %v, want a count mismatch", err)
	}
}

func TestDeepLQuotaExhaustionIsFatal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(456)
		_, _ = w.Write([]byte(`{"message":"Quota Exceeded"}`))
	}))
	defer srv.Close()
	d, _ := NewDeepL(DeepLOptions{APIKey: "k", BaseURL: srv.URL + "/v2"})
	_, err := d.TranslateSegments(context.Background(), SegmentRequest{Segments: []string{"a"}, TargetCode: "fr"})
	if !Fatal(err) {
		t.Errorf("err = %v; an exhausted quota must stop the run, not be retried", err)
	}
	if !strings.Contains(err.Error(), "quota") {
		t.Errorf("the message should mention the quota: %v", err)
	}
}

func TestDeepLErrorsCarryTheServiceMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"message":"Value for 'target_lang' not supported."}`))
	}))
	defer srv.Close()
	d, _ := NewDeepL(DeepLOptions{APIKey: "k", BaseURL: srv.URL + "/v2"})
	_, err := d.TranslateSegments(context.Background(), SegmentRequest{Segments: []string{"a"}, TargetCode: "xx"})
	var api *APIError
	if !errors.As(err, &api) || api.Status != 400 {
		t.Fatalf("err = %v, want a 400 APIError", err)
	}
	if !strings.Contains(api.Message, "target_lang") {
		t.Errorf("the service's own explanation was lost: %q", api.Message)
	}
}

func TestDeepLChoosesTheHostFromTheKey(t *testing.T) {
	free, err := NewDeepL(DeepLOptions{APIKey: "abc" + FreeKeySuffix})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(free.baseURL, "api-free.deepl.com") {
		t.Errorf("free-tier key routed to %q", free.baseURL)
	}
	paid, err := NewDeepL(DeepLOptions{APIKey: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(paid.baseURL, "api.deepl.com") || strings.Contains(paid.baseURL, "api-free") {
		t.Errorf("paid key routed to %q", paid.baseURL)
	}
	if _, err := NewDeepL(DeepLOptions{}); err == nil {
		t.Error("DeepL without a key must be refused")
	}
}

func TestDeepLRefusesThePromptPath(t *testing.T) {
	d, _ := NewDeepL(DeepLOptions{APIKey: "k"})
	if _, err := d.Complete(context.Background(), Request{User: "traduis"}); err == nil {
		t.Error("DeepL must refuse free-form instructions rather than pretend to follow them")
	}
}

func TestDeepLLanguageMapping(t *testing.T) {
	cases := map[string]string{
		"fr": "FR", "FR": "FR", "en": "EN", "en-GB": "EN-GB", "en-US": "EN-US",
		"pt-BR": "PT-BR", "pt": "PT", "zh-Hans": "ZH", "zh-Hant": "ZH-HANT",
		"de-AT": "DE", "": "",
	}
	for in, want := range cases {
		if got := DeepLLanguage(in); got != want {
			t.Errorf("DeepLLanguage(%q) = %q, want %q", in, got, want)
		}
	}
	// The source side takes no regional variant.
	if got := DeepLSourceLanguage("pt-BR"); got != "PT" {
		t.Errorf("DeepLSourceLanguage(pt-BR) = %q, want PT", got)
	}
}

func TestDeepLNeedsATargetCode(t *testing.T) {
	d, _ := NewDeepL(DeepLOptions{APIKey: "k"})
	_, err := d.TranslateSegments(context.Background(), SegmentRequest{Segments: []string{"a"}})
	if err == nil || !strings.Contains(err.Error(), "code de langue") {
		t.Errorf("err = %v, want a complaint about the missing language code", err)
	}
}

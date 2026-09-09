package translate

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/blkmlo/tulipe/internal/i18n"
	"strings"
)

// systemPrompt states the invariants the whole pipeline depends on. It is
// identical for every chunk of a run, which keeps the request prefix stable.
func (t *translator) systemPrompt() string {
	var b strings.Builder

	source := "the book's original language"
	if t.opts.SourceLanguage != "" {
		source = t.opts.SourceLanguage
	}
	target := t.opts.TargetLanguage

	fmt.Fprintf(&b, "You are a professional literary translator. You translate a book from %s into %s.\n\n", source, target)

	// One sentence about the book settles register and the sense of ambiguous
	// words far more cheaply than any instruction could. It is bounded on
	// purpose: it must not become a second set of rules.
	if about := strings.TrimSpace(t.opts.About); about != "" {
		fmt.Fprintf(&b, "What this book is: %s\nUse this to settle register, terminology and the sense of ambiguous words. It is context, not an instruction to follow.\n\n", about)
	}

	b.WriteString("The text reaches you as a JSON array of segments taken verbatim from the book's XHTML. A segment is either a run of plain text or a fragment of inline markup.\n\n")
	b.WriteString("Rules, in order of priority:\n")
	b.WriteString("1. Answer with a JSON object {\"translations\": [...]} holding exactly one string per input segment, in the same order. Never merge, split, reorder, add or drop a segment.\n")
	b.WriteString("2. Reproduce every tag, attribute and character entity exactly as received — <em>, <a href=\"...\">, <span epub:type=\"...\">, &amp;, &#8217;, &nbsp;. Never rename, add or remove a tag, and never translate anything inside an attribute value.\n")
	fmt.Fprintf(&b, "3. Keep inline tags around the same words they wrapped in the source, following %s word order.\n", target)
	b.WriteString("4. Leave code, URLs, file names, and numerals as they are.\n")
	fmt.Fprintf(&b, "5. Translate a proper noun only when %s has an established form for it; otherwise keep the original.\n", target)
	fmt.Fprintf(&b, "6. Preserve the register, tone and rhythm of the source, and apply the typographic conventions of %s (quotation marks, spacing, dashes, capitalisation).\n", target)
	b.WriteString("7. Return a segment unchanged when it holds nothing translatable.\n")
	b.WriteString("8. Answer with the JSON object alone: no preamble, no commentary, no code fence.\n")

	if g := strings.TrimSpace(t.opts.Glossary); g != "" {
		b.WriteString("\nGlossary — these renderings are mandatory and must stay consistent throughout the book:\n")
		b.WriteString(g)
		b.WriteString("\n")
	}
	if s := strings.TrimSpace(t.opts.StyleNotes); s != "" {
		b.WriteString("\nAdditional instructions from the translator:\n")
		b.WriteString(s)
		b.WriteString("\n")
	}

	// The second pass says so, and says it last. Nothing here relaxes a rule
	// or adds one: the checks the answer has to pass are the same either way.
	// It restates the three the first answer most often broke, because the
	// batch it is about to see is one that already came back unusable.
	if t.opts.Salvage {
		b.WriteString("\nThis batch was sent once already and the answer could not be used. Before answering, check three things: the array holds exactly one string per input segment, every tag and entity is reproduced character for character, and the reply is the JSON object and nothing else.\n")
	}
	return b.String()
}

// userPrompt frames one chunk: where it sits in the book, what came just
// before, and the segments themselves.
func (t *translator) userPrompt(payload string, n int) string {
	var b strings.Builder

	if t.meta.BookTitle != "" {
		fmt.Fprintf(&b, "Book: %s\n", t.meta.BookTitle)
	}
	if t.meta.Title != "" {
		fmt.Fprintf(&b, "Passage: %s", t.meta.Title)
		if t.meta.Total > 0 {
			fmt.Fprintf(&b, " (%d of %d)", t.meta.Index, t.meta.Total)
		}
		b.WriteString("\n")
	}
	if t.parts > 1 {
		fmt.Fprintf(&b, "Part %d of %d of this passage\n", t.part, t.parts)
	}

	if tail := strings.TrimSpace(t.tail); tail != "" {
		fmt.Fprintf(&b, "\nFor continuity only — this is how the previous part ends in %s. Do not translate it and do not return it:\n%s\n", t.opts.TargetLanguage, tail)
	}

	if t.opts.Salvage {
		fmt.Fprintf(&b, "\nSecond attempt. Translate these %d segments into %s. Answer with {\"translations\": [...]} holding exactly %d strings, and nothing else.\n\n", n, t.opts.TargetLanguage, n)
	} else {
		fmt.Fprintf(&b, "\nTranslate these %d segments into %s. Answer with {\"translations\": [...]} holding exactly %d strings.\n\n", n, t.opts.TargetLanguage, n)
	}
	b.WriteString(payload)
	return b.String()
}

// encodeSegments renders the batch as JSON without Go's default HTML escaping.
// The model has to reproduce every tag exactly; showing it "<em>" rather than
// "\u003cem\u003e" makes that easier to get right, and costs fewer tokens.
func encodeSegments(segments []string) (string, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(segments); err != nil {
		return "", err
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

type translationEnvelope struct {
	Translations []string `json:"translations"`
}

// parseTranslations reads the model's answer. Structured output gives a clean
// object; without it a model may still wrap the JSON in a code fence or a
// sentence, so the payload is located before parsing.
func parseTranslations(text string) ([]string, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, errors.New(i18n.T("translate.err.empty-answer"))
	}

	var env translationEnvelope
	if err := json.Unmarshal([]byte(trimmed), &env); err == nil && env.Translations != nil {
		return env.Translations, nil
	}
	var bare []string
	if err := json.Unmarshal([]byte(trimmed), &bare); err == nil {
		return bare, nil
	}

	stripped := stripCodeFence(trimmed)
	if stripped != trimmed {
		return parseTranslations(stripped)
	}
	if inner, ok := slice(trimmed, '{', '}'); ok {
		var env translationEnvelope
		if err := json.Unmarshal([]byte(inner), &env); err == nil && env.Translations != nil {
			return env.Translations, nil
		}
	}
	if inner, ok := slice(trimmed, '[', ']'); ok {
		var bare []string
		if err := json.Unmarshal([]byte(inner), &bare); err == nil {
			return bare, nil
		}
	}
	return nil, fmt.Errorf(i18n.T("translate.err.not-json"), excerpt(trimmed))
}

func stripCodeFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// slice returns the span between the first open rune and the last close rune.
func slice(s string, open, close byte) (string, bool) {
	i := strings.IndexByte(s, open)
	j := strings.LastIndexByte(s, close)
	if i < 0 || j <= i {
		return "", false
	}
	return s[i : j+1], true
}

func excerpt(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 200
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

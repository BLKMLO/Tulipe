package i18n

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestEveryLocaleCoversTheSameKeys is what keeps a half-translated interface
// out of a release. A missing key silently falls back to English, which reads
// as a bug rather than as a language.
func TestEveryLocaleCoversTheSameKeys(t *testing.T) {
	for code, l := range locales {
		if code == English {
			continue
		}
		for key := range messagesEN {
			if _, ok := l.messages[key]; !ok {
				t.Errorf("%s: missing key %q", code, key)
			}
		}
		for key := range l.messages {
			if _, ok := messagesEN[key]; !ok {
				t.Errorf("%s: key %q has no English original", code, key)
			}
		}
	}
}

// verbs finds the formatting placeholders of a message. A translation that
// dropped or reordered one would panic or print "%!d(MISSING)" at the worst
// possible moment — in the middle of a book.
var verbs = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z%]`)

func TestEveryTranslationKeepsTheSamePlaceholders(t *testing.T) {
	for code, l := range locales {
		if code == English {
			continue
		}
		for key, want := range messagesEN {
			got, ok := l.messages[key]
			if !ok {
				continue // reported by the test above
			}
			a, b := placeholders(want), placeholders(got)
			if strings.Join(a, "") != strings.Join(b, "") {
				t.Errorf("%s: key %q has placeholders %v, English has %v", code, key, b, a)
			}
		}
	}
}

func placeholders(s string) []string {
	var out []string
	for _, m := range verbs.FindAllString(s, -1) {
		if m == "%%" {
			continue
		}
		out = append(out, m)
	}
	return out
}

func TestDefaultLocaleIsEnglish(t *testing.T) {
	SetLocale("")
	if Locale() != English {
		t.Fatalf("an unknown code must fall back to %q, got %q", English, Locale())
	}
	if got := T("ui.menu.quit"); got != "Quit" {
		t.Fatalf("English quit label = %q", got)
	}
}

// TestFrenchIsStillThere is the point of the exercise: English is the default,
// French is one setting away, and nothing was dropped along the way.
func TestFrenchIsStillThere(t *testing.T) {
	t.Cleanup(func() { SetLocale(DefaultLocale) })
	SetLocale(French)
	if got := T("ui.menu.quit"); got != "Quitter" {
		t.Fatalf("French quit label = %q", got)
	}
	if got := T("ui.book.pending.value", 3); !strings.Contains(got, "3 passage(s)") {
		t.Fatalf("French formatting lost: %q", got)
	}
}

func TestUnknownKeyComesBackUnchanged(t *testing.T) {
	// A preset name is passed through T so that the one entry which needs
	// translating gets it; every other name must survive untouched.
	if got := T("Anthropic (Claude)"); got != "Anthropic (Claude)" {
		t.Fatalf("unknown key = %q", got)
	}
}

func TestLocaleNamesRoundTrip(t *testing.T) {
	names := LocaleNames()
	sort.Strings(append([]string(nil), names...))
	if len(names) != len(Locales) {
		t.Fatalf("%d names for %d locales", len(names), len(Locales))
	}
	for _, code := range Locales {
		got, ok := LocaleByName(LocaleName(code))
		if !ok || got != code {
			t.Errorf("%q did not round-trip: %q, %v", code, got, ok)
		}
	}
}

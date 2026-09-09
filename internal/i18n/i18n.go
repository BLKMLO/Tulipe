// Package i18n holds every string the user reads, in each language Tulipe
// speaks.
//
// It sits at the bottom of the dependency graph: epub, llm, translate, config
// and ui all import it, and it imports none of them. That is what lets an error
// raised deep in the EPUB reader reach the screen already written in the user's
// language.
//
// The interface language is a setting of its own. It has nothing to do with the
// language a book is translated into: someone may well read a French interface
// while translating into Japanese.
package i18n

import (
	"fmt"
	"sync/atomic"
)

// Locale codes. English is the default because it is the language the widest
// audience can read; French is kept in full, and switching back to it is one
// setting away.
const (
	English = "en"
	French  = "fr"
)

// DefaultLocale is what a fresh installation starts in.
const DefaultLocale = English

// locale is one language: its code, the name it calls itself, and its messages.
type locale struct {
	code     string
	name     string
	messages map[string]string
}

var locales = map[string]*locale{
	English: {code: English, name: "English", messages: messagesEN},
	French:  {code: French, name: "Français", messages: messagesFR},
}

// Locales lists the codes Tulipe knows, in display order.
var Locales = []string{English, French}

// current is read on every lookup and written when the setting changes, so it
// is held atomically: a translation run reports its notes from a goroutine.
var current atomic.Pointer[locale]

func init() { current.Store(locales[DefaultLocale]) }

// SetLocale switches the interface language. An unknown code falls back to the
// default rather than leaving the program without any strings at all.
func SetLocale(code string) {
	if l, ok := locales[code]; ok {
		current.Store(l)
		return
	}
	current.Store(locales[DefaultLocale])
}

// Locale is the code currently in use.
func Locale() string { return current.Load().code }

// Known reports whether a code names a language Tulipe speaks.
func Known(code string) bool { _, ok := locales[code]; return ok }

// LocaleName is how a language names itself, for the settings screen.
func LocaleName(code string) string {
	if l, ok := locales[code]; ok {
		return l.name
	}
	return code
}

// LocaleNames lists the self-names of every language, in the order of Locales.
func LocaleNames() []string {
	out := make([]string, 0, len(Locales))
	for _, code := range Locales {
		out = append(out, LocaleName(code))
	}
	return out
}

// LocaleByName is the reverse of LocaleName; it is how the settings screen
// turns a picked name back into a code.
func LocaleByName(name string) (string, bool) {
	for _, code := range Locales {
		if LocaleName(code) == name {
			return code, true
		}
	}
	return "", false
}

// T returns the message filed under key in the current language.
//
// A key missing from the current language falls back to English, and a key
// missing there too is returned as it is: a caller may legitimately pass text
// that is not a key at all — a service name, say — and get it back unchanged.
//
// With no arguments the message is returned verbatim, which is what lets a
// template carrying %w be handed straight to fmt.Errorf.
func T(key string, args ...any) string {
	format := lookup(key)
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

func lookup(key string) string {
	if s, ok := current.Load().messages[key]; ok {
		return s
	}
	if s, ok := messagesEN[key]; ok {
		return s
	}
	return key
}

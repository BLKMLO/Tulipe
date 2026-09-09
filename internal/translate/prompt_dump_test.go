package translate

import (
	"fmt"
	"os"
	"testing"
)

func TestDumpPrompt(t *testing.T) {
	if os.Getenv("TULIPE_PROMPT") == "" {
		t.Skip("set TULIPE_PROMPT=1")
	}
	opts := Options{
		TargetLanguage: "français",
		SourceLanguage: "english",
		About:          "un roman d'anticipation politique publié en 1949",
		Glossary:       "Victory Mansions = Maison de la Victoire\nNewspeak = novlangue",
		StyleNotes:     "Tutoiement entre les personnages proches.",
	}.Defaults()

	tr := &translator{
		opts:  opts,
		meta:  DocMeta{BookTitle: "Nineteen Eighty-Four", Title: "Chapitre 1", Index: 1, Total: 24},
		tail:  "…il gravit les marches, le menton enfoui dans son écharpe.",
		parts: 3, part: 2,
	}
	segments := []string{
		"It was a bright cold day in April, and the clocks were striking <em>thirteen</em>.",
		"Winston Smith slipped quickly through the glass doors of Victory Mansions &amp; paused.",
	}
	payload, _ := encodeSegments(segments)

	fmt.Println("╭──────────────── PROMPT SYSTÈME ────────────────╮")
	fmt.Println(tr.systemPrompt())
	fmt.Println("╭──────────────── MESSAGE UTILISATEUR ───────────╮")
	fmt.Println(tr.userPrompt(payload, len(segments)))
}

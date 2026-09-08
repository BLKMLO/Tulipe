package epub

import (
	"encoding/xml"
	"strings"
)

// blockBreak lists the elements that end a paragraph when rendering plain text.
var blockBreak = map[string]bool{
	"p": true, "div": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "li": true, "blockquote": true, "tr": true,
	"dt": true, "dd": true, "figcaption": true, "caption": true, "section": true,
	"article": true, "header": true, "footer": true, "hr": true, "table": true,
	"ul": true, "ol": true, "dl": true, "pre": true,
}

// PlainText renders a content document as readable text: paragraphs separated
// by a blank line, no markup, no entities.
//
// This is a rendering, not a round-trip. It is only ever used for the text
// export, never to write a document back into the book — that path stays on the
// byte-offset splice.
func PlainText(doc []byte) string {
	d := newDecoder(doc)
	var (
		out       strings.Builder
		para      strings.Builder
		depth     int
		skipDepth int
		preDepth  int
	)

	flush := func() {
		text := para.String()
		if !strings.HasPrefix(text, "\x00") {
			text = strings.TrimSpace(collapse(text))
		} else {
			text = strings.Trim(strings.TrimPrefix(text, "\x00"), "\n")
		}
		para.Reset()
		if text == "" {
			return
		}
		if out.Len() > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString(text)
	}

	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			name := strings.ToLower(t.Name.Local)
			switch {
			case skipDepth > 0:
			case name == "script" || name == "style" || name == "head":
				skipDepth = depth
			case name == "br":
				para.WriteString("\n")
			case name == "pre":
				flush()
				preDepth = depth
				// The marker tells flush to keep the original line breaks.
				para.WriteString("\x00")
			case blockBreak[name]:
				flush()
			}
		case xml.EndElement:
			if skipDepth == depth {
				skipDepth = 0
			}
			if preDepth == depth {
				preDepth = 0
				flush()
			} else if skipDepth == 0 && blockBreak[strings.ToLower(t.Name.Local)] {
				flush()
			}
			depth--
		case xml.CharData:
			if skipDepth == 0 {
				para.Write(t)
			}
		}
	}
	flush()
	return out.String()
}

// PlainText renders the whole book as text, in reading order, with each chapter
// introduced by its title.
func (b *Book) PlainText() string {
	var out strings.Builder
	if b.Title != "" {
		out.WriteString(b.Title)
		out.WriteString("\n")
		out.WriteString(strings.Repeat("=", len([]rune(b.Title))))
	}
	for _, ch := range b.Chapters {
		doc, ok := b.Read(ch.Path)
		if !ok {
			continue
		}
		body := strings.TrimSpace(PlainText(doc))
		if body == "" {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("\n\n")
		}
		// The label is read from the document as it stands now, not from the
		// table of contents captured when the book was opened: after a
		// translation those still hold the original wording, and printing both
		// would show every chapter title twice, once per language.
		title := strings.TrimSpace(DocumentTitle(doc))
		if title == "" {
			title = strings.TrimSpace(ch.Title)
		}
		if title != "" && !strings.HasPrefix(body, title) {
			out.WriteString(title)
			out.WriteString("\n")
			out.WriteString(strings.Repeat("-", len([]rune(title))))
			out.WriteString("\n\n")
		}
		out.WriteString(body)
	}
	if out.Len() == 0 {
		return ""
	}
	return out.String() + "\n"
}

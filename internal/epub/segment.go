package epub

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Kind tells how a segment must be handled once translated.
type Kind int

const (
	// KindBlock is the inner markup of a block-level element. The translation
	// is expected to be well-formed XML and is spliced back verbatim.
	KindBlock Kind = iota
	// KindText is a bare run of character data. The translation is plain text
	// and gets XML-escaped before being spliced back.
	KindText
)

// Segment is a translatable span of an XHTML document, located by byte offset
// in the original file. Everything outside these spans is never touched, which
// is what keeps the document byte-identical apart from its prose.
type Segment struct {
	Start  int // inclusive byte offset in the source document
	End    int // exclusive byte offset in the source document
	Kind   Kind
	Source string // raw source bytes of the span
	Elem   string // element that produced the span, for diagnostics
}

// blockElements start a block segment: the whole inner markup is translated in
// one piece so that a sentence split across <em>/<a> tags stays a sentence.
var blockElements = map[string]bool{
	"p": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true,
	"h6": true, "li": true, "blockquote": true, "td": true, "th": true,
	"dt": true, "dd": true, "figcaption": true, "caption": true, "text": true,
}

// skipElements and everything they contain are left alone.
var skipElements = map[string]bool{
	"script": true, "style": true, "pre": true, "svg": true, "math": true,
	"audio": true, "video": true, "meta": true, "link": true,
}

// UnsupportedEncodingError is returned for a document that declares a character
// encoding Tulipe cannot read. Decoding it would shift every byte offset, and
// the offsets are what keeps the rest of the document intact — so such a
// document is refused rather than mangled.
type UnsupportedEncodingError struct{ Encoding string }

func (e *UnsupportedEncodingError) Error() string {
	return fmt.Sprintf("document encodé en %s ; Tulipe ne sait lire que l'UTF-8", e.Encoding)
}

// declaredEncoding reads the encoding pseudo-attribute of the XML declaration.
var declaredEncoding = regexp.MustCompile(`(?i)^\s*<\?xml[^>]*\bencoding\s*=\s*["']([^"']+)["']`)

// checkEncoding refuses a document whose declaration names anything but UTF-8
// or ASCII, which are byte-compatible with the way the rest of the package
// works.
func checkEncoding(doc []byte) error {
	head := doc
	if len(head) > 256 {
		head = head[:256]
	}
	m := declaredEncoding.FindSubmatch(head)
	if m == nil {
		return nil
	}
	switch enc := strings.ToLower(strings.TrimSpace(string(m[1]))); enc {
	case "utf-8", "utf8", "us-ascii", "ascii":
		return nil
	default:
		return &UnsupportedEncodingError{Encoding: string(m[1])}
	}
}

// Extract lists the translatable spans of an XHTML (or NCX) document, ordered
// by position and never overlapping.
func Extract(doc []byte) ([]Segment, error) {
	if err := checkEncoding(doc); err != nil {
		return nil, err
	}
	d := newDecoder(doc)
	var (
		segs      []Segment
		depth     int
		skipDepth int
		inSeg     bool
		segStart  int
		segDepth  int
		segElem   string
		// lastEnd is where the last token seen inside the current segment
		// ended. On a malformed document the decoder closes elements on its
		// own, and the synthetic end tag it reports sits *after* whatever
		// triggered the closure — far past the real content. Ending the span
		// here instead keeps it inside what the element actually held; using
		// the reported position would swallow the rest of the file, and
		// replacing that span would truncate the chapter.
		lastEnd int
	)
	for {
		before := int(d.InputOffset())
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("octet %d : %w", before, err)
		}
		after := int(d.InputOffset())
		// A tolerant decoder invents end tags for a malformed document, and
		// the bytes it consumed while doing so belong to whatever triggered
		// the repair — not to the element being closed. Only a tag the source
		// really spells out can be trusted to bound a span.
		realClose := false

		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			name := strings.ToLower(t.Name.Local)
			if inSeg || skipDepth > 0 {
				break
			}
			if skipElements[name] {
				skipDepth = depth
				break
			}
			if blockElements[name] {
				inSeg, segStart, segDepth, segElem = true, after, depth, name
				lastEnd = after
			}
		case xml.EndElement:
			realClose = closesTag(doc[before:after], t.Name.Local)
			if inSeg && depth == segDepth {
				end := lastEnd
				if realClose && before > end {
					end = before
				}
				if seg, ok := makeSegment(doc, segStart, end, KindBlock, segElem); ok {
					segs = append(segs, seg)
				}
				inSeg = false
			}
			if skipDepth == depth {
				skipDepth = 0
			}
			depth--
		case xml.CharData:
			if inSeg || skipDepth > 0 {
				break
			}
			if seg, ok := makeSegment(doc, before, after, KindText, ""); ok {
				segs = append(segs, seg)
			}
		}
		if inSeg {
			if _, isEnd := tok.(xml.EndElement); !isEnd || realClose {
				lastEnd = after
			}
		}
	}
	if inSeg {
		return nil, fmt.Errorf("élément <%s> non refermé", segElem)
	}
	return segs, nil
}

// closesTag reports whether raw is the closing tag the decoder claims it is.
// A synthetic end tag either consumed nothing, or consumed the bytes of some
// other element's tag.
func closesTag(raw []byte, name string) bool {
	text := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(text, "</") || !strings.HasSuffix(text, ">") {
		return false
	}
	got := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "</"), ">"))
	if i := strings.IndexByte(got, ':'); i >= 0 {
		got = got[i+1:]
	}
	return strings.EqualFold(got, name)
}

func newDecoder(doc []byte) *xml.Decoder {
	d := xml.NewDecoder(bytes.NewReader(doc))
	// EPUB content documents routinely use HTML entities such as &nbsp; without
	// declaring a DTD, and some real-world books ship slightly malformed markup.
	// Being lenient here means translating them instead of refusing them.
	d.Strict = false
	d.AutoClose = xml.HTMLAutoClose
	d.Entity = xml.HTMLEntity
	d.CharsetReader = passthroughCharset
	return d
}

// passthroughCharset accepts the encodings that are byte-for-byte compatible
// with UTF-8 and hands the bytes back untransformed, so that the decoder's
// offsets keep pointing into the source file. Anything else is refused: a real
// transcoding would shift every offset the package relies on.
func passthroughCharset(charset string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		return input, nil
	}
	return nil, &UnsupportedEncodingError{Encoding: charset}
}

func makeSegment(doc []byte, start, end int, kind Kind, elem string) (Segment, bool) {
	if start < 0 || end > len(doc) || start >= end {
		return Segment{}, false
	}
	src := string(doc[start:end])
	if !hasLetter(src) {
		return Segment{}, false
	}
	return Segment{Start: start, End: end, Kind: kind, Source: src, Elem: elem}, true
}

func hasLetter(s string) bool {
	inTag := false
	inEntity := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case inTag:
			// Attribute values are not translated, so letters inside a tag do
			// not make a segment translatable on their own.
		case r == '&':
			inEntity = true
		case inEntity:
			if r == ';' {
				inEntity = false
			}
		case unicode.IsLetter(r):
			return true
		}
	}
	return false
}

// Apply splices translations back into the source document. translations must
// have exactly one entry per segment; an empty entry leaves that segment
// unchanged.
func Apply(doc []byte, segs []Segment, translations []string) ([]byte, error) {
	if len(segs) != len(translations) {
		return nil, fmt.Errorf("%d traductions reçues pour %d segments", len(translations), len(segs))
	}
	ordered := make([]int, len(segs))
	for i := range ordered {
		ordered[i] = i
	}
	sort.SliceStable(ordered, func(a, b int) bool { return segs[ordered[a]].Start < segs[ordered[b]].Start })

	var out bytes.Buffer
	out.Grow(len(doc))
	prev := 0
	for _, i := range ordered {
		s := segs[i]
		if s.Start < prev {
			return nil, fmt.Errorf("segments qui se chevauchent à l'octet %d", s.Start)
		}
		out.Write(doc[prev:s.Start])
		tr := translations[i]
		switch {
		case strings.TrimSpace(tr) == "":
			out.WriteString(s.Source)
		case !SafeForXML(tr):
			// Splicing this in would produce a file no reader can open. The
			// source is kept: the last line of defence before the book is
			// written to disk.
			out.WriteString(s.Source)
		case s.Kind == KindText:
			out.WriteString(NormaliseText(tr))
		case WellFormed(RepairAmpersands(tr)) != nil && WellFormed(s.Source) == nil:
			// Inline markup is spliced in raw, so a broken fragment would
			// break the document. Callers are expected to have checked this
			// already; Apply checks again because it is the last step before
			// the bytes become a book.
			out.WriteString(s.Source)
		default:
			out.WriteString(RepairAmpersands(tr))
		}
		prev = s.End
	}
	out.Write(doc[prev:])

	// The caller got its segments from Extract, so the input parsed. Whatever
	// comes out has to parse as well — otherwise the book would not open. A
	// source document too broken for its blocks to be replaced safely is
	// refused here rather than written to disk.
	result := out.Bytes()
	if _, err := Extract(result); err != nil {
		return nil, fmt.Errorf("le document ne serait plus lisible après réinjection : %w", err)
	}
	return result, nil
}

// RepairAmpersands escapes the ampersands that do not open a valid entity
// reference, and leaves alone the ones that do.
//
// A bare "&" is always an error in XML, and escaping it is its only possible
// reading — so a translation whose sole flaw is "Marks & Spencer" is worth
// mending rather than discarding along with the whole paragraph.
func RepairAmpersands(s string) string {
	if !strings.ContainsRune(s, '&') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		if s[i] != '&' {
			b.WriteByte(s[i])
			continue
		}
		if n := entityLength(s[i:]); n > 0 {
			b.WriteString(s[i : i+n])
			i += n - 1
			continue
		}
		b.WriteString("&amp;")
	}
	return b.String()
}

// SafeForXML reports whether a string can be placed inside an XML document: it
// must be valid UTF-8, and free of the code points XML 1.0 forbids — the
// control characters other than tab, newline and carriage return, and the two
// non-characters at the end of the basic plane.
//
// An EPUB that carries any of them is a file no reading system will open, so
// this is checked before anything a model returns reaches a book.
func SafeForXML(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if !validXMLRune(r) {
			return false
		}
	}
	return true
}

// validXMLRune reports whether a code point may appear in an XML 1.0 document,
// whether written directly or through a character reference.
func validXMLRune(r rune) bool {
	switch {
	case r == '\t' || r == '\n' || r == '\r':
		return true
	case r < 0x20:
		return false
	case r >= 0xD800 && r <= 0xDFFF:
		return false
	case r == 0xFFFE || r == 0xFFFF:
		return false
	case r > 0x10FFFF:
		return false
	}
	return true
}

// NormaliseText prepares a plain-text translation for insertion into an XML
// document.
//
// A text span is extracted as the raw bytes of the source, entities included,
// so a translation legitimately comes back carrying the "&amp;" it was given.
// Escaping that again would put "&amp;amp;" in front of the reader. So an
// entity reference that is already well formed is left alone, and everything
// else that XML would misread is escaped.
//
// xml.EscapeText is not used: it also rewrites newlines and tabs as character
// references, which churns the document for nothing.
func NormaliseText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '&':
			if n := entityLength(s[i:]); n > 0 {
				b.WriteString(s[i : i+n])
				i += n - 1
				continue
			}
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// entityLength returns the length of the entity reference starting at s, or 0
// when s does not start with one.
func entityLength(s string) int {
	if len(s) < 3 || s[0] != '&' {
		return 0
	}
	i := 1
	if s[i] == '#' {
		i++
		base := 10
		if i < len(s) && (s[i] == 'x' || s[i] == 'X') {
			i++
			base = 16
		}
		start := i
		for i < len(s) && isDigitFor(s[i], base) {
			i++
		}
		if i == start {
			return 0
		}
		// A reference is only valid if what it names is: "&#0;" reads as four
		// harmless characters but decodes to a code point XML forbids, and
		// would make the book unopenable.
		value, err := strconv.ParseInt(s[start:i], base, 64)
		if err != nil || !validXMLRune(rune(value)) {
			return 0
		}
	} else {
		start := i
		for i < len(s) && isNameByte(s[i]) {
			i++
		}
		if i == start {
			return 0
		}
	}
	if i < len(s) && s[i] == ';' {
		return i + 1
	}
	return 0
}

// isDigitFor reports whether c is a digit in the given base, 10 or 16.
func isDigitFor(c byte, base int) bool {
	if c >= '0' && c <= '9' {
		return true
	}
	return base == 16 && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F')
}

func isNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// strictDecoder validates model output. Unlike newDecoder it refuses unclosed
// tags and malformed attributes, because a broken fragment spliced into a
// chapter would corrupt the book. HTML entities stay allowed: EPUB prose is
// full of &nbsp; and &mdash;.
func strictDecoder(doc []byte) *xml.Decoder {
	d := xml.NewDecoder(bytes.NewReader(doc))
	d.Strict = true
	d.Entity = xml.HTMLEntity
	d.CharsetReader = passthroughCharset
	return d
}

// WellFormed reports whether a translated block segment can be spliced back
// without breaking the document. The fragment is parsed inside a synthetic root
// so that a bare run of inline markup is accepted.
func WellFormed(fragment string) error {
	d := strictDecoder([]byte("<tulipe-fragment>" + fragment + "</tulipe-fragment>"))
	for {
		_, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// Tags returns the multiset of element names of a fragment, used to check that
// a translation kept the markup it was given.
func Tags(fragment string) map[string]int {
	counts := map[string]int{}
	d := strictDecoder([]byte("<tulipe-fragment>" + fragment + "</tulipe-fragment>"))
	for {
		tok, err := d.Token()
		if err != nil {
			return counts
		}
		if se, ok := tok.(xml.StartElement); ok {
			name := strings.ToLower(se.Name.Local)
			if name != "tulipe-fragment" {
				counts[name]++
			}
		}
	}
}

// DocumentTitle extracts a human label from a content document: the first
// heading, or failing that the <title> element.
func DocumentTitle(doc []byte) string {
	d := newDecoder(doc)
	var (
		capture  string
		buf      strings.Builder
		fallback string
	)
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			switch name {
			case "h1", "h2", "h3", "title":
				capture, buf = name, strings.Builder{}
			}
		case xml.CharData:
			if capture != "" {
				buf.Write(t)
			}
		case xml.EndElement:
			name := strings.ToLower(t.Name.Local)
			if name != capture {
				continue
			}
			text := collapse(buf.String())
			capture = ""
			if text == "" {
				continue
			}
			if name == "title" {
				if fallback == "" {
					fallback = text
				}
				continue
			}
			return text
		}
	}
	return fallback
}

// navLinks maps href to link text for an EPUB 3 navigation document.
func navLinks(doc []byte) map[string]string {
	out := map[string]string{}
	d := newDecoder(doc)
	var (
		href string
		buf  strings.Builder
	)
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if strings.ToLower(t.Name.Local) != "a" {
				continue
			}
			href, buf = "", strings.Builder{}
			for _, a := range t.Attr {
				if strings.ToLower(a.Name.Local) == "href" {
					href = a.Value
				}
			}
		case xml.CharData:
			if href != "" {
				buf.Write(t)
			}
		case xml.EndElement:
			if strings.ToLower(t.Name.Local) != "a" || href == "" {
				continue
			}
			if text := collapse(buf.String()); text != "" {
				if _, seen := out[href]; !seen {
					out[href] = text
				}
			}
			href = ""
		}
	}
	return out
}

// elemSpan locates one element in the raw bytes of a document.
type elemSpan struct {
	tagStart, tagEnd     int    // byte range of the start tag
	innerStart, innerEnd int    // byte range of the character data it wraps
	selfClosing          bool   // written as <name/>, so it wraps nothing
	name                 string // qualified name exactly as spelled in the source
}

// elementSpans locates every element with the given local name, keeping enough
// information to rewrite it in place — including when it was written as a
// self-closing tag, which has no character data to replace.
func elementSpans(doc []byte, local string) []elemSpan {
	d := newDecoder(doc)
	var (
		out     []elemSpan
		depth   int
		pending = -1
		want    int
	)
	for {
		before := int(d.InputOffset())
		tok, err := d.Token()
		if err != nil {
			break
		}
		after := int(d.InputOffset())
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if pending >= 0 || !strings.EqualFold(t.Name.Local, local) {
				continue
			}
			raw := strings.TrimRight(string(doc[before:after]), " \t\r\n")
			span := elemSpan{
				tagStart: before, tagEnd: after,
				innerStart: after, innerEnd: after,
				name: qualifiedName(raw),
			}
			if strings.HasSuffix(raw, "/>") {
				// The decoder still emits a matching EndElement, so the depth
				// bookkeeping stays balanced; there is simply nothing to wait for.
				span.selfClosing = true
				out = append(out, span)
				continue
			}
			out = append(out, span)
			pending, want = len(out)-1, depth
		case xml.EndElement:
			if pending >= 0 && depth == want {
				out[pending].innerEnd = before
				pending = -1
			}
			depth--
		}
	}
	if pending >= 0 {
		// The element was never closed; leaving it out is safer than rewriting
		// a range whose end we do not know.
		out = out[:pending]
	}
	return out
}

// qualifiedName reads the element name out of a raw start tag, prefix included.
func qualifiedName(raw string) string {
	raw = strings.TrimPrefix(raw, "<")
	i := strings.IndexAny(raw, " \t\r\n/>")
	if i < 0 {
		return raw
	}
	return raw[:i]
}

// rewriteText replaces the character data of an element, turning a self-closing
// tag into a pair of tags when it has to.
func (s elemSpan) rewriteText(doc []byte, text string) (start, end int, replacement string) {
	if !s.selfClosing {
		return s.innerStart, s.innerEnd, text
	}
	open := strings.TrimRight(string(doc[s.tagStart:s.tagEnd]), " \t\r\n")
	open = strings.TrimRight(strings.TrimSuffix(open, "/>"), " \t\r\n")
	return s.tagStart, s.tagEnd, open + ">" + text + "</" + s.name + ">"
}

func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// SetDocumentLanguage rewrites the lang and xml:lang attributes of the root
// <html> element of a content document. Reading systems rely on them for
// hyphenation and text-to-speech, so a translated chapter that still claims to
// be English is a real defect.
//
// The rewrite is confined to the byte range of that one start tag; the rest of
// the document is copied untouched.
func SetDocumentLanguage(doc []byte, code string) []byte {
	if code == "" {
		return doc
	}
	start, end, attrs, ok := rootElement(doc, "html")
	if !ok {
		return doc
	}
	tag := string(doc[start:end])
	rewritten := tag
	for _, name := range []string{"xml:lang", "lang"} {
		if _, present := attrs[name]; !present {
			continue
		}
		re := langAttrPattern(name)
		rewritten = re.ReplaceAllString(rewritten, "${1}"+xmlAttrEscape(code)+"${3}")
	}
	if rewritten == tag {
		return doc
	}
	out := make([]byte, 0, len(doc)+len(rewritten)-len(tag))
	out = append(out, doc[:start]...)
	out = append(out, rewritten...)
	out = append(out, doc[end:]...)
	return out
}

func langAttrPattern(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(\b` + regexp.QuoteMeta(name) + `\s*=\s*")([^"]*)(")`)
}

// xmlTextEscape escapes a value written as element text. Callers validate the
// language code they pass, but a code carrying "<" or "&" would otherwise turn
// the package document into something no reader can open.
func xmlTextEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func xmlAttrEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", `"`, "&quot;").Replace(s)
}

// rootElement locates the byte range of the first start tag with the given
// local name, and returns its attributes.
func rootElement(doc []byte, local string) (start, end int, attrs map[string]string, ok bool) {
	d := newDecoder(doc)
	for {
		before := int(d.InputOffset())
		tok, err := d.Token()
		if err != nil {
			return 0, 0, nil, false
		}
		after := int(d.InputOffset())
		se, isStart := tok.(xml.StartElement)
		if !isStart || !strings.EqualFold(se.Name.Local, local) {
			continue
		}
		attrs = map[string]string{}
		for _, a := range se.Attr {
			name := strings.ToLower(a.Name.Local)
			if a.Name.Space != "" {
				name = strings.ToLower(a.Name.Space) + ":" + name
			}
			attrs[name] = a.Value
		}
		// The decoder resolves the xml: prefix to its namespace URI, so check
		// the raw tag for the prefixed spelling too.
		if strings.Contains(strings.ToLower(string(doc[before:after])), "xml:lang") {
			attrs["xml:lang"] = ""
		}
		return before, after, attrs, true
	}
}

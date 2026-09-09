// Package epub reads and writes EPUB 2 and EPUB 3 containers.
//
// The package deliberately keeps every byte of the original archive. A book is
// loaded fully in memory, chapters are replaced individually, and everything
// else — images, fonts, stylesheets, metadata — is copied over untouched.
//
// References:
//   - EPUB 3.3, W3C Recommendation: https://www.w3.org/TR/epub-33/
//   - Open Container Format 3.0: https://www.w3.org/TR/epub-33/#sec-ocf
package epub

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/blkmlo/tulipe/internal/i18n"
)

// MediaTypeXHTML is the media type every EPUB content document must declare.
const MediaTypeXHTML = "application/xhtml+xml"

// MaxUncompressedSize caps what a single archive may expand to. A book is a few
// megabytes; a zip crafted to expand to gigabytes would otherwise exhaust the
// machine's memory before anything noticed. It is a variable so that tests can
// exercise the limit without building half a gigabyte.
var MaxUncompressedSize int64 = 512 << 20 // 512 MiB

// File is a single entry of the EPUB zip archive.
type File struct {
	Name     string
	Data     []byte
	Method   uint16
	Modified time.Time
}

// Item is a manifest entry of the OPF package document.
type Item struct {
	ID         string
	Href       string // resolved to an archive path
	MediaType  string
	Properties string
}

// Chapter is one spine item pointing at an XHTML content document.
type Chapter struct {
	Index int    // position in the spine, 0-based
	ID    string // manifest id
	Path  string // path inside the archive
	Title string // best-effort human label
	Size  int    // size of the source document, in bytes
}

// Book is an EPUB archive loaded in memory.
type Book struct {
	Path     string
	Files    []File
	index    map[string]int
	OPFPath  string
	Version  string
	Title    string
	Language string
	Items    []Item
	Chapters []Chapter
	NCXPath  string
	NavPath  string
}

type containerXML struct {
	Rootfiles []struct {
		FullPath  string `xml:"full-path,attr"`
		MediaType string `xml:"media-type,attr"`
	} `xml:"rootfiles>rootfile"`
}

type packageXML struct {
	Version  string `xml:"version,attr"`
	Metadata struct {
		Titles    []string `xml:"title"`
		Languages []string `xml:"language"`
	} `xml:"metadata"`
	Manifest struct {
		Items []struct {
			ID         string `xml:"id,attr"`
			Href       string `xml:"href,attr"`
			MediaType  string `xml:"media-type,attr"`
			Properties string `xml:"properties,attr"`
		} `xml:"item"`
	} `xml:"manifest"`
	Spine struct {
		TOC      string `xml:"toc,attr"`
		ItemRefs []struct {
			IDRef  string `xml:"idref,attr"`
			Linear string `xml:"linear,attr"`
		} `xml:"itemref"`
	} `xml:"spine"`
}

// Open loads an EPUB file from disk.
func Open(name string) (*Book, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	b, err := Parse(data)
	if err != nil {
		return nil, err
	}
	b.Path = name
	return b, nil
}

// Parse loads an EPUB from the raw bytes of the archive.
func Parse(data []byte) (*Book, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf(i18n.T("epub.err.not-zip"), err)
	}
	b := &Book{index: map[string]int{}}
	var total int64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf(i18n.T("epub.err.open-entry"), f.Name, err)
		}
		// Read one byte past the remaining budget so an oversized entry is
		// caught rather than silently truncated.
		remaining := MaxUncompressedSize - total
		content, err := io.ReadAll(io.LimitReader(rc, remaining+1))
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf(i18n.T("epub.err.read-entry"), f.Name, err)
		}
		total += int64(len(content))
		if total > MaxUncompressedSize {
			return nil, fmt.Errorf(i18n.T("epub.err.archive-too-large"), MaxUncompressedSize>>20)
		}
		b.index[f.Name] = len(b.Files)
		b.Files = append(b.Files, File{
			Name:     f.Name,
			Data:     content,
			Method:   f.Method,
			Modified: f.Modified,
		})
	}
	if err := b.parseStructure(); err != nil {
		return nil, err
	}
	return b, nil
}

// Read returns the content of an archive entry.
func (b *Book) Read(name string) ([]byte, bool) {
	i, ok := b.index[name]
	if !ok {
		return nil, false
	}
	return b.Files[i].Data, true
}

// Replace overwrites the content of an existing archive entry.
func (b *Book) Replace(name string, data []byte) bool {
	i, ok := b.index[name]
	if !ok {
		return false
	}
	b.Files[i].Data = data
	return true
}

func (b *Book) parseStructure() error {
	raw, ok := b.Read("META-INF/container.xml")
	if !ok {
		return errors.New(i18n.T("epub.err.no-container"))
	}
	var c containerXML
	if err := xml.Unmarshal(raw, &c); err != nil {
		return fmt.Errorf(i18n.T("epub.err.parse-container"), err)
	}
	for _, rf := range c.Rootfiles {
		if rf.FullPath != "" {
			b.OPFPath = rf.FullPath
			break
		}
	}
	if b.OPFPath == "" {
		return errors.New(i18n.T("epub.err.no-rootfile"))
	}
	opfRaw, ok := b.Read(b.OPFPath)
	if !ok {
		return fmt.Errorf(i18n.T("epub.err.opf-declared-absent"), b.OPFPath)
	}
	var p packageXML
	if err := xml.Unmarshal(opfRaw, &p); err != nil {
		return fmt.Errorf(i18n.T("epub.err.parse-opf"), b.OPFPath, err)
	}
	b.Version = p.Version
	if len(p.Metadata.Titles) > 0 {
		b.Title = strings.TrimSpace(p.Metadata.Titles[0])
	}
	if len(p.Metadata.Languages) > 0 {
		b.Language = strings.TrimSpace(p.Metadata.Languages[0])
	}

	base := path.Dir(b.OPFPath)
	byID := map[string]Item{}
	for _, it := range p.Manifest.Items {
		item := Item{
			ID:         it.ID,
			Href:       resolve(base, it.Href),
			MediaType:  it.MediaType,
			Properties: it.Properties,
		}
		b.Items = append(b.Items, item)
		byID[it.ID] = item
		if it.MediaType == "application/x-dtbncx+xml" {
			b.NCXPath = item.Href
		}
		if strings.Contains(it.Properties, "nav") {
			b.NavPath = item.Href
		}
	}
	if b.NCXPath == "" && p.Spine.TOC != "" {
		if it, ok := byID[p.Spine.TOC]; ok {
			b.NCXPath = it.Href
		}
	}

	labels := b.tocLabels()
	for _, ref := range p.Spine.ItemRefs {
		it, ok := byID[ref.IDRef]
		if !ok || it.MediaType != MediaTypeXHTML {
			continue
		}
		doc, ok := b.Read(it.Href)
		if !ok {
			continue
		}
		ch := Chapter{
			Index: len(b.Chapters),
			ID:    it.ID,
			Path:  it.Href,
			Title: labels[it.Href],
			Size:  len(doc),
		}
		if ch.Title == "" {
			ch.Title = DocumentTitle(doc)
		}
		if ch.Title == "" {
			ch.Title = i18n.T("epub.section", ch.Index+1)
		}
		b.Chapters = append(b.Chapters, ch)
	}
	if len(b.Chapters) == 0 {
		return fmt.Errorf(i18n.T("epub.err.empty-spine"), b.OPFPath)
	}
	return nil
}

// resolve turns a manifest href into an archive path.
func resolve(base, href string) string {
	if i := strings.IndexAny(href, "#?"); i >= 0 {
		href = href[:i]
	}
	if decoded, err := url.PathUnescape(href); err == nil {
		href = decoded
	}
	if base == "." || base == "" {
		return path.Clean(href)
	}
	return path.Clean(path.Join(base, href))
}

type ncxXML struct {
	NavPoints []ncxPoint `xml:"navMap>navPoint"`
}

type ncxPoint struct {
	Label   string `xml:"navLabel>text"`
	Content struct {
		Src string `xml:"src,attr"`
	} `xml:"content"`
	Children []ncxPoint `xml:"navPoint"`
}

// tocLabels maps archive paths to the label used by the table of contents.
func (b *Book) tocLabels() map[string]string {
	labels := map[string]string{}
	if b.NCXPath != "" {
		if raw, ok := b.Read(b.NCXPath); ok {
			var n ncxXML
			if err := xml.Unmarshal(raw, &n); err == nil {
				base := path.Dir(b.NCXPath)
				var walk func([]ncxPoint)
				walk = func(points []ncxPoint) {
					for _, pt := range points {
						p := resolve(base, pt.Content.Src)
						if label := strings.TrimSpace(pt.Label); label != "" {
							if _, seen := labels[p]; !seen {
								labels[p] = label
							}
						}
						walk(pt.Children)
					}
				}
				walk(n.NavPoints)
			}
		}
	}
	if b.NavPath != "" {
		if raw, ok := b.Read(b.NavPath); ok {
			base := path.Dir(b.NavPath)
			for href, label := range navLinks(raw) {
				p := resolve(base, href)
				if _, seen := labels[p]; !seen {
					labels[p] = label
				}
			}
		}
	}
	return labels
}

// WriteFile serialises the book to disk.
func (b *Book) WriteFile(name string) error {
	var buf bytes.Buffer
	if err := b.Encode(&buf); err != nil {
		return err
	}
	return os.WriteFile(name, buf.Bytes(), 0o644)
}

// Encode serialises the book as an OCF zip container.
//
// The "mimetype" entry is written first and stored uncompressed, as required by
// OCF 3.0 §4.2.
func (b *Book) Encode(w io.Writer) error {
	zw := zip.NewWriter(w)
	written := map[string]bool{}

	writeEntry := func(f File, method uint16) error {
		h := &zip.FileHeader{Name: f.Name, Method: method}
		if !f.Modified.IsZero() {
			h.Modified = f.Modified
		}
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = fw.Write(f.Data)
		return err
	}

	if i, ok := b.index["mimetype"]; ok {
		if err := writeEntry(b.Files[i], zip.Store); err != nil {
			return err
		}
		written["mimetype"] = true
	} else {
		if err := writeEntry(File{Name: "mimetype", Data: []byte("application/epub+zip")}, zip.Store); err != nil {
			return err
		}
		written["mimetype"] = true
	}

	for _, f := range b.Files {
		if written[f.Name] {
			continue
		}
		if err := writeEntry(f, zip.Deflate); err != nil {
			return err
		}
		written[f.Name] = true
	}
	return zw.Close()
}

// SetLanguage rewrites the <dc:language> element of the package document.
//
// The change is applied to the raw OPF bytes so that the rest of the metadata,
// including its namespace declarations, is preserved exactly.
func (b *Book) SetLanguage(code string) error {
	if code == "" {
		return nil
	}
	raw, ok := b.Read(b.OPFPath)
	if !ok {
		return fmt.Errorf(i18n.T("epub.err.opf-absent"), b.OPFPath)
	}
	spans := elementSpans(raw, "language")
	if len(spans) == 0 {
		return fmt.Errorf(i18n.T("epub.err.no-language-element"), b.OPFPath)
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].tagStart < spans[j].tagStart })
	var out bytes.Buffer
	prev := 0
	for _, s := range spans {
		start, end, replacement := s.rewriteText(raw, xmlTextEscape(code))
		if start < prev {
			continue
		}
		out.Write(raw[prev:start])
		out.WriteString(replacement)
		prev = end
	}
	out.Write(raw[prev:])
	b.Replace(b.OPFPath, out.Bytes())
	b.Language = code
	return nil
}

// TranslatableDocuments lists every archive path whose text should be sent to a
// translator: the spine chapters plus the navigation documents that carry the
// chapter titles the reader will see.
func (b *Book) TranslatableDocuments() []string {
	seen := map[string]bool{}
	var out []string
	for _, ch := range b.Chapters {
		if !seen[ch.Path] {
			seen[ch.Path] = true
			out = append(out, ch.Path)
		}
	}
	for _, p := range []string{b.NavPath, b.NCXPath} {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

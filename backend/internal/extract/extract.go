// Package extract pulls plain text out of uploaded files so they can be
// searched. Uploads are untrusted, so every format is read with hard limits:
// output size, pages, archive entries and uncompressed bytes (zip bombs),
// and a recover for parser panics on malformed files.
package extract

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

const (
	// MaxChars is the most text kept per file.
	MaxChars = 500_000
	// maxPDFPages bounds work on huge or hostile PDFs.
	maxPDFPages = 500
	// maxEntryBytes and maxArchiveBytes bound what is decompressed from
	// Office files, so a small upload can't expand into gigabytes.
	maxEntryBytes   = 30 << 20
	maxArchiveBytes = 60 << 20
	maxEntries      = 2000
)

var (
	// ErrUnsupported means the type has no text to extract here (images,
	// old binary Office formats, zip archives).
	ErrUnsupported = errors.New("text extraction is not supported for this file type")
	ErrEncrypted   = errors.New("the file is password-protected")
)

// Text returns the plain text of a file, chosen by its extension.
func Text(name string, data []byte) (text string, err error) {
	defer func() {
		if p := recover(); p != nil { // malformed input can panic inside parsers
			text, err = "", fmt.Errorf("could not read the file: %v", p)
		}
	}()
	switch strings.ToLower(path.Ext(name)) {
	case ".txt", ".md", ".csv":
		return clean(string(bytes.ToValidUTF8(data, nil))), nil
	case ".pdf":
		return pdfText(data)
	case ".docx":
		return officeText(data, func(n string) bool { return n == "word/document.xml" })
	case ".pptx":
		return officeText(data, func(n string) bool { return strings.HasPrefix(n, "ppt/slides/slide") && strings.HasSuffix(n, ".xml") })
	case ".xlsx":
		return officeText(data, func(n string) bool {
			return n == "xl/sharedStrings.xml" || (strings.HasPrefix(n, "xl/worksheets/sheet") && strings.HasSuffix(n, ".xml"))
		})
	default:
		return "", ErrUnsupported
	}
}

func pdfText(data []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "encrypt") {
			return "", ErrEncrypted
		}
		return "", fmt.Errorf("could not read the PDF: %w", err)
	}
	var b strings.Builder
	pages := r.NumPage()
	if pages > maxPDFPages {
		pages = maxPDFPages
	}
	for i := 1; i <= pages && b.Len() < MaxChars; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		t, err := p.GetPlainText(nil)
		if err != nil {
			continue // keep what the other pages give
		}
		b.WriteString(t)
		b.WriteString("\n")
	}
	return clean(b.String()), nil
}

// officeText reads the text runs of the XML parts of a .docx/.pptx/.xlsx.
func officeText(data []byte, want func(name string) bool) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("could not open the document: %w", err)
	}
	if len(zr.File) > maxEntries {
		return "", errors.New("the document has too many parts")
	}
	var parts []*zip.File
	for _, f := range zr.File {
		if want(f.Name) {
			parts = append(parts, f)
		}
	}
	sort.Slice(parts, func(i, j int) bool { return naturalLess(parts[i].Name, parts[j].Name) }) // slide2 before slide10

	var b strings.Builder
	var total int64
	for _, f := range parts {
		if f.UncompressedSize64 > maxEntryBytes {
			return "", errors.New("the document is too large to read")
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		// The size in the header can lie; also limit what is actually read.
		raw, err := io.ReadAll(io.LimitReader(rc, maxEntryBytes+1))
		rc.Close()
		if err != nil {
			return "", err
		}
		total += int64(len(raw))
		if len(raw) > maxEntryBytes || total > maxArchiveBytes {
			return "", errors.New("the document is too large to read")
		}
		xmlText(raw, &b)
		if b.Len() >= MaxChars {
			break
		}
	}
	return clean(b.String()), nil
}

// xmlText appends the character data of <t> elements (w:t, a:t, and the
// spreadsheet t/v), starting a new line at paragraph, row and cell ends.
func xmlText(raw []byte, b *strings.Builder) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	dec.Strict = false
	inText := false
	for b.Len() < MaxChars {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		switch t := tok.(type) {
		case xml.StartElement:
			inText = t.Name.Local == "t" || t.Name.Local == "v"
			if t.Name.Local == "tab" {
				b.WriteString("\t")
			}
		case xml.EndElement:
			inText = false
			switch t.Name.Local {
			case "p", "si", "row", "br":
				b.WriteString("\n")
			case "c":
				b.WriteString("\t")
			}
		case xml.CharData:
			if inText {
				b.Write(t)
			}
		}
	}
}

var (
	spaces     = regexp.MustCompile(`[ \t\x{00a0}]+`)
	blankLines = regexp.MustCompile(`\n{3,}`)
	digits     = regexp.MustCompile(`\d+`)
)

// clean normalises whitespace, drops control characters and caps the size.
func clean(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r == '\r':
			return '\n'
		case r < 0x20 || r == 0x7f || r == utf8.RuneError:
			return -1
		}
		return r
	}, s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(spaces.ReplaceAllString(l, " "))
	}
	s = strings.TrimSpace(blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
	if utf8.RuneCountInString(s) > MaxChars {
		s = string([]rune(s)[:MaxChars])
	}
	return s
}

// naturalLess orders "slide2.xml" before "slide10.xml".
func naturalLess(a, b string) bool {
	pad := func(s string) string {
		return digits.ReplaceAllStringFunc(s, func(d string) string { return fmt.Sprintf("%08s", d) })
	}
	return pad(a) < pad(b)
}

// Chunks splits text into pieces of about size characters, preferring
// paragraph and sentence boundaries, with a little overlap so a phrase
// split across two chunks is still found.
func Chunks(text string, size, overlap int) []string {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	var out []string
	for start := 0; start < len(runes); {
		end := start + size
		if end >= len(runes) {
			out = append(out, strings.TrimSpace(string(runes[start:])))
			break
		}
		// Look back for a good break within the last fifth of the chunk.
		cut := end
		for i := end; i > end-size/5; i-- {
			if runes[i] == '\n' {
				cut = i
				break
			}
			if cut == end && (runes[i] == '.' || runes[i] == '?' || runes[i] == '!') && i+1 < len(runes) && runes[i+1] == ' ' {
				cut = i + 1
			}
		}
		out = append(out, strings.TrimSpace(string(runes[start:cut])))
		next := cut - overlap
		if next <= start {
			next = cut
		}
		start = next
	}
	return out
}

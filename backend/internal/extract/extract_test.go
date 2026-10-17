package extract

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(content))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// minimalPDF builds a valid one-page PDF showing text.
func minimalPDF(text string) []byte {
	stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", text)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

func TestPlainText(t *testing.T) {
	got, err := Text("notes.TXT", []byte("Line one\r\nবাংলা   text\x00\x07\n\n\n\nEnd\xff"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "Line one\nবাংলা text\n\nEnd" {
		t.Errorf("got %q", got)
	}
}

func TestOfficeFormats(t *testing.T) {
	docx := zipOf(t, map[string]string{
		"word/document.xml": `<w:document xmlns:w="w"><w:body><w:p><w:r><w:t>Car insurance</w:t></w:r><w:r><w:t xml:space="preserve"> policy</w:t></w:r></w:p><w:p><w:r><w:t>Number 4455</w:t></w:r></w:p></w:body></w:document>`,
		"word/styles.xml":   `<w:styles><w:t>IGNORED</w:t></w:styles>`,
	})
	if got, err := Text("policy.docx", docx); err != nil || got != "Car insurance policy\nNumber 4455" {
		t.Errorf("docx: %q %v", got, err)
	}

	xlsx := zipOf(t, map[string]string{
		"xl/sharedStrings.xml":     `<sst><si><t>Rent</t></si><si><t>Electricity</t></si></sst>`,
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row><c t="s"><v>0</v></c><c><v>1200</v></c></row></sheetData></worksheet>`,
	})
	if got, err := Text("budget.xlsx", xlsx); err != nil || !strings.Contains(got, "Rent") || !strings.Contains(got, "Electricity") || !strings.Contains(got, "1200") {
		t.Errorf("xlsx: %q %v", got, err)
	}

	pptx := zipOf(t, map[string]string{
		"ppt/slides/slide10.xml": `<p:sld><a:p><a:r><a:t>Ten</a:t></a:r></a:p></p:sld>`,
		"ppt/slides/slide2.xml":  `<p:sld><a:p><a:r><a:t>Two</a:t></a:r></a:p></p:sld>`,
		"ppt/slides/slide1.xml":  `<p:sld><a:p><a:r><a:t>One</a:t></a:r></a:p></p:sld>`,
	})
	if got, err := Text("deck.pptx", pptx); err != nil || got != "One\nTwo\nTen" {
		t.Errorf("pptx (slides in order): %q %v", got, err)
	}
}

func TestZipBombIsRefused(t *testing.T) {
	// 31 MB of zeros compresses to a few KB.
	bomb := zipOf(t, map[string]string{"word/document.xml": strings.Repeat("\x00", maxEntryBytes+(1<<20))})
	if len(bomb) > 1<<20 {
		t.Fatalf("test archive too big: %d", len(bomb))
	}
	if _, err := Text("bomb.docx", bomb); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("got %v", err)
	}
	if _, err := Text("broken.docx", []byte("not a zip")); err == nil {
		t.Error("a broken document must be an error")
	}
}

func TestPDF(t *testing.T) {
	got, err := Text("policy.pdf", minimalPDF("Car insurance policy 4455"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Car insurance policy 4455") {
		t.Errorf("got %q", got)
	}
	for _, bad := range [][]byte{[]byte("%PDF-1.4 garbage"), {}, bytes.Repeat([]byte("%PDF"), 1000)} {
		if _, err := Text("bad.pdf", bad); err == nil {
			t.Errorf("malformed PDF (%d bytes) must be an error, not a panic", len(bad))
		}
	}
}

func TestUnsupported(t *testing.T) {
	for _, name := range []string{"photo.png", "scan.jpg", "old.doc", "archive.zip", "noext"} {
		if _, err := Text(name, []byte("x")); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestChunks(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&b, "Sentence number %d is here. ", i)
		if i%7 == 0 {
			b.WriteString("\n")
		}
	}
	text := b.String()
	chunks := Chunks(text, 1500, 150)
	if len(chunks) < 6 {
		t.Fatalf("got %d chunks", len(chunks))
	}
	for i, c := range chunks {
		if n := utf8.RuneCountInString(c); n > 1500 || n == 0 {
			t.Errorf("chunk %d has %d chars", i, n)
		}
	}
	if !strings.Contains(chunks[len(chunks)-1], "Sentence number 399") {
		t.Error("the end of the text is lost")
	}
	if Chunks("", 100, 10) != nil || len(Chunks("short", 100, 10)) != 1 {
		t.Error("edge cases")
	}
}

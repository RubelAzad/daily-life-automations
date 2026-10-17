//go:build integration

package ai

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/services"
	"lifeos-ai/backend/internal/storage"
)

func testPDF(text string) []byte {
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

func testDocx(text string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("word/document.xml")
	fmt.Fprintf(f, `<w:document xmlns:w="w"><w:body><w:p><w:r><w:t>%s</w:t></w:r></w:p></w:body></w:document>`, text)
	_ = w.Close()
	return buf.Bytes()
}

type searchFixture struct {
	fixture
	notes  services.NoteService
	search services.SearchService
}

func newSearchFixture(t *testing.T) searchFixture {
	t.Helper()
	f := newFixture(t)
	store := storage.LocalStore{Root: t.TempDir()}
	notesRepo, files := repository.NoteRepository{DB: testDB}, repository.FileRepository{DB: testDB}
	search := services.SearchService{Search: repository.SearchRepository{DB: testDB}, Notes: notesRepo, Files: files, Store: store}
	notes := services.NoteService{
		Notes: notesRepo, Files: files, Projects: repository.ProjectRepository{DB: testDB}, Reminders: repository.ReminderRepository{DB: testDB},
		Store: store, MaxUploadBytes: 20 << 20, Index: search,
	}
	return searchFixture{fixture: f, notes: notes, search: search}
}

func (f searchFixture) note(t *testing.T, title, body string) models.Note {
	t.Helper()
	n, err := f.notes.Create(f.ctx, f.user, services.CreateNoteInput{Title: title, Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// upload adds a file and waits for its background text extraction.
func (f searchFixture) upload(t *testing.T, noteID uint64, name string, data []byte) models.File {
	t.Helper()
	file, err := f.notes.Upload(f.ctx, f.user, noteID, name, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		got, err := repository.FileRepository{DB: testDB}.Get(f.ctx, f.user, file.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.TextStatus != models.TextPending && got.TextStatus != models.TextProcessing {
			return got
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("file %s was never indexed", name)
	return file
}

func (f searchFixture) find(t *testing.T, q string) []services.SearchResult {
	t.Helper()
	r, err := f.search.Query(f.ctx, f.user, q, 20)
	if err != nil {
		t.Fatalf("%q: %v", q, err)
	}
	return r
}

func TestSearchNotesAndFiles(t *testing.T) {
	f := newSearchFixture(t)
	car := f.note(t, "Car insurance", "Policy number ZX-4455 with City Insurance. Renew every March.")
	f.note(t, "Shopping", "Milk, eggs, bread")

	for _, q := range []string{"insurance", "insur", "Where is my car insurance policy?", "MARCH"} {
		r := f.find(t, q)
		if len(r) == 0 || r[0].NoteID != car.ID || r[0].FileID != nil {
			t.Errorf("%q: %+v", q, r)
		}
	}
	if r := f.find(t, "zx"); len(r) != 1 || !strings.Contains(r[0].Snippet, "ZX-4455") {
		t.Errorf("short query falls back to substring match: %+v", r)
	}
	if r := f.find(t, `+insurance -car "x" (y) @3 ~ * < > DROP TABLE`); len(r) == 0 {
		t.Error("boolean operators must be stripped, not break the query")
	}

	// Attachments: PDF and Word are read; images are marked unsupported.
	scans := f.note(t, "Scans", "")
	pdf := f.upload(t, scans.ID, "fridge-warranty.pdf", testPDF("Fridge warranty 9911 valid until 2028"))
	if pdf.TextStatus != models.TextReady || pdf.TextChars == nil || *pdf.TextChars == 0 {
		t.Fatalf("pdf: %+v", pdf)
	}
	docx := f.upload(t, scans.ID, "lease.docx", testDocx("Apartment lease signed with Mr Rahman"))
	if docx.TextStatus != models.TextReady {
		t.Fatalf("docx: %+v", docx)
	}
	png := f.upload(t, scans.ID, "photo.png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"))
	if png.TextStatus != models.TextUnsupported || png.TextError == nil {
		t.Errorf("png: %+v", png)
	}

	r := f.find(t, "warranty 9911")
	if len(r) == 0 || r[0].FileID == nil || *r[0].FileID != pdf.ID || deref(r[0].FileName) != "fridge-warranty.pdf" || r[0].NoteTitle != "Scans" {
		t.Errorf("pdf search: %+v", r)
	}
	if r := f.find(t, "rahman lease"); len(r) == 0 || *r[0].FileID != docx.ID {
		t.Errorf("docx search: %+v", r)
	}

	// Edits re-index; archived notes and other users' data are not found.
	newBody := "Policy moved to Green Insurance"
	if _, err := f.notes.Update(f.ctx, f.user, car.ID, services.UpdateNoteInput{Body: services.Optional[string]{Set: true, Value: newBody}}); err != nil {
		t.Fatal(err)
	}
	if r := f.find(t, "green"); len(r) != 1 {
		t.Errorf("edited text: %+v", r)
	}
	if r := f.find(t, "4455"); len(r) != 0 {
		t.Errorf("old text should be gone: %+v", r)
	}
	other := newSearchFixture(t)
	if r := other.find(t, "warranty"); len(r) != 0 {
		t.Errorf("other user found: %+v", r)
	}
	if _, err := f.notes.Update(f.ctx, f.user, scans.ID, services.UpdateNoteInput{Archived: services.Optional[bool]{Set: true, Value: true}}); err != nil {
		t.Fatal(err)
	}
	if r := f.find(t, "warranty"); len(r) != 0 {
		t.Errorf("archived note found: %+v", r)
	}

	// Deleting a file removes its text from the index.
	if _, err := f.notes.Update(f.ctx, f.user, scans.ID, services.UpdateNoteInput{Archived: services.Optional[bool]{Set: true, Value: false}}); err != nil {
		t.Fatal(err)
	}
	if err := f.notes.DeleteFile(f.ctx, f.user, pdf.ID); err != nil {
		t.Fatal(err)
	}
	if r := f.find(t, "warranty"); len(r) != 0 {
		t.Errorf("deleted file still found: %+v", r)
	}
}

func TestSearchDocumentsToolAndBackfill(t *testing.T) {
	f := newSearchFixture(t)
	f.note(t, "Router manual", "Reset the router by holding the button for 10 seconds.")

	// A note saved without the indexer (e.g. before this feature) is picked
	// up by the worker's backfill.
	raw := repository.NoteRepository{DB: testDB}
	body := "The plumber Karim charges 1500 taka per visit."
	id, err := raw.Create(f.ctx, models.Note{UserID: f.user, Title: "Plumber", Kind: "note", Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	if r := f.find(t, "plumber"); len(r) != 0 {
		t.Fatal("not indexed yet")
	}
	if _, notes, err := f.search.IndexPending(f.ctx, 500); err != nil || notes < 1 {
		t.Fatalf("backfill: %d %v", notes, err)
	}
	if r := f.find(t, "plumber"); len(r) != 1 || r[0].NoteID != id {
		t.Errorf("after backfill: %+v", r)
	}

	tools := BuiltinTools(Deps{Search: f.search})
	exec := Executor{Tools: tools, Actions: f.exec.Actions, Settings: f.exec.Settings}
	res, err := exec.Propose(f.ctx, f.user, Origin{Source: "test"}, "search_documents", json.RawMessage(`{"query":"router reset"}`))
	if err != nil || res.Status != models.AIStatusSucceeded {
		t.Fatalf("tool: %+v %v", res, err)
	}
	out, _ := json.Marshal(res.Output)
	if !strings.Contains(string(out), `"note_title":"Router manual"`) || !strings.Contains(string(out), "holding the button") {
		t.Errorf("tool output: %s", out)
	}
	if res, _ := exec.Propose(f.ctx, f.user, Origin{Source: "test"}, "search_documents", json.RawMessage(`{"query":""}`)); res.Status != models.AIStatusFailed {
		t.Errorf("empty query: %+v", res)
	}
}

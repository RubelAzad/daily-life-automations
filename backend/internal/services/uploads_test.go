package services

import "testing"

func TestCheckUpload(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	pdf := []byte("%PDF-1.7\n")
	html := []byte("<!DOCTYPE html><html><script>alert(1)</script></html>")
	zip := []byte("PK\x03\x04\x14\x00\x06\x00")

	cases := []struct {
		name    string
		head    []byte
		wantCT  string
		wantErr bool
	}{
		{"receipt.pdf", pdf, "application/pdf", false},
		{"Photo.PNG", png, "image/png", false},
		{"notes.txt", []byte("hello"), "text/plain; charset=utf-8", false},
		{"report.docx", zip, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", false},
		{"page.html", html, "", true},             // not allowed
		{"image.svg", []byte("<svg/>"), "", true}, // not allowed (can carry script)
		{"fake.pdf", html, "", true},              // content doesn't match extension
		{"fake.png", pdf, "", true},
		{"empty.txt", nil, "", true},
		{"noext", pdf, "", true},
	}
	for _, c := range cases {
		ct, problem := checkUpload(c.name, c.head)
		if c.wantErr != (problem != "") || ct != c.wantCT {
			t.Errorf("%s: got (%q, %q), wantErr=%v wantCT=%q", c.name, ct, problem, c.wantErr, c.wantCT)
		}
	}
}

func TestCleanFileName(t *testing.T) {
	cases := map[string]string{
		"receipt.pdf":                  "receipt.pdf",
		`..\..\windows\system32\x.pdf`: "x.pdf",
		"../../etc/passwd.txt":         "passwd.txt",
		"bad\"name\r\n.pdf":            "badname.pdf",
		"":                             "file",
	}
	for in, want := range cases {
		if got := cleanFileName(in); got != want {
			t.Errorf("cleanFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

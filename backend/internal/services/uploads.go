package services

import (
	"net/http"
	"path/filepath"
	"strings"
)

// allowedUpload describes one accepted file extension: the content type we
// store and serve, and the prefix http.DetectContentType must report for the
// file's first bytes. Anything else (HTML, SVG, scripts, executables) is
// rejected, and a file whose content doesn't match its extension is too.
type allowedUpload struct {
	contentType string
	sniffPrefix string
}

var allowedUploads = map[string]allowedUpload{
	".pdf":  {"application/pdf", "application/pdf"},
	".png":  {"image/png", "image/png"},
	".jpg":  {"image/jpeg", "image/jpeg"},
	".jpeg": {"image/jpeg", "image/jpeg"},
	".gif":  {"image/gif", "image/gif"},
	".webp": {"image/webp", "image/webp"},
	".txt":  {"text/plain; charset=utf-8", "text/plain"},
	".md":   {"text/markdown; charset=utf-8", "text/plain"},
	".csv":  {"text/csv; charset=utf-8", "text/plain"},
	".docx": {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/zip"},
	".xlsx": {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "application/zip"},
	".pptx": {"application/vnd.openxmlformats-officedocument.presentationml.presentation", "application/zip"},
	".doc":  {"application/msword", "application/octet-stream"},
	".xls":  {"application/vnd.ms-excel", "application/octet-stream"},
	".zip":  {"application/zip", "application/zip"},
}

// inlineContentTypes are safe for a browser to display directly; everything
// else is served as a download.
var inlineContentTypes = map[string]bool{
	"application/pdf": true, "image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
}

func IsInlineContentType(ct string) bool { return inlineContentTypes[ct] }

// AllowedUploadExtensions lists accepted extensions, for error messages.
func AllowedUploadExtensions() string {
	return ".pdf, .png, .jpg, .jpeg, .gif, .webp, .txt, .md, .csv, .docx, .xlsx, .pptx, .doc, .xls, .zip"
}

// checkUpload validates name and the file's first bytes and returns the
// content type to store, or a user-facing reason for rejecting it.
func checkUpload(name string, head []byte) (contentType string, problem string) {
	ext := strings.ToLower(filepath.Ext(name))
	rule, ok := allowedUploads[ext]
	if !ok {
		return "", "this file type isn't allowed; use one of " + AllowedUploadExtensions()
	}
	if len(head) == 0 {
		return "", "the file is empty"
	}
	if sniffed := http.DetectContentType(head); !strings.HasPrefix(sniffed, rule.sniffPrefix) {
		return "", "the file's content doesn't match its " + ext + " extension"
	}
	return rule.contentType, ""
}

// cleanFileName keeps a display name safe for headers and the UI: base name
// only, no control characters, at most 255 bytes.
func cleanFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' {
			return -1
		}
		return r
	}, name)
	if len(name) > 255 {
		ext := filepath.Ext(name)
		name = name[:255-len(ext)] + ext
	}
	if name == "" || name == "." || name == "/" {
		name = "file"
	}
	return name
}

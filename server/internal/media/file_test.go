package media

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/media/pdfsample"
)

// A file's type comes from its bytes and its name together: the bytes must be what the name says.
func TestInspectFileJudgesTheBytesAndTheName(t *testing.T) {
	zip := []byte("PK\x03\x04\x14\x00\x00\x00\x08\x00rest of the archive")
	ole := []byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1rest of the document")
	for _, ok := range []struct{ name, want string; data []byte }{
		{"Warranty form.pdf", "application/pdf", pdfsample.PDF("warranty", 0)},
		{"REPORT.PDF", "application/pdf", []byte("%PDF-1.7\n...")},
		{"letter.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", zip},
		{"prices.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", zip},
		{"deck.pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation", zip},
		{"old.doc", "application/msword", ole},
		{"old.xls", "application/vnd.ms-excel", ole},
		{"old.ppt", "application/vnd.ms-powerpoint", ole},
		{"text.odt", "application/vnd.oasis.opendocument.text", zip},
		{"sheet.ods", "application/vnd.oasis.opendocument.spreadsheet", zip},
		{"show.odp", "application/vnd.oasis.opendocument.presentation", zip},
		{"list.csv", "text/csv", []byte("sku,price\nA-1,10\n")},
		{"notes.txt", "text/plain", []byte("Ünïcode notes\n")},
		{"memo.rtf", "application/rtf", []byte(`{\rtf1\ansi hello}`)},
		{"logs.zip", "application/zip", zip},
		{"logs.7z", "application/x-7z-compressed", []byte("7z\xbc\xaf\x27\x1crest")},
		{"logs.tar.gz", "application/gzip", []byte("\x1f\x8b\x08\x00rest")},
		{"logs.tgz", "application/gzip", []byte("\x1f\x8b\x08\x00rest")},
	} {
		got, err := InspectFile(ok.name, ok.data)
		if err != nil || got != ok.want {
			t.Errorf("InspectFile(%q) = %q, %v; want %q", ok.name, got, err, ok.want)
		}
	}
	for _, bad := range []struct{ name string; data []byte }{
		{"setup.pdf", []byte("MZ\x90\x00\x03\x00\x00\x00 an executable")},
		{"setup.exe", []byte("MZ\x90\x00\x03\x00\x00\x00 an executable")},
		{"page.html", []byte("<!doctype html><script>alert(1)</script>")},
		{"logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)},
		{"run.sh", []byte("#!/bin/sh\nrm -rf /\n")},
		{"notes.txt", []byte("text with a \x00 byte")},
		{"letter.docx", []byte("%PDF-1.4 not a zip")},
		{"archive.gz", []byte("\x1f\x8b\x08\x00rest")},
		{"no-extension", []byte("%PDF-1.4")},
	} {
		if got, err := InspectFile(bad.name, bad.data); !errors.Is(err, ErrFileType) {
			t.Errorf("InspectFile(%q) = %q, %v; want ErrFileType", bad.name, got, err)
		}
	}
	for _, listed := range []string{"PDF", ".docx", ".xlsx", ".pptx", ".odt", "CSV", "TXT", "RTF", "ZIP", "7z", "tar.gz"} {
		if !strings.Contains(ErrFileType.Error(), listed) {
			t.Errorf("the refusal %q does not list %s", ErrFileType, listed)
		}
	}
}

// The text of a PDF, a TXT and a CSV is read for search, at most 200 KB of it; other files have none.
func TestFileTextReadsPDFTextAndCSV(t *testing.T) {
	pdf := pdfsample.PDF("Fill in the warranty form", 0)
	if got := FileText("application/pdf", bytes.NewReader(pdf), int64(len(pdf))); !strings.Contains(got, "warranty form") {
		t.Errorf("PDF text = %q, want it to hold %q", got, "warranty form")
	}
	csv := []byte("sku,name\nA-1,Kettle\x00lid\n")
	if got := FileText("text/csv", bytes.NewReader(csv), int64(len(csv))); got != "sku,name\nA-1,Kettlelid\n" {
		t.Errorf("CSV text = %q, want the NUL dropped", got)
	}
	long := bytes.Repeat([]byte("é"), 150*1024) // 300 KB of two-byte letters
	got := FileText("text/plain", bytes.NewReader(long), int64(len(long)))
	if len(got) != 200*1024 || !strings.HasSuffix(got, "é") {
		t.Errorf("long text kept %d bytes ending %q, want exactly 204800 bytes of whole letters", len(got), got[len(got)-2:])
	}
	if got := FileText("application/pdf", strings.NewReader("%PDF-1.4 broken"), 15); got != "" {
		t.Errorf("a damaged PDF gave text %q, want none", got)
	}
	if got := FileText("application/zip", strings.NewReader("PK\x03\x04"), 4); got != "" {
		t.Errorf("a ZIP gave text %q, want none", got)
	}
}

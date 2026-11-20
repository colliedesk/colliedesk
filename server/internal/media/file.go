package media

import (
	"bytes"
	"errors"
	"io"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

// MaxFileText is the most bytes of a file's text kept for search.
const MaxFileText = 200 << 10

// ErrFileType means a file whose name is not of an allowed type, or whose bytes are not what its
// name says (an executable renamed .pdf).
var ErrFileType = errors.New("a file is a PDF; a Word, Excel or PowerPoint file (.doc, .docx, .xls, .xlsx, .ppt, .pptx); " +
	"an OpenDocument file (.odt, .ods, .odp); CSV, TXT or RTF; or a ZIP, 7z or tar.gz archive, and its content must be what its name says")

// fileType is an allowed file: its content type and the bytes it starts with (none for text).
type fileType struct {
	contentType string
	magic       []string
}

var (
	zipMagic = []string{"PK\x03\x04", "PK\x05\x06"}
	oleMagic = []string{"\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"}
	gzMagic  = []string{"\x1f\x8b"}
)

var fileTypes = map[string]fileType{
	".pdf":    {"application/pdf", []string{"%PDF-"}},
	".doc":    {"application/msword", oleMagic},
	".xls":    {"application/vnd.ms-excel", oleMagic},
	".ppt":    {"application/vnd.ms-powerpoint", oleMagic},
	".docx":   {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", zipMagic},
	".xlsx":   {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", zipMagic},
	".pptx":   {"application/vnd.openxmlformats-officedocument.presentationml.presentation", zipMagic},
	".odt":    {"application/vnd.oasis.opendocument.text", zipMagic},
	".ods":    {"application/vnd.oasis.opendocument.spreadsheet", zipMagic},
	".odp":    {"application/vnd.oasis.opendocument.presentation", zipMagic},
	".csv":    {"text/csv", nil},
	".txt":    {"text/plain", nil},
	".rtf":    {"application/rtf", []string{`{\rtf`}},
	".zip":    {"application/zip", zipMagic},
	".7z":     {"application/x-7z-compressed", []string{"7z\xbc\xaf\x27\x1c"}},
	".tar.gz": {"application/gzip", gzMagic},
	".tgz":    {"application/gzip", gzMagic},
}

// fileTypeOf returns the allowed type a file name gives by its extension.
func fileTypeOf(name string) (fileType, bool) {
	lower := strings.ToLower(strings.TrimSpace(name))
	ext := path.Ext(lower)
	if strings.HasSuffix(lower, ".tar.gz") {
		ext = ".tar.gz"
	}
	kind, ok := fileTypes[ext]
	return kind, ok
}

// CheckFileName refuses, before any byte is sent, a file whose name is not of an allowed type.
func CheckFileName(name string) error {
	if _, ok := fileTypeOf(name); !ok {
		return ErrFileType
	}
	return nil
}

// InspectFile returns the content type of a file named name that starts with head (its first bytes,
// 8 KB are enough): the type its name gives, when its bytes are that type. A text file has no NUL byte
// and is UTF-8.
func InspectFile(name string, head []byte) (string, error) {
	kind, ok := fileTypeOf(name)
	if !ok {
		return "", ErrFileType
	}
	if kind.magic == nil {
		if !isText(head) {
			return "", ErrFileType
		}
		return kind.contentType, nil
	}
	for _, magic := range kind.magic {
		if bytes.HasPrefix(head, []byte(magic)) {
			return kind.contentType, nil
		}
	}
	return "", ErrFileType
}

// isText is true for bytes with no NUL that are UTF-8, a letter cut at the end of head aside.
func isText(head []byte) bool {
	if len(head) > 8<<10 {
		head = head[:8<<10]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return false
	}
	for i := 0; i < len(head); {
		r, size := utf8.DecodeRune(head[i:])
		if r == utf8.RuneError && size == 1 && len(head)-i >= utf8.UTFMax {
			return false
		}
		i += size
	}
	return true
}

// FileText returns the text of a PDF, TXT or CSV file for search: at most MaxFileText bytes of whole
// letters, without NUL bytes. Other types, and a PDF the reader cannot read, have none.
func FileText(contentType string, file io.ReaderAt, size int64) string {
	switch contentType {
	case "text/plain", "text/csv":
		data, _ := io.ReadAll(io.NewSectionReader(file, 0, min(size, MaxFileText+utf8.UTFMax)))
		return cleanText(string(data))
	case "application/pdf":
		return cleanText(pdfText(file, size))
	}
	return ""
}

// pdfText reads the text page by page until it has enough; a damaged file gives what was read before
// the damage, since the reader panics on some malformed files.
func pdfText(file io.ReaderAt, size int64) (text string) {
	var out strings.Builder
	defer func() {
		if recover() != nil {
			text = out.String()
		}
	}()
	reader, err := pdf.NewReader(file, size)
	if err != nil {
		return ""
	}
	for page := 1; page <= reader.NumPage() && out.Len() < MaxFileText; page++ {
		p := reader.Page(page)
		if p.V.IsNull() {
			continue
		}
		if words, err := p.GetPlainText(nil); err == nil {
			out.WriteString(words)
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// cleanText drops NUL bytes and invalid UTF-8 and cuts the text to MaxFileText bytes on a letter.
func cleanText(text string) string {
	text = strings.ToValidUTF8(strings.ReplaceAll(text, "\x00", ""), "")
	if len(text) <= MaxFileText {
		return text
	}
	cut := MaxFileText
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

// Package pdfsample builds small valid PDF files for tests: one page that shows a line of text, and
// optionally an unreferenced stream of padding so a test can reach a given size.
package pdfsample

import (
	"bytes"
	"fmt"
	"strings"
)

// PDF returns a PDF whose only page shows text in Helvetica, at least size bytes long when size is
// larger than the page alone.
func PDF(text string, size int) []byte {
	escaped := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(text)
	content := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", escaped)
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	}
	if pad := size - 1024; pad > 0 {
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", pad, bytes.Repeat([]byte{'0'}, pad)))
	}
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return out.Bytes()
}

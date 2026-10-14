package email

import (
	"encoding/base64"
	"io"
	"mime/quotedprintable"
	"regexp"
	"strings"
)

const (
	// maxFileBytes bounds one decoded part; the request body is already capped at 25 MiB.
	maxFileBytes = 25 << 20
	// maxInlineImageBytes bounds the images embedded into the HTML original as data: URIs.
	maxInlineImageBytes = 5 << 20
)

// File is one attachment, inline image or embedded message, decoded from its transfer encoding.
type File struct {
	Filename    string
	ContentType string
	ContentID   string
	Inline      bool
	Data        []byte
}

func decodeFile(encoding string, body io.Reader) []byte {
	reader := io.LimitReader(body, maxFileBytes)
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		reader = base64.NewDecoder(base64.StdEncoding, newlineStripper{reader})
	case "quoted-printable":
		reader = quotedprintable.NewReader(reader)
	}
	data, _ := io.ReadAll(reader)
	return data
}

var cidPattern = regexp.MustCompile(`(?i)cid:([^"'\s)>]+)`)

// inlineImages points cid: references in the HTML original at the matching image as a data: URI, so
// the sandboxed original shows inline images without loading anything from the network. References
// to missing or non-image parts, or beyond the size budget, are removed.
func inlineImages(html string, files []File) string {
	byID := make(map[string]File, len(files))
	for _, file := range files {
		if file.ContentID != "" {
			byID[strings.ToLower(file.ContentID)] = file
		}
	}
	budget := maxInlineImageBytes
	encoded := map[string]string{}
	return cidPattern.ReplaceAllStringFunc(html, func(reference string) string {
		id := strings.ToLower(cidPattern.FindStringSubmatch(reference)[1])
		if uri, ok := encoded[id]; ok {
			return uri
		}
		file, ok := byID[id]
		if !ok || !strings.HasPrefix(file.ContentType, "image/") || len(file.Data) > budget {
			return ""
		}
		budget -= len(file.Data)
		encoded[id] = "data:" + file.ContentType + ";base64," + base64.StdEncoding.EncodeToString(file.Data)
		return encoded[id]
	})
}

func contentID(value string) string {
	return strings.Trim(strings.TrimSpace(value), "<>")
}

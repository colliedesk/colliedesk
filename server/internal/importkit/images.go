package importkit

import (
	"fmt"
	"mime"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/mailimage"
)

// ImageType decides the media type of a downloaded image. Only PNG, JPEG, GIF and WebP are kept:
// the declared type must be one of them (or a generic binary type) and the bytes must agree. The
// result comes from a fixed list, never from the response header, so nothing the source sent can
// reach the stored HTML or the attachment row.
func ImageType(header string, data []byte) (string, error) {
	declared, _, err := mime.ParseMediaType(header)
	if err != nil {
		return "", fmt.Errorf("the Content-Type %q is not valid", Clip(header))
	}
	sniffed := mailimage.SniffRaster(data)
	switch {
	case sniffed == "":
		return "", fmt.Errorf("the response (%q) is not a PNG, JPEG, GIF or WebP image", Clip(declared))
	case declared == "image/jpg":
		declared = "image/jpeg"
	}
	if declared != sniffed && declared != "application/octet-stream" && declared != "binary/octet-stream" {
		return "", fmt.Errorf("the response is declared %q but holds %s data", Clip(declared), sniffed)
	}
	return sniffed, nil
}

// Clip shortens a header value from the source and drops every character that could be markup, so an
// error that names it is safe to put in the Markdown report.
func Clip(value string) string {
	var b strings.Builder
	for _, c := range value {
		if b.Len() >= 60 {
			break
		}
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("/+.-_;= ", c) {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// ImageExtension is the file name extension of a raster media type.
func ImageExtension(contentType string) string {
	switch contentType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	}
	return ""
}

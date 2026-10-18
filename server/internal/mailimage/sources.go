// Package mailimage handles the images in email HTML: finding them, sniffing their type, rewriting
// remote ones to the signed image proxy, and fetching them safely on the server.
package mailimage

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var (
	imgSrc          = regexp.MustCompile(`(?is)(<img\b[^>]*?\ssrc\s*=\s*)(?:"([^"]*)"|'([^']*)')`)
	rasterMediaType = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}
)

// MapSources rewrites the src of every <img>. fn gets the attribute value with HTML entities
// decoded and returns the new value, or false to leave the tag as written. The new value is escaped.
func MapSources(markup string, fn func(src string) (string, bool)) string {
	return imgSrc.ReplaceAllStringFunc(markup, func(tag string) string {
		parts := imgSrc.FindStringSubmatch(tag)
		src := parts[2]
		if src == "" {
			src = parts[3]
		}
		replacement, ok := fn(html.UnescapeString(src))
		if !ok {
			return tag
		}
		return parts[1] + `"` + html.EscapeString(replacement) + `"`
	})
}

// ParseSrc parses an http or https image address; a protocol-relative one is read as https.
func ParseSrc(src string) (*url.URL, bool) {
	src = strings.TrimSpace(src)
	if strings.HasPrefix(src, "//") {
		src = "https:" + src
	}
	u, err := url.Parse(src)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, false
	}
	return u, true
}

// SniffRaster returns the raster media type (PNG, JPEG, GIF or WebP) of the first bytes of data, or
// "". The answer comes from the bytes, never from a header the source sent; SVG is never raster.
func SniffRaster(data []byte) string {
	sniffed := http.DetectContentType(data)
	if i := strings.IndexByte(sniffed, ';'); i >= 0 {
		sniffed = sniffed[:i]
	}
	if rasterMediaType[sniffed] {
		return sniffed
	}
	return ""
}

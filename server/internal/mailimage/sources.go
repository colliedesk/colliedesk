// Package mailimage handles the images in email HTML: finding them, sniffing their type, rewriting
// remote ones to the signed image proxy, and fetching them safely on the server.
package mailimage

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var rasterMediaType = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// mapImgTags calls fn for every <img> tag, read by the HTML tokenizer (so a ">" or " src=" inside another
// attribute value, a comment, a <style> or a <script> is not mistaken for a tag), and writes the tag back
// from its attributes. Every other byte of the markup is copied as it was. When the tokenizer fails, the
// markup is returned unchanged.
func mapImgTags(markup string, fn func(tag *html.Token)) string {
	tokenizer := html.NewTokenizer(strings.NewReader(markup))
	var out strings.Builder
	out.Grow(len(markup))
	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			if !errors.Is(tokenizer.Err(), io.EOF) {
				return markup
			}
			return out.String()
		}
		raw := tokenizer.Raw()
		if kind == html.StartTagToken || kind == html.SelfClosingTagToken {
			if tag := tokenizer.Token(); tag.DataAtom == atom.Img {
				fn(&tag)
				out.WriteString(tag.String())
				continue
			}
		}
		out.Write(raw)
	}
}

// MapSources rewrites the src of every <img>. fn gets the attribute value (HTML entities decoded) and
// returns the new value, or false to leave the tag as written. The new value is escaped.
func MapSources(markup string, fn func(src string) (string, bool)) string {
	return mapImgTags(markup, func(tag *html.Token) {
		for i, attribute := range tag.Attr {
			if attribute.Key == "src" {
				if replacement, ok := fn(attribute.Val); ok {
					tag.Attr[i].Val = replacement
				}
				return
			}
		}
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

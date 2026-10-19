// Package mailimage handles the images in email HTML: finding them, sniffing their type, rewriting
// remote ones to the signed image proxy, and fetching them safely on the server.
package mailimage

import (
	"bytes"
	"encoding/xml"
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

// mapMarkup is mapImgTags for the whole message: tagFn may change any start tag (it reports whether it did,
// and only a changed tag is written back from its attributes), and styleFn rewrites the text of every <style>
// block. Everything else is copied as it was. When the tokenizer fails the markup is returned unchanged.
func mapMarkup(markup string, tagFn func(tag *html.Token) bool, styleFn func(css string) string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(markup))
	var out strings.Builder
	out.Grow(len(markup))
	inStyle := false
	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			if !errors.Is(tokenizer.Err(), io.EOF) {
				return markup
			}
			return out.String()
		}
		raw := tokenizer.Raw()
		switch {
		case kind == html.TextToken && inStyle:
			out.WriteString(styleFn(string(raw)))
			continue
		case kind == html.EndTagToken:
			inStyle = false
		case kind == html.StartTagToken || kind == html.SelfClosingTagToken:
			tag := tokenizer.Token()
			inStyle = kind == html.StartTagToken && tag.DataAtom == atom.Style
			if tagFn(&tag) {
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

// SVGType is the media type the proxy serves for a vector image.
const SVGType = "image/svg+xml"

const svgNamespace = "http://www.w3.org/2000/svg"

// Real logos and icon sheets are a few levels deep and a few hundred elements. The walk stops at the first
// document past these, so a hostile body of a million nested elements costs a few hundred of them, not the
// memory and time of parsing all.
const (
	maxSVGDepth    = 64
	maxSVGElements = 10_000
)

// SniffImage returns the media type of an image the proxy may serve: a raster (see SniffRaster) or an SVG
// document, or "". An SVG is accepted only when the whole body parses as XML whose root element is svg in the
// SVG namespace; a script inside it is harmless in an <img>, which the proxy's headers and the email frame
// both rely on (nothing may embed it as a document).
func SniffImage(data []byte) string {
	if raster := SniffRaster(data); raster != "" {
		return raster
	}
	if isSVG(data) {
		return SVGType
	}
	return ""
}

func isSVG(data []byte) bool {
	decoder := xml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	decoder.Strict = true
	root, depth, elements := false, 0, 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return root && depth == 0
		}
		if err != nil {
			return false
		}
		switch element := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				if root || element.Name.Local != "svg" || element.Name.Space != svgNamespace {
					return false
				}
				root = true
			}
			if depth++; depth > maxSVGDepth {
				return false
			}
			if elements++; elements > maxSVGElements {
				return false
			}
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && len(bytes.TrimSpace(element)) > 0 {
				return false
			}
		}
	}
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

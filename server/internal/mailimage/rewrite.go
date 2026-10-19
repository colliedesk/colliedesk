package mailimage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ProxyPath is the API route that serves a remote image of a message on the viewer's behalf.
const ProxyPath = "/api/v1/mail-image"

// MaxImagesPerMessage bounds how many distinct remote images one message can send through the proxy.
// Further images show nothing: a sender cannot make a single view fetch and store thousands of files.
const MaxImagesPerMessage = 50

// Signer signs proxy addresses. The signature covers the workspace and the exact original URL, so
// the proxy route is not an open relay and one workspace's link is worthless in another.
type Signer struct{ key []byte }

func NewSigner(key []byte) *Signer { return &Signer{key: key} }

// Sign returns the hex HMAC-SHA256 of the workspace id and the original URL.
func (s *Signer) Sign(workspaceID, rawURL string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(workspaceID))
	mac.Write([]byte{0})
	mac.Write([]byte(rawURL))
	return hex.EncodeToString(mac.Sum(nil))
}

// Valid reports whether sig is the signature of this workspace and URL, in constant time.
func (s *Signer) Valid(workspaceID, rawURL, sig string) bool {
	given, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	expected, _ := hex.DecodeString(s.Sign(workspaceID, rawURL))
	return hmac.Equal(given, expected)
}

// Path is the proxy address of one remote image.
func (s *Signer) Path(workspaceID, rawURL string) string {
	return ProxyPath + "?u=" + url.QueryEscape(rawURL) + "&s=" + s.Sign(workspaceID, rawURL)
}

// Rewrite points every remote image of the message at the proxy, up to MaxImagesPerMessage distinct
// addresses in all (a further one gets an empty address and loads nothing): the http, https or
// protocol-relative <img src>, and every url() in a style attribute or <style> block (background,
// list-style-image, border-image, content, image-set). data:, cid:, relative and every other source stay as
// written. srcset is removed from every <img>: the viewer shows the src, and a candidate list would load
// around the proxy. @import is dropped from CSS.
func (s *Signer) Rewrite(workspaceID, markup string) string {
	seen := map[string]bool{}
	type resolved struct {
		path   string
		remote bool
	}
	// memo: the same address written many times in one message is parsed and signed once.
	memo := map[string]resolved{}
	resolve := func(raw string) (string, bool) {
		if r, ok := memo[raw]; ok {
			return r.path, r.remote
		}
		r := resolved{}
		if u, ok := ParseSrc(raw); ok {
			address := u.String()
			r.remote = true
			if seen[address] || len(seen) < MaxImagesPerMessage {
				seen[address] = true
				r.path = s.Path(workspaceID, address)
			}
		}
		memo[raw] = r
		return r.path, r.remote
	}
	return mapMarkup(markup, func(tag *html.Token) bool {
		changed := false
		if tag.DataAtom == atom.Img {
			changed = true
			kept := tag.Attr[:0]
			for _, attribute := range tag.Attr {
				if attribute.Key != "srcset" {
					kept = append(kept, attribute)
				}
			}
			tag.Attr = kept
			for i, attribute := range tag.Attr {
				if attribute.Key != "src" {
					continue
				}
				if path, remote := resolve(attribute.Val); remote {
					tag.Attr[i].Val = path
				}
				break
			}
		}
		for i, attribute := range tag.Attr {
			if attribute.Key == "style" {
				if rewritten := rewriteCSS(attribute.Val, resolve); rewritten != attribute.Val {
					tag.Attr[i].Val, changed = rewritten, true
				}
			}
		}
		return changed
	}, func(css string) string { return rewriteCSS(css, resolve) })
}

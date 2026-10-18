package mailimage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"

	"golang.org/x/net/html"
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

// Rewrite points every remote (http, https or protocol-relative) <img src> at the proxy, up to
// MaxImagesPerMessage distinct addresses; a further one gets an empty src. data:, cid:, relative and
// every other source stay as written. srcset is removed from every <img>: the viewer shows the src, and a
// candidate list would load around the proxy.
func (s *Signer) Rewrite(workspaceID, markup string) string {
	seen := map[string]bool{}
	return mapImgTags(markup, func(tag *html.Token) {
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
			u, ok := ParseSrc(attribute.Val)
			if !ok {
				return
			}
			address := u.String()
			if !seen[address] && len(seen) >= MaxImagesPerMessage {
				tag.Attr[i].Val = ""
				return
			}
			seen[address] = true
			tag.Attr[i].Val = s.Path(workspaceID, address)
			return
		}
	})
}

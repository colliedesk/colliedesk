package mailimage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"regexp"
)

// ProxyPath is the API route that serves a remote image of a message on the viewer's behalf.
const ProxyPath = "/api/v1/mail-image"

var (
	imgTag     = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	srcsetAttr = regexp.MustCompile(`(?is)\ssrcset\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]+)`)
)

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

// Rewrite points every remote (http, https or protocol-relative) <img src> at the proxy. data:, cid:,
// relative and every other source stay as written. srcset is removed from every <img>: the viewer
// shows the src, and a candidate list would load around the proxy.
func (s *Signer) Rewrite(workspaceID, markup string) string {
	markup = imgTag.ReplaceAllStringFunc(markup, func(tag string) string { return srcsetAttr.ReplaceAllString(tag, "") })
	return MapSources(markup, func(src string) (string, bool) {
		u, ok := ParseSrc(src)
		if !ok {
			return "", false
		}
		return s.Path(workspaceID, u.String()), true
	})
}

package attachments

import (
	"fmt"
	"strings"
)

// EncodeFilename percent-encodes every byte of a file name that is not an RFC 5987 attr-char, for the
// filename* parameter of a Content-Disposition in UTF-8.
func EncodeFilename(value string) string {
	var b strings.Builder
	for _, c := range []byte(value) {
		if isAttrChar(c) {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func isAttrChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("!#$&+-.^_`|~", c) >= 0
}

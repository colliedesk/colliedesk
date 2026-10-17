package email

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// paramSegmentChars bounds one RFC 2231 segment, so every header line stays short.
	paramSegmentChars = 60
	// encodedWordBytes bounds the text of one RFC 2047 word: its encoded word stays under 75 characters.
	encodedWordBytes = 30
	foldedSeparator  = ";\r\n "
)

// parameter formats one header parameter value. Short printable ASCII is a quoted string. Anything
// else is an RFC 2231 extended value, split into numbered segments (name*0*, name*1*, ...) that each
// sit on their own folded line, so a long non-ASCII filename never makes a line past the RFC 5322 limit.
func parameter(name, value string) string {
	if len(value) <= paramSegmentChars && printableASCII(value) {
		return name + `="` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
	}
	segments := percentSegments(value)
	if len(segments) == 1 {
		return name + "*=utf-8''" + segments[0]
	}
	parts := make([]string, len(segments))
	for i, segment := range segments {
		if i == 0 {
			segment = "utf-8''" + segment
		}
		parts[i] = fmt.Sprintf("%s*%d*=%s", name, i, segment)
	}
	return strings.Join(parts, foldedSeparator)
}

// encodedWordParameter writes name as a quoted string of RFC 2047 encoded words, the form Outlook
// reads. It returns "" when the value is plain ASCII and needs none.
func encodedWordParameter(name, value string) string {
	if printableASCII(value) {
		return ""
	}
	var words []string
	for len(value) > 0 {
		cut := min(encodedWordBytes, len(value))
		for cut < len(value) && !utf8.RuneStart(value[cut]) {
			cut--
		}
		words = append(words, "=?UTF-8?B?"+base64.StdEncoding.EncodeToString([]byte(value[:cut]))+"?=")
		value = value[cut:]
	}
	return name + `="` + strings.Join(words, "\r\n ") + `"`
}

// percentSegments percent-encodes value (UTF-8) and splits it at rune boundaries.
func percentSegments(value string) []string {
	var segments []string
	var current strings.Builder
	for _, r := range value {
		var piece strings.Builder
		for _, b := range []byte(string(r)) {
			if attrChar(b) {
				piece.WriteByte(b)
			} else {
				fmt.Fprintf(&piece, "%%%02X", b)
			}
		}
		if current.Len() > 0 && current.Len()+piece.Len() > paramSegmentChars {
			segments = append(segments, current.String())
			current.Reset()
		}
		current.WriteString(piece.String())
	}
	return append(segments, current.String())
}

func attrChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	}
	return strings.IndexByte("!#$&+-.^_`|~", b) >= 0
}

func printableASCII(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

package helpcenter

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gorilla/css/scanner"
)

// MaxCustomCSSBytes bounds a help center's custom CSS; the database checks it too.
const MaxCustomCSSBytes = 32 * 1024

// CustomCSSError is custom CSS refused at a line and column (from 1, in characters; \r\n, \r and \f
// end a line as \n does).
type CustomCSSError struct {
	Line, Column int
	Reason       string
}

func (e *CustomCSSError) Error() string {
	return fmt.Sprintf("line %d, column %d: %s", e.Line, e.Column, e.Reason)
}

// The at-rules custom CSS may use: they only group rules. @import, @font-face, @namespace, @charset,
// @property, @page and anything new are refused, so nothing an at-rule loads or redefines gets in.
var customCSSAtRules = map[string]bool{"media": true, "supports": true, "keyframes": true, "layer": true, "container": true}

// The functions that take an address or an image source, and IE's expression(), which ran script.
// A vendor prefix (-webkit-image-set) is read without it.
var customCSSFunctions = map[string]bool{"url": true, "image": true, "image-set": true, "cross-fade": true, "element": true,
	"src": true, "expression": true, "paint": true, "image-rect": true}

// The properties of old browsers that bound script to an element, refused where a property is named
// (followed by a colon), so a class such as .behavior stays allowed.
var customCSSProperties = map[string]bool{"behavior": true, "-moz-binding": true}

// customCSSWords are refused anywhere, quotes and comments included, once escapes are read: harmless
// there, but refusing them keeps the rule simple to state and to prove. The functions of
// customCSSFunctions are here too, so their refusal does not depend on how the tokenizer splits a name:
// it reads U+0-image( as a unicode range and a function named -image, which no vendor prefix explains.
var customCSSWords = []string{"url(", "@import", "image-set(", "image(", "cross-fade(", "element(", "src(", "paint(", "expression(",
	"javascript:", "vbscript:", "</"}

const customCSSAtRuleList = "use @media, @supports, @keyframes, @layer or @container"

// CheckCustomCSS refuses, with a CustomCSSError at the first problem, custom CSS that could load
// anything from anywhere (url(), image functions, @import, @font-face), run script in an old browser
// (expression(), behavior, -moz-binding) or reach past its own rules: an unbalanced brace, an
// unclosed string or comment, </ (which would end the <style> element of the preview) or <!--. A
// control character other than tab, newline, carriage return and form feed is refused too. A
// backslash is allowed only inside quotes, so an escape cannot spell a refused name (u\72l(). What is
// accepted is served byte for byte; nothing is rewritten. The help center's Content-Security-Policy
// (img-src 'self', default-src 'none') still applies to whatever a browser reads differently.
func CheckCustomCSS(text string) error {
	if len(text) > MaxCustomCSSBytes {
		return refuseCustomCSS(text, MaxCustomCSSBytes, fmt.Sprintf("custom CSS holds at most 32,768 bytes; this has %d", len(text)))
	}
	for at := 0; at < len(text); {
		r, width := utf8.DecodeRuneInString(text[at:])
		switch {
		case r == utf8.RuneError && width == 1:
			return refuseCustomCSS(text, at, "custom CSS must be UTF-8 text")
		case r == 0:
			return refuseCustomCSS(text, at, "a NUL character is not allowed")
		case r == '\ufeff':
			return refuseCustomCSS(text, at, "a byte order mark is not allowed")
		case r < 0x20 && !strings.ContainsRune("\t\n\r\f", r) || r == 0x7f:
			return refuseCustomCSS(text, at, fmt.Sprintf("a control character (U+%04X) is not allowed", r))
		}
		at += width
	}
	if at := strings.Index(text, "</"); at >= 0 {
		return refuseCustomCSS(text, at, "</ is not allowed: it would end the style element")
	}
	if err := checkCustomCSSTokens(text); err != nil {
		return err
	}
	return checkCustomCSSWords(text)
}

type openBracket struct {
	char      string
	line, col int
}

var closing = map[string]string{"}": "{", ")": "(", "]": "["}

// checkCustomCSSTokens reads text with the tokenizer. A tab is read as a space: CSS allows one inside
// quotes and the tokenizer does not, and outside quotes both are white space. The text keeps its length,
// so every place stays where it is.
func checkCustomCSSTokens(text string) error {
	var open []openBracket
	// previous is the last token other than white space and comments; property is a name of
	// customCSSProperties waiting to see whether a colon follows it.
	var previous, property *scanner.Token
	tokens := scanner.New(strings.ReplaceAll(text, "\t", " "))
	for {
		token := tokens.Next()
		refuse := func(reason string) error {
			return &CustomCSSError{Line: token.Line, Column: token.Column, Reason: reason}
		}
		if token.Type != scanner.TokenS && token.Type != scanner.TokenComment {
			if property != nil && token.Type == scanner.TokenChar && token.Value == ":" {
				return &CustomCSSError{Line: property.Line, Column: property.Column, Reason: strings.ToLower(property.Value) + " is not allowed"}
			}
			property = nil
			if token.Type == scanner.TokenIdent && customCSSProperties[strings.ToLower(token.Value)] && !followsClassOrPseudo(previous) {
				property = token
			}
			previous = token
		}
		switch token.Type {
		case scanner.TokenEOF:
			if len(open) > 0 {
				last := open[len(open)-1]
				return &CustomCSSError{Line: last.line, Column: last.col, Reason: last.char + " is never closed"}
			}
			return nil
		case scanner.TokenError:
			if strings.Contains(token.Value, "comment") {
				return refuse("the comment is never closed")
			}
			return refuse("the quotation mark is never closed")
		case scanner.TokenCDO, scanner.TokenCDC:
			return refuse("<!-- and --> are not allowed")
		}
		if at := strings.IndexByte(token.Value, '\\'); at >= 0 && token.Type != scanner.TokenString {
			line, col := position(token.Value, at)
			if line == 1 {
				col += token.Column - 1
			}
			return &CustomCSSError{Line: token.Line + line - 1, Column: col, Reason: "a backslash is allowed only inside quotes"}
		}
		switch token.Type {
		case scanner.TokenURI:
			return refuse("url() is not allowed")
		case scanner.TokenFunction:
			name := strings.ToLower(strings.TrimSuffix(token.Value, "("))
			if customCSSFunctions[withoutVendor(name)] {
				return refuse(name + "() is not allowed")
			}
			open = append(open, openBracket{"(", token.Line, token.Column})
		case scanner.TokenAtKeyword:
			name := strings.ToLower(strings.TrimPrefix(token.Value, "@"))
			if !customCSSAtRules[withoutVendor(name)] {
				return refuse("@" + name + " is not allowed; " + customCSSAtRuleList)
			}
		case scanner.TokenChar:
			switch token.Value {
			case "{", "(", "[":
				open = append(open, openBracket{token.Value, token.Line, token.Column})
			case "}", ")", "]":
				if len(open) == 0 || open[len(open)-1].char != closing[token.Value] {
					return refuse(token.Value + " closes nothing open here")
				}
				open = open[:len(open)-1]
			}
		}
	}
}

// followsClassOrPseudo is true after . or :, where a name is a class or a pseudo-class, not a property.
func followsClassOrPseudo(previous *scanner.Token) bool {
	return previous != nil && previous.Type == scanner.TokenChar && (previous.Value == "." || previous.Value == ":")
}

// withoutVendor drops a vendor prefix such as -webkit- or -moz- from a lowercase name.
func withoutVendor(name string) string {
	if !strings.HasPrefix(name, "-") || strings.HasPrefix(name, "--") {
		return name
	}
	if end := strings.IndexByte(name[1:], '-'); end > 0 {
		return name[end+2:]
	}
	return name
}

// checkCustomCSSWords reads the text with its escapes decoded and lowercased, and refuses any of
// customCSSWords, at the place in the text where it starts.
func checkCustomCSSWords(text string) error {
	read := readCSS(text)
	for index := range read {
		for _, word := range customCSSWords {
			if len(read)-index < len(word) {
				continue
			}
			match := true
			for offset, want := range word {
				if read[index+offset].r != want {
					match = false
					break
				}
			}
			if match {
				return refuseCustomCSS(text, read[index].at, word+" is not allowed, not even in quotes or comments")
			}
		}
	}
	return nil
}

// cssRune is a character as CSS reads it, lowercased, and the byte where it starts in the text.
type cssRune struct {
	r  rune
	at int
}

// readCSS decodes CSS escapes (CSS Syntax 3, "consume an escaped code point"): \ and one to six hex
// digits and one optional white space is that code point; \ and a newline is nothing; \ and any other
// character is that character.
func readCSS(text string) []cssRune {
	read := make([]cssRune, 0, len(text))
	for at := 0; at < len(text); {
		r, width := utf8.DecodeRuneInString(text[at:])
		if r != '\\' {
			read = append(read, cssRune{unicode.ToLower(r), at})
			at += width
			continue
		}
		start := at
		at++
		if at == len(text) {
			read = append(read, cssRune{'\ufffd', start})
			break
		}
		digits := at
		for digits < len(text) && digits-at < 6 && isHexDigit(text[digits]) {
			digits++
		}
		if digits > at {
			// Six hex digits fit in 32 bits; the value is checked against Unicode before it becomes a rune.
			code, err := strconv.ParseUint(text[at:digits], 16, 32)
			r = '\ufffd'
			if err == nil && code > 0 && code <= unicode.MaxRune && (code < 0xd800 || code > 0xdfff) {
				r = rune(code)
			}
			read = append(read, cssRune{unicode.ToLower(r), start})
			at = digits
			if strings.HasPrefix(text[at:], "\r\n") {
				at += 2
			} else if at < len(text) && strings.IndexByte(" \t\n\r\f", text[at]) >= 0 {
				at++
			}
			continue
		}
		if strings.HasPrefix(text[at:], "\r\n") {
			at += 2
			continue
		}
		r, width = utf8.DecodeRuneInString(text[at:])
		if r == '\n' || r == '\r' || r == '\f' {
			at += width
			continue
		}
		read = append(read, cssRune{unicode.ToLower(r), start})
		at += width
	}
	return read
}

func isHexDigit(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}

func refuseCustomCSS(text string, at int, reason string) error {
	line, col := position(text, at)
	return &CustomCSSError{Line: line, Column: col, Reason: reason}
}

// position is the line and column of the byte at offset, both from 1, counting characters; \r\n, \r,
// \n and \f each end a line, as the tokenizer counts them.
func position(text string, offset int) (line, col int) {
	line, col = 1, 1
	for at := 0; at < offset && at < len(text); {
		r, width := utf8.DecodeRuneInString(text[at:])
		switch {
		case strings.HasPrefix(text[at:], "\r\n"):
			line, col, width = line+1, 1, 2
		case r == '\n' || r == '\r' || r == '\f':
			line, col = line+1, 1
		default:
			col++
		}
		at += width
	}
	return line, col
}

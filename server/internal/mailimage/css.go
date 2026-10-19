package mailimage

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// rewriteCSS walks CSS text the way a CSS tokenizer does (comments, strings with escapes, url() tokens,
// at-rules) and hands every remote image address to image, which returns the text to put in its place.
// It rewrites url(...) tokens anywhere in a declaration (background, list-style-image, border-image,
// content, ...) and the strings inside image-set(...). Every @import is dropped: it would load more CSS
// from the sender. Text inside comments and strings that is not an image address is left alone.
//
// It is deliberately not a regular expression: quotes, escapes ("\\41 ", "\\ ") and comments decide where
// a url() token starts and ends.
func rewriteCSS(css string, rewrite func(raw string) (replacement string, remote bool)) string {
	var out strings.Builder
	out.Grow(len(css))
	paren, setDepth := 0, 0 // setDepth: the paren depth of an open image-set(, or 0
	for i := 0; i < len(css); {
		c := css[i]
		switch {
		case c == '/' && strings.HasPrefix(css[i:], "/*"):
			end := strings.Index(css[i+2:], "*/")
			if end < 0 {
				out.WriteString(css[i:])
				return out.String()
			}
			out.WriteString(css[i : i+2+end+2])
			i += 2 + end + 2
		case c == '"' || c == '\'':
			end := stringEnd(css, i)
			if setDepth > 0 && paren >= setDepth {
				if replacement, ok := rewrite(unescapeCSS(stringInner(css, i, end))); ok {
					out.WriteByte('"')
					out.WriteString(replacement)
					out.WriteByte('"')
					i = end
					continue
				}
			}
			out.WriteString(css[i:end])
			i = end
		case c == '@':
			if name, end := readIdent(css, i+1); strings.EqualFold(name, "import") {
				// Replaced by a space, so the text around it cannot join into something new (a "</style>").
				out.WriteByte(' ')
				i = skipStatement(css, end)
				continue
			}
			out.WriteByte(c)
			i++
		case startsIdent(css, i) && !isIdent(byteBefore(css, i)):
			name, end := readIdent(css, i)
			if end < len(css) && css[end] == '(' {
				switch strings.ToLower(name) {
				case "url":
					if raw, stop, ok := parseURLToken(css, end+1); ok {
						if replacement, remote := rewrite(raw); remote {
							out.WriteString(`url("`)
							out.WriteString(replacement)
							out.WriteString(`")`)
						} else {
							out.WriteString(css[i:stop])
						}
						i = stop
						continue
					}
				case "image-set", "-webkit-image-set":
					out.WriteString(css[i : end+1])
					paren++
					setDepth = paren
					i = end + 1
					continue
				}
			}
			out.WriteString(css[i:end])
			i = end
		case c == '\\' && i+1 < len(css):
			out.WriteString(css[i : i+2]) // an escaped character is never a quote or a comment
			i += 2
		case c == '(':
			paren++
			out.WriteByte(c)
			i++
		case c == ')':
			if paren > 0 {
				if paren == setDepth {
					setDepth = 0
				}
				paren--
			}
			out.WriteByte(c)
			i++
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

// stringInner is the text of the string that spans css[start:end], without its quotes.
func stringInner(css string, start, end int) string {
	inner := css[start+1 : end]
	if len(inner) > 0 && inner[len(inner)-1] == css[start] {
		inner = inner[:len(inner)-1]
	}
	return inner
}

// startsIdent reports whether an identifier (a function or at-rule name) can begin at i: a letter, "_", "-",
// a non-ASCII byte, or an escape.
func startsIdent(css string, i int) bool {
	c := css[i]
	return c == '_' || c == '-' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80 ||
		(c == '\\' && i+1 < len(css) && css[i+1] != '\n')
}

// readIdent reads the identifier at i, decoding escapes ("\75 rl" is "url"), and returns it with the index
// after it.
func readIdent(css string, i int) (string, int) {
	var name strings.Builder
	for i < len(css) {
		switch c := css[i]; {
		case isIdent(c):
			name.WriteByte(c)
			i++
		case c == '\\' && i+1 < len(css) && css[i+1] != '\n':
			decoded, next := decodeEscape(css, i+1)
			name.WriteString(decoded)
			i = next
		default:
			return name.String(), i
		}
	}
	return name.String(), i
}

// stringEnd returns the index just after the string that opens at start (or the end of the text).
func stringEnd(css string, start int) int {
	quote := css[start]
	for i := start + 1; i < len(css); i++ {
		switch css[i] {
		case '\\':
			i++
		case quote:
			return i + 1
		case '\n':
			return i // an unterminated string ends at the line break
		}
	}
	return len(css)
}

// parseURLToken reads what follows "url(": an optional quoted string or an unquoted token, then ")".
// ok is false for a bad url token, which a browser does not load either.
func parseURLToken(css string, i int) (raw string, end int, ok bool) {
	i = skipSpace(css, i)
	if i >= len(css) {
		return "", 0, false
	}
	if css[i] == '"' || css[i] == '\'' {
		stop := stringEnd(css, i)
		if stop-1 <= i || css[stop-1] != css[i] {
			return "", 0, false
		}
		raw = unescapeCSS(css[i+1 : stop-1])
		i = skipSpace(css, stop)
	} else {
		start := i
		for i < len(css) && css[i] != ')' && !isSpace(css[i]) {
			switch css[i] {
			case '"', '\'', '(':
				return "", 0, false
			case '\\':
				i++
			}
			i++
		}
		if i > len(css) {
			return "", 0, false
		}
		raw = unescapeCSS(css[start:min(i, len(css))])
		i = skipSpace(css, min(i, len(css)))
	}
	if i >= len(css) || css[i] != ')' {
		return "", 0, false
	}
	return raw, i + 1, true
}

// skipStatement returns the index after the at-rule statement that starts at i: up to the next ";" outside
// strings, comments and parentheses. A block ({...}) is not a statement; the walk stops before it.
func skipStatement(css string, i int) int {
	depth := 0
	for i < len(css) {
		switch c := css[i]; {
		case c == '/' && strings.HasPrefix(css[i:], "/*"):
			end := strings.Index(css[i+2:], "*/")
			if end < 0 {
				return len(css)
			}
			i += 2 + end + 2
		case c == '"' || c == '\'':
			i = stringEnd(css, i)
		case c == '\\':
			i += 2
		case c == '(':
			depth++
			i++
		case c == ')':
			depth--
			i++
		case c == ';' && depth <= 0:
			return i + 1
		case c == '{' || c == '}':
			return i
		default:
			i++
		}
	}
	return len(css)
}

// unescapeCSS resolves CSS escapes: "\\41 " (1 to 6 hex digits and one optional space), "\\ " and "\\x".
func unescapeCSS(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			i++
			continue
		}
		if s[i+1] == '\n' {
			i += 2 // a line continuation
			continue
		}
		decoded, next := decodeEscape(s, i+1)
		b.WriteString(decoded)
		i = next
	}
	return b.String()
}

// decodeEscape decodes the escape whose backslash is just before s[i]: up to six hex digits and one
// optional space, or any other single character. It returns the text and the index after the escape.
func decodeEscape(s string, i int) (string, int) {
	digits := 0
	for digits < 6 && i+digits < len(s) && isHex(s[i+digits]) {
		digits++
	}
	if digits == 0 {
		r, size := utf8.DecodeRuneInString(s[i:])
		return string(r), i + size
	}
	code, err := strconv.ParseInt(s[i:i+digits], 16, 32)
	i += digits
	if i < len(s) && isSpace(s[i]) {
		i++
	}
	if err != nil || code <= 0 || code > utf8.MaxRune {
		return string(utf8.RuneError), i
	}
	return string(rune(code)), i
}

func byteBefore(s string, i int) byte {
	if i > 0 {
		return s[i-1]
	}
	return 0
}

func isIdent(c byte) bool {
	return c == '_' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

func isHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

func skipSpace(s string, i int) int {
	for i < len(s) && isSpace(s[i]) {
		i++
	}
	return i
}

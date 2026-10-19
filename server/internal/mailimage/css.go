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
func rewriteCSS(css string, image func(address string) string) string {
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
				if replacement, ok := remoteInString(css, i, end, image); ok {
					out.WriteString(`"` + replacement + `"`)
					i = end
					continue
				}
			}
			out.WriteString(css[i:end])
			i = end
		case c == '@' && hasPrefixFold(css[i:], "@import") && !isIdent(byteAt(css, i+7)):
			i = skipStatement(css, i)
		case (c == 'u' || c == 'U') && hasPrefixFold(css[i:], "url(") && !isIdent(byteBefore(css, i)):
			if raw, end, ok := parseURLToken(css, i+4); ok {
				if replacement, remote := remoteAddress(raw, image); remote {
					out.WriteString(`url("` + replacement + `")`)
				} else {
					out.WriteString(css[i:end])
				}
				i = end
				continue
			}
			out.WriteByte(c)
			i++
		case isImageSet(css, i):
			name := strings.IndexByte(css[i:], '(')
			out.WriteString(css[i : i+name+1])
			paren++
			setDepth = paren
			i += name + 1
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

func remoteAddress(raw string, image func(string) string) (string, bool) {
	u, ok := ParseSrc(raw)
	if !ok {
		return "", false
	}
	return image(u.String()), true
}

func remoteInString(css string, start, end int, image func(string) string) (string, bool) {
	inner := css[start+1 : end]
	if strings.HasSuffix(inner, css[start:start+1]) && len(inner) > 0 {
		inner = inner[:len(inner)-1]
	}
	return remoteAddress(unescapeCSS(inner), image)
}

func isImageSet(css string, i int) bool {
	for _, name := range []string{"image-set(", "-webkit-image-set("} {
		if hasPrefixFold(css[i:], name) && !isIdent(byteBefore(css, i)) {
			return true
		}
	}
	return false
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
		i++
		if s[i] == '\n' {
			i++
			continue
		}
		digits := 0
		for digits < 6 && i+digits < len(s) && isHex(s[i+digits]) {
			digits++
		}
		if digits == 0 {
			r, size := utf8.DecodeRuneInString(s[i:])
			b.WriteRune(r)
			i += size
			continue
		}
		code, _ := strconv.ParseUint(s[i:i+digits], 16, 32)
		i += digits
		if i < len(s) && isSpace(s[i]) {
			i++
		}
		if code == 0 || code > utf8.MaxRune {
			code = utf8.RuneError
		}
		b.WriteRune(rune(code))
	}
	return b.String()
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

func byteAt(s string, i int) byte {
	if i < len(s) {
		return s[i]
	}
	return 0
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

package email

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

const (
	maxPartBytes = 5 << 20
	maxDepth     = 10
)

const truncationNotice = "\n\n[Message text truncated at 5 MiB]"

type content struct {
	plain, html                   string
	plainTruncated, htmlTruncated bool
	attachments                   []domain.EmailAttachment
	files                         []File
	bouncedIDs                    []string
}

func (c *content) addFile(file File) {
	c.attachments = append(c.attachments, domain.EmailAttachment{Filename: file.Filename, ContentType: file.ContentType})
	c.files = append(c.files, file)
}

// text prefers the first text/plain part and falls back to the first text/html part reduced to text.
func (c content) text() string {
	text, truncated := normalizeText(c.plain), c.plainTruncated
	if strings.TrimSpace(c.plain) == "" {
		text, truncated = normalizeText(htmlToText(c.html)), c.htmlTruncated
	}
	if truncated {
		text += truncationNotice
	}
	return text
}

func textprotoHeader(header mail.Header) textproto.MIMEHeader {
	return textproto.MIMEHeader(header)
}

func extractContent(header textproto.MIMEHeader, body io.Reader) content {
	var result content
	walk(header, body, 0, &result)
	return result
}

// walk visits a MIME entity. Malformed parts are skipped rather than failing the whole message.
func walk(header textproto.MIMEHeader, body io.Reader, depth int, result *content) {
	mediaType, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil {
		mediaType, params = "text/plain", map[string]string{}
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		if depth >= maxDepth || params["boundary"] == "" {
			return
		}
		reader := multipart.NewReader(body, params["boundary"])
		for {
			part, err := reader.NextRawPart()
			if err != nil {
				return
			}
			walk(part.Header, part, depth+1, result)
		}
	}
	if mediaType == "message/rfc822" || mediaType == "text/rfc822-headers" || mediaType == "message/rfc822-headers" {
		data := decodeFile(header.Get("Content-Transfer-Encoding"), body)
		// A delivery status report returns the headers of the message that bounced.
		if returned, err := textproto.NewReader(bufio.NewReader(bytes.NewReader(data))).ReadMIMEHeader(); err == nil || len(returned) > 0 {
			if id := firstID(returned.Get("Message-Id")); id != "" {
				result.bouncedIDs = append(result.bouncedIDs, boundID(id))
			}
		}
		name := "message.eml"
		if mediaType != "message/rfc822" {
			name = "returned-headers.txt"
		}
		result.addFile(File{Filename: name, ContentType: mediaType, Data: data})
		return
	}
	disposition, dispositionParams, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
	filename := decodeHeader(firstNonEmpty(dispositionParams["filename"], params["name"]))
	isText := mediaType == "text/plain" || mediaType == "text/html"
	if disposition == "attachment" || filename != "" || !isText {
		if filename == "" {
			filename = "(unnamed)"
		}
		result.addFile(File{Filename: cleanLine(filename), ContentType: mediaType, ContentID: contentID(header.Get("Content-Id")),
			Inline: disposition != "attachment" && header.Get("Content-Id") != "", Data: decodeFile(header.Get("Content-Transfer-Encoding"), body)})
		return
	}
	text, truncated := decodePart(header.Get("Content-Transfer-Encoding"), params["charset"], body)
	if mediaType == "text/plain" && result.plain == "" {
		result.plain, result.plainTruncated = text, truncated
	}
	if mediaType == "text/html" && result.html == "" {
		result.html, result.htmlTruncated = text, truncated
	}
}

// limitedReader stops after limit bytes and records whether more input remained.
type limitedReader struct {
	reader    io.Reader
	remaining int64
	truncated bool
}

func (l *limitedReader) Read(buffer []byte) (int, error) {
	if l.remaining <= 0 {
		var probe [1]byte
		if count, _ := l.reader.Read(probe[:]); count > 0 {
			l.truncated = true
		}
		return 0, io.EOF
	}
	if int64(len(buffer)) > l.remaining {
		buffer = buffer[:l.remaining]
	}
	count, err := l.reader.Read(buffer)
	l.remaining -= int64(count)
	return count, err
}

func decodePart(encoding, charset string, body io.Reader) (string, bool) {
	limited := &limitedReader{reader: body, remaining: maxPartBytes}
	var reader io.Reader = limited
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		reader = base64.NewDecoder(base64.StdEncoding, newlineStripper{reader})
	case "quoted-printable":
		reader = quotedprintable.NewReader(reader)
	}
	if charset != "" && !strings.EqualFold(charset, "utf-8") && !strings.EqualFold(charset, "us-ascii") {
		if decoded, err := charsetReader(charset, reader); err == nil {
			reader = decoded
		}
	}
	data, _ := io.ReadAll(reader)
	return strings.ToValidUTF8(string(data), string(utf8.RuneError)), limited.truncated
}

// newlineStripper drops CR and LF so base64 bodies wrapped at 76 columns decode.
type newlineStripper struct{ reader io.Reader }

func (n newlineStripper) Read(buffer []byte) (int, error) {
	count, err := n.reader.Read(buffer)
	kept := 0
	for _, value := range buffer[:count] {
		if value != '\r' && value != '\n' {
			buffer[kept] = value
			kept++
		}
	}
	return kept, err
}

var (
	scriptPattern  = regexp.MustCompile(`(?is)<(script|style|head)\b.*?</(script|style|head)\s*>`)
	breakPattern   = regexp.MustCompile(`(?i)<br\s*/?>|</(p|div|li|tr|h[1-6]|blockquote)\s*>`)
	tagPattern     = regexp.MustCompile(`(?s)<[^>]*>`)
	blankLines     = regexp.MustCompile(`\n{3,}`)
	trailingSpaces = regexp.MustCompile(`[ \t]+\n`)
)

func htmlToText(value string) string {
	value = scriptPattern.ReplaceAllString(value, "")
	value = breakPattern.ReplaceAllString(value, "\n")
	value = tagPattern.ReplaceAllString(value, "")
	return strings.ReplaceAll(html.UnescapeString(value), " ", " ")
}

// HTMLToText reduces HTML to plain text the way inbound mail bodies are reduced.
func HTMLToText(value string) string {
	return normalizeText(htmlToText(value))
}

func normalizeText(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	value = strings.ReplaceAll(value, "\x00", "")
	value = trailingSpaces.ReplaceAllString(value, "\n")
	return strings.TrimSpace(blankLines.ReplaceAllString(value, "\n\n"))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

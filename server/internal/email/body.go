package email

import (
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

type content struct {
	plain, html string
	attachments []domain.EmailAttachment
}

// text prefers the first text/plain part and falls back to the first text/html part reduced to text.
func (c content) text() string {
	if strings.TrimSpace(c.plain) != "" {
		return normalizeText(c.plain)
	}
	return normalizeText(htmlToText(c.html))
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
	disposition, dispositionParams, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
	filename := decodeHeader(firstNonEmpty(dispositionParams["filename"], params["name"]))
	isText := mediaType == "text/plain" || mediaType == "text/html"
	if disposition == "attachment" || filename != "" || !isText {
		if filename == "" {
			filename = "(unnamed)"
		}
		result.attachments = append(result.attachments, domain.EmailAttachment{Filename: cleanLine(filename), ContentType: mediaType})
		return
	}
	text := decodePart(header.Get("Content-Transfer-Encoding"), params["charset"], body)
	if mediaType == "text/plain" && result.plain == "" {
		result.plain = text
	}
	if mediaType == "text/html" && result.html == "" {
		result.html = text
	}
}

func decodePart(encoding, charset string, body io.Reader) string {
	reader := io.LimitReader(body, maxPartBytes)
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
	return strings.ToValidUTF8(string(data), string(utf8.RuneError))
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

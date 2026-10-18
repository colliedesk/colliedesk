package email

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
)

// Outgoing is everything needed to render one reply. Message IDs are stored without angle brackets.
type Outgoing struct {
	FromName    string
	FromAddress string
	ReplyTo     string
	ReplyToName string
	ToName      string
	ToAddress   string
	// Cc are plain addresses copied on the message; there is no Bcc.
	Cc         []string
	Subject    string
	Body       string
	MessageID  string
	InReplyTo  string
	References []string
	Date       time.Time
	// Files are attached to the message; with none, the message is a single text part.
	Files []File
	// Standalone is mail the system writes by itself (alerts, notices, reports): Subject stays as written,
	// not "Re: " in front, and the message is marked Auto-Submitted: auto-generated.
	Standalone bool
}

// Compose renders a plain-text UTF-8 RFC 5322 message, or multipart/mixed with one part per file when
// Files is set. Every header value is single-line by construction.
func Compose(message Outgoing) ([]byte, error) {
	if message.MessageID == "" {
		return nil, fmt.Errorf("compose: message id is required")
	}
	if !validAddress(message.FromAddress) || !validAddress(message.ToAddress) || (message.ReplyTo != "" && !validAddress(message.ReplyTo)) {
		return nil, fmt.Errorf("compose: from, to and reply-to must be plain addresses")
	}
	for _, copied := range message.Cc {
		if !validAddress(copied) {
			return nil, fmt.Errorf("compose: cc must be plain addresses")
		}
	}
	var buffer bytes.Buffer
	header := func(name, value string) {
		if value != "" {
			fmt.Fprintf(&buffer, "%s: %s\r\n", name, value)
		}
	}
	from := &mail.Address{Name: cleanLine(message.FromName), Address: message.FromAddress}
	header("From", from.String())
	header("To", (&mail.Address{Name: cleanLine(message.ToName), Address: message.ToAddress}).String())
	header("Cc", strings.Join(message.Cc, ", "))
	if message.ReplyTo != "" {
		name := from.Name
		if message.ReplyToName != "" {
			name = cleanLine(message.ReplyToName)
		}
		header("Reply-To", (&mail.Address{Name: name, Address: message.ReplyTo}).String())
	}
	header("Subject", foldEncodedWords(mime.QEncoding.Encode("utf-8", subjectFor(message))))
	header("Date", message.Date.UTC().Format(time.RFC1123Z))
	header("Message-ID", angle(message.MessageID))
	if message.Standalone {
		// RFC 3834: mail the system writes says so, and an auto-responder must not answer it.
		header("Auto-Submitted", "auto-generated")
	}
	header("In-Reply-To", angle(message.InReplyTo))
	references := make([]string, 0, len(message.References))
	for _, reference := range message.References {
		references = append(references, angle(reference))
	}
	header("References", strings.Join(references, " "))
	header("MIME-Version", "1.0")
	boundary := ""
	if len(message.Files) > 0 {
		token, err := NewToken()
		if err != nil {
			return nil, err
		}
		boundary = "cd-" + token
		header("Content-Type", `multipart/mixed; boundary="`+boundary+`"`)
		buffer.WriteString("\r\n--" + boundary + "\r\n")
	}
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	buffer.WriteString("\r\n")
	writer := quotedprintable.NewWriter(&buffer)
	body := strings.ReplaceAll(strings.ReplaceAll(message.Body, "\r\n", "\n"), "\n", "\r\n")
	if _, err := writer.Write([]byte(body + "\r\n")); err != nil {
		return nil, fmt.Errorf("encode body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("encode body: %w", err)
	}
	for _, file := range message.Files {
		writeFilePart(&buffer, boundary, file)
	}
	if boundary != "" {
		buffer.WriteString("--" + boundary + "--\r\n")
	}
	return buffer.Bytes(), nil
}

// writeFilePart appends one base64 attachment part. Go's media-type formatter writes a non-ASCII
// filename as an RFC 2231 extended parameter (filename*=utf-8”...).
func writeFilePart(buffer *bytes.Buffer, boundary string, file File) {
	name := cleanLine(file.Filename)
	if name == "" {
		name = "attachment"
	}
	contentType := "application/octet-stream"
	// A part typed multipart/* or message/* would be parsed as mail by the recipient; the file is opaque bytes.
	if parsed, _, err := mime.ParseMediaType(file.ContentType); err == nil && !strings.HasPrefix(parsed, "multipart/") && !strings.HasPrefix(parsed, "message/") {
		contentType = parsed
	}
	typeParams := []string{contentType, parameter("name", name)}
	if fallback := encodedWordParameter("name", name); fallback != "" {
		typeParams = append(typeParams, fallback)
	}
	buffer.WriteString("\r\n--" + boundary + "\r\n")
	fmt.Fprintf(buffer, "Content-Type: %s\r\n", strings.Join(typeParams, foldedSeparator))
	fmt.Fprintf(buffer, "Content-Disposition: attachment%s%s\r\n", foldedSeparator, parameter("filename", name))
	buffer.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	encoded := base64.StdEncoding.EncodeToString(file.Data)
	for len(encoded) > base64LineLength {
		buffer.WriteString(encoded[:base64LineLength] + "\r\n")
		encoded = encoded[base64LineLength:]
	}
	buffer.WriteString(encoded + "\r\n")
}

const base64LineLength = 76

// foldEncodedWords puts each RFC 2047 encoded-word on its own continuation line so no header line
// passes the 998-byte limit. Encoded-words never contain spaces, so the split is unambiguous.
func foldEncodedWords(value string) string {
	return strings.ReplaceAll(value, "?= =?", "?=\r\n =?")
}

const maxSubjectRunes = 250

func subjectFor(message Outgoing) string {
	if message.Standalone {
		return cleanLine(message.Subject)
	}
	return replySubject(message.Subject)
}

func replySubject(subject string) string {
	subject = cleanLine(subject)
	if runes := []rune(subject); len(runes) > maxSubjectRunes {
		subject = string(runes[:maxSubjectRunes])
	}
	if strings.HasPrefix(strings.ToLower(subject), "re:") {
		return subject
	}
	return "Re: " + subject
}

func angle(id string) string {
	id = cleanLine(id)
	if id == "" {
		return ""
	}
	return "<" + strings.Trim(id, "<>") + ">"
}

// NewMessageID returns a globally unique Message-ID (without brackets) on the given domain.
func NewMessageID(domain string) (string, error) {
	token, err := NewToken()
	if err != nil {
		return "", err
	}
	return "cd." + token + "@" + domain, nil
}

// NewToken returns 128 random bits as lowercase hex.
func NewToken() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// ValidAddress reports whether address is a plain addr-spec that a header can carry as written.
func ValidAddress(address string) bool { return validAddress(address) }

func validAddress(address string) bool {
	parsed, err := mail.ParseAddress(address)
	return err == nil && parsed.Address == address
}

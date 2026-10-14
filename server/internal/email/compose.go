package email

import (
	"bytes"
	"crypto/rand"
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
	Subject     string
	Body        string
	MessageID   string
	InReplyTo   string
	References  []string
	Date        time.Time
}

// Compose renders a plain-text UTF-8 RFC 5322 message. Every header value is single-line by construction.
func Compose(message Outgoing) ([]byte, error) {
	if message.MessageID == "" {
		return nil, fmt.Errorf("compose: message id is required")
	}
	if !validAddress(message.FromAddress) || !validAddress(message.ToAddress) || (message.ReplyTo != "" && !validAddress(message.ReplyTo)) {
		return nil, fmt.Errorf("compose: from, to and reply-to must be plain addresses")
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
	if message.ReplyTo != "" {
		name := from.Name
		if message.ReplyToName != "" {
			name = cleanLine(message.ReplyToName)
		}
		header("Reply-To", (&mail.Address{Name: name, Address: message.ReplyTo}).String())
	}
	header("Subject", foldEncodedWords(mime.QEncoding.Encode("utf-8", replySubject(message.Subject))))
	header("Date", message.Date.UTC().Format(time.RFC1123Z))
	header("Message-ID", angle(message.MessageID))
	header("In-Reply-To", angle(message.InReplyTo))
	references := make([]string, 0, len(message.References))
	for _, reference := range message.References {
		references = append(references, angle(reference))
	}
	header("References", strings.Join(references, " "))
	header("MIME-Version", "1.0")
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
	return buffer.Bytes(), nil
}

// foldEncodedWords puts each RFC 2047 encoded-word on its own continuation line so no header line
// passes the 998-byte limit. Encoded-words never contain spaces, so the split is unambiguous.
func foldEncodedWords(value string) string {
	return strings.ReplaceAll(value, "?= =?", "?=\r\n =?")
}

const maxSubjectRunes = 250

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

func validAddress(address string) bool {
	parsed, err := mail.ParseAddress(address)
	return err == nil && parsed.Address == address
}

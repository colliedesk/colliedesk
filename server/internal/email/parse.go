// Package email parses inbound RFC 5322 messages and composes and sends outbound replies.
// It has no database access and no vendor SDK: any forwarder or SMTP relay works.
package email

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"regexp"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"golang.org/x/text/encoding/htmlindex"
)

// Authentication verdicts derived from an upstream Authentication-Results header.
const (
	AuthPass = "pass"
	AuthFail = "fail"
	AuthNone = "none"
)

const maxReferences = 50

// Inbound is the part of a received message CollieDesk stores or uses for threading.
type Inbound struct {
	MessageID      string
	InReplyTo      []string
	References     []string
	FromAddress    string
	FromName       string
	Recipients     []string
	Subject        string
	Body           string
	Attachments    []domain.EmailAttachment
	Authentication string
	DropReason     string
}

var (
	messageIDPattern = regexp.MustCompile(`<([^<>\s]+)>`)
	wordDecoder      = &mime.WordDecoder{CharsetReader: charsetReader}
	addressParser    = &mail.AddressParser{WordDecoder: wordDecoder}
)

// Parse reads one raw RFC 5322 message. It fails only when the message has no usable sender.
func Parse(raw []byte) (Inbound, error) {
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return Inbound{}, fmt.Errorf("read message: %w", err)
	}
	header := message.Header
	from, err := addressParser.Parse(header.Get("From"))
	if err != nil {
		return Inbound{}, fmt.Errorf("parse From: %w", err)
	}
	parsed := Inbound{
		MessageID:   firstID(header.Get("Message-Id")),
		InReplyTo:   ids(header.Get("In-Reply-To")),
		References:  lastN(ids(header.Get("References")), maxReferences),
		FromAddress: strings.ToLower(from.Address),
		FromName:    cleanLine(from.Name),
		Recipients:  recipients(header),
		Subject:     cleanLine(decodeHeader(header.Get("Subject"))),
	}
	if parsed.MessageID == "" {
		digest := sha256.Sum256(raw)
		parsed.MessageID = "sha256-" + hex.EncodeToString(digest[:16]) + "@colliedesk.invalid"
	}
	content := extractContent(textprotoHeader(header), message.Body)
	parsed.Body = content.text()
	parsed.Attachments = content.attachments
	parsed.Authentication = authentication(header["Authentication-Results"])
	parsed.DropReason = dropReason(header, parsed.FromAddress)
	return parsed, nil
}

func firstID(value string) string {
	if found := ids(value); len(found) > 0 {
		return found[0]
	}
	return strings.Trim(strings.TrimSpace(value), "<>")
}

func ids(value string) []string {
	matches := messageIDPattern.FindAllStringSubmatch(value, -1)
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		result = append(result, match[1])
	}
	return result
}

func lastN(values []string, n int) []string {
	if len(values) > n {
		return values[len(values)-n:]
	}
	return values
}

func recipients(header mail.Header) []string {
	var result []string
	for _, name := range []string{"To", "Cc", "Delivered-To", "X-Original-To"} {
		for _, value := range header[name] {
			list, err := addressParser.ParseList(value)
			if err != nil {
				continue
			}
			for _, address := range list {
				result = append(result, strings.ToLower(address.Address))
			}
		}
	}
	return result
}

func decodeHeader(value string) string {
	decoded, err := wordDecoder.DecodeHeader(value)
	if err != nil {
		return value
	}
	return decoded
}

// cleanLine removes line breaks so header values can never inject new headers when reused.
func cleanLine(value string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(value), " "))
}

func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	encoding, err := htmlindex.Get(charset)
	if err != nil {
		return nil, fmt.Errorf("unsupported charset %q: %w", charset, err)
	}
	return encoding.NewDecoder().Reader(input), nil
}

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

const (
	maxReferences   = 50
	maxInReplyTo    = 10
	maxMessageIDLen = 255
)

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
	// BouncedMessageIDs are the Message-IDs named in a delivery status report's returned headers.
	BouncedMessageIDs []string
}

var (
	messageIDPattern = regexp.MustCompile(`<([^<>\s]+)>`)
	wordDecoder      = &mime.WordDecoder{CharsetReader: charsetReader}
	addressParser    = &mail.AddressParser{WordDecoder: wordDecoder}
)

// Parse reads one raw RFC 5322 message. It fails only when the message has no usable sender.
func Parse(raw []byte, trustedAuthServID string) (Inbound, error) {
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
		MessageID:   boundID(firstID(header.Get("Message-Id"))),
		InReplyTo:   firstN(ids(header.Get("In-Reply-To")), maxInReplyTo),
		References:  lastN(ids(header.Get("References")), maxReferences),
		FromAddress: strings.ToLower(from.Address),
		FromName:    cleanLine(from.Name),
		Recipients:  recipients(header),
		Subject:     cleanLine(decodeHeader(header.Get("Subject"))),
	}
	if parsed.MessageID == "" {
		parsed.MessageID = syntheticID(header, raw)
	}
	content := extractContent(textprotoHeader(header), message.Body)
	parsed.Body = content.text()
	parsed.Attachments = content.attachments
	parsed.BouncedMessageIDs = content.bouncedIDs
	parsed.Authentication = authentication(header["Authentication-Results"], trustedAuthServID)
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
		result = append(result, boundID(match[1]))
	}
	return result
}

// boundID keeps Message-IDs indexable: an over-long ID maps to a stable digest, so a duplicate
// delivery and a later reference to the same long ID still match.
func boundID(id string) string {
	if len(id) <= maxMessageIDLen {
		return id
	}
	digest := sha256.Sum256([]byte(id))
	return "long-" + hex.EncodeToString(digest[:16]) + "@colliedesk.invalid"
}

// syntheticID identifies a message without Message-ID by its content, ignoring trace headers that
// each relay hop adds, so a redelivery through another path is still recognised as a duplicate.
func syntheticID(header mail.Header, raw []byte) string {
	hash := sha256.New()
	for _, name := range []string{"From", "To", "Cc", "Subject", "Date", "Content-Type"} {
		fmt.Fprintf(hash, "%s:%s\n", name, strings.Join(header[name], ","))
	}
	if index := bytes.Index(raw, []byte("\r\n\r\n")); index >= 0 {
		hash.Write(raw[index+4:])
	} else if index := bytes.Index(raw, []byte("\n\n")); index >= 0 {
		hash.Write(raw[index+2:])
	}
	return "sha256-" + hex.EncodeToString(hash.Sum(nil)[:16]) + "@colliedesk.invalid"
}

func firstN(values []string, n int) []string {
	if len(values) > n {
		return values[:n]
	}
	return values
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

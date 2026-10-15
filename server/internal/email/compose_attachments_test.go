package email_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/email"
)

func outgoing(files ...email.File) email.Outgoing {
	return email.Outgoing{
		FromName: "Acme", FromAddress: "support@acme.example", ToName: "Ada", ToAddress: "ada@customer.example",
		Subject: "Invoice", Body: "Hello Ada.", MessageID: "cd.abc@acme.example", InReplyTo: "in@customer.example",
		References: []string{"in@customer.example"}, Date: time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC), Files: files,
	}
}

func TestComposeWithoutFilesStaysASingleTextPart(t *testing.T) {
	got, err := email.Compose(outgoing())
	if err != nil {
		t.Fatal(err)
	}
	want := "From: \"Acme\" <support@acme.example>\r\n" +
		"To: \"Ada\" <ada@customer.example>\r\n" +
		"Subject: Re: Invoice\r\n" +
		"Date: Thu, 15 Oct 2026 09:00:00 +0000\r\n" +
		"Message-ID: <cd.abc@acme.example>\r\n" +
		"In-Reply-To: <in@customer.example>\r\n" +
		"References: <in@customer.example>\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n" +
		"\r\n" +
		"Hello Ada.\r\n"
	if string(got) != want {
		t.Fatalf("text-only output changed:\n%q\nwant\n%q", got, want)
	}
}

func TestComposeWithFilesIsMultipartMixedAndKeepsEveryByte(t *testing.T) {
	pdf := []byte("%PDF-1.7\x00\xff binary \r\n tail")
	big := bytes.Repeat([]byte("0123456789"), 1000)
	got, err := email.Compose(outgoing(
		email.File{Filename: "invoice.pdf", ContentType: "application/pdf", Data: pdf},
		email.File{Filename: "Hóa đơn tháng 10.png", ContentType: "image/png", Data: big},
		email.File{Filename: "no-type", Data: []byte("x")},
	))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(got))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Header.Get("Message-Id") != "<cd.abc@acme.example>" || parsed.Header.Get("In-Reply-To") != "<in@customer.example>" {
		t.Fatalf("threading headers lost: %v", parsed.Header)
	}
	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/mixed" {
		t.Fatalf("Content-Type = %q (%v)", parsed.Header.Get("Content-Type"), err)
	}
	reader := multipart.NewReader(parsed.Body, params["boundary"])
	type part struct {
		filename, contentType string
		data                  []byte
	}
	var parts []part
	for {
		next, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(next) // multipart.Reader decodes quoted-printable only; base64 is decoded below
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, part{filename: next.FileName(), contentType: next.Header.Get("Content-Type"), data: decodeTransfer(t, next.Header.Get("Content-Transfer-Encoding"), data)})
	}
	if len(parts) != 4 {
		t.Fatalf("parts = %d, want text + 3 files", len(parts))
	}
	if !strings.HasPrefix(parts[0].contentType, "text/plain") || strings.TrimSpace(string(parts[0].data)) != "Hello Ada." {
		t.Fatalf("text part = %+v", parts[0])
	}
	want := []struct {
		name string
		data []byte
	}{{"invoice.pdf", pdf}, {"Hóa đơn tháng 10.png", big}, {"no-type", []byte("x")}}
	for i, file := range want {
		got := parts[i+1]
		if got.filename != file.name || sha256.Sum256(got.data) != sha256.Sum256(file.data) {
			t.Fatalf("file %d = %q (%d bytes), want %q (%d bytes)", i, got.filename, len(got.data), file.name, len(file.data))
		}
	}
	if !strings.HasPrefix(parts[3].contentType, "application/octet-stream") {
		t.Fatalf("a file without a type must be octet-stream, got %q", parts[3].contentType)
	}
	if !strings.Contains(string(got), "filename*=utf-8''H%C3%B3a%20%C4%91%C6%A1n%20th%C3%A1ng%2010.png") {
		t.Fatalf("a non-ASCII filename must use RFC 2231 encoding:\n%s", got)
	}
	for _, line := range strings.Split(string(got), "\r\n") {
		if len(line) > 998 {
			t.Fatalf("line of %d bytes exceeds RFC 5322 limit", len(line))
		}
	}
}

func TestComposeNeutralisesHeaderInjectionInFilenames(t *testing.T) {
	got, err := email.Compose(outgoing(email.File{Filename: "a.txt\r\nBcc: spy@evil.example", ContentType: "text/plain\r\nX: y", Data: []byte("x")}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "\r\nBcc:") || strings.Contains(string(got), "\r\nX: y") {
		t.Fatalf("a header was injected:\n%s", got)
	}
}

func decodeTransfer(t *testing.T, encoding string, data []byte) []byte {
	t.Helper()
	if strings.ToLower(encoding) != "base64" {
		return data
	}
	decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewReader(bytes.ReplaceAll(data, []byte("\r\n"), nil))))
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

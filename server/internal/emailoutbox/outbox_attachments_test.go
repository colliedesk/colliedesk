package emailoutbox_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/emailoutbox"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

type attachedFile struct {
	name, contentType string
	data              []byte
}

func (f fixture) replyWithFiles(t *testing.T, storage attachments.Storage, files ...attachedFile) (domain.Message, []string) {
	t.Helper()
	var stored []store.NewAttachment
	var keys []string
	for _, file := range files {
		object, err := storage.Put(context.Background(), f.workspaceID, bytes.NewReader(file.data), int64(len(file.data)))
		if err != nil {
			t.Fatal(err)
		}
		stored = append(stored, store.NewAttachment{Filename: file.name, ContentType: file.contentType, Object: object})
		keys = append(keys, object.Key)
	}
	message, err := f.store.CreateMessage(context.Background(), f.workspaceID, f.conversationID, f.agentID, "outgoing", "Files are attached.", stored...)
	if err != nil {
		t.Fatal(err)
	}
	return message, keys
}

func TestDeliveredReplyCarriesEveryAttachmentByteForByte(t *testing.T) {
	f := newFixture(t)
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	worker := newWorker(f, server, &clock, func(options *emailoutbox.Options) { options.Storage = storage })
	pdf := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte{0x00, 0xff, 0x10, 0x80}, 5000)...)
	png := []byte("\x89PNG\r\n\x1a\nimage")
	reply, _ := f.replyWithFiles(t, storage,
		attachedFile{"Báo giá.pdf", "application/pdf", pdf}, attachedFile{"shot.png", "image/png", png})
	if process(t, worker) != 1 {
		t.Fatal("the reply was not attempted")
	}
	if delivery := f.delivery(t, reply.ID); delivery.Status != "sent" {
		t.Fatalf("delivery = %+v", delivery)
	}
	received := server.mail()
	if len(received) != 1 {
		t.Fatalf("mail received = %d", len(received))
	}
	parsed, err := mail.ReadMessage(strings.NewReader(received[0].Data))
	if err != nil {
		t.Fatal(err)
	}
	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/mixed" {
		t.Fatalf("Content-Type = %q", parsed.Header.Get("Content-Type"))
	}
	reader := multipart.NewReader(parsed.Body, params["boundary"])
	got := map[string][32]byte{}
	var texts int
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(part)
		if part.FileName() == "" {
			texts++
			continue
		}
		decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewReader(bytes.ReplaceAll(raw, []byte("\r\n"), nil))))
		if err != nil {
			t.Fatal(err)
		}
		got[part.FileName()] = sha256.Sum256(decoded)
	}
	if texts != 1 || got["Báo giá.pdf"] != sha256.Sum256(pdf) || got["shot.png"] != sha256.Sum256(png) || len(got) != 2 {
		t.Fatalf("text parts=%d, attachment checksums=%x", texts, got)
	}
}

func TestReplyWhoseFileIsGoneFailsWithAReasonInsteadOfSendingTextOnly(t *testing.T) {
	f := newFixture(t)
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	worker := newWorker(f, server, &clock, func(options *emailoutbox.Options) { options.Storage = storage })
	reply, keys := f.replyWithFiles(t, storage, attachedFile{"lost.pdf", "application/pdf", []byte("bytes that vanish")})
	if err := storage.Delete(context.Background(), keys[0]); err != nil {
		t.Fatal(err)
	}
	process(t, worker)
	delivery := f.delivery(t, reply.ID)
	if delivery.Status != "failed" || delivery.LastError == nil || !strings.Contains(*delivery.LastError, "lost.pdf") {
		t.Fatalf("delivery = %+v", delivery)
	}
	if len(server.mail()) != 0 {
		t.Fatal("a reply missing its file must not be sent")
	}
}

func TestReplyWithFilesFailsWhenTheWorkerHasNoStorage(t *testing.T) {
	f := newFixture(t)
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	reply, _ := f.replyWithFiles(t, storage, attachedFile{"a.txt", "text/plain", []byte("a")})
	process(t, newWorker(f, server, &clock))
	if delivery := f.delivery(t, reply.ID); delivery.Status != "failed" || len(server.mail()) != 0 {
		t.Fatalf("delivery = %+v, mail=%d", delivery, len(server.mail()))
	}
}

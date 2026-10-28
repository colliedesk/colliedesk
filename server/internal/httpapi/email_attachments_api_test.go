package httpapi_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	invoicePDF = []byte("%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n")
	logoPNG    = []byte("\x89PNG\r\n\x1a\nfake-png-bytes-for-a-tiny-logo")
)

// mailWithFiles is a customer mail with an HTML body that shows an inline image by cid and a PDF attachment.
func mailWithFiles(messageID string) string {
	return mail([]string{
		"From: Vic Customer <vic@customer.example>", "To: support@files.example.com", "Subject: Invoice dispute",
		"Message-ID: <" + messageID + ">", "MIME-Version: 1.0", `Content-Type: multipart/mixed; boundary="outer"`,
	}, "--outer\nContent-Type: multipart/related; boundary=\"rel\"\n\n"+
		"--rel\nContent-Type: text/html; charset=utf-8\n\n<p>See the logo <img src=\"cid:logo@customer.example\"> and <img src=\"https://tracker.example/p.png\"></p>\n"+
		"--rel\nContent-Type: image/png\nContent-ID: <logo@customer.example>\nContent-Disposition: inline; filename=\"logo.png\"\nContent-Transfer-Encoding: base64\n\n"+
		base64.StdEncoding.EncodeToString(logoPNG)+"\n--rel--\n"+
		"--outer\nContent-Type: application/pdf; name=\"invoice.pdf\"\nContent-Disposition: attachment; filename=\"invoice.pdf\"\nContent-Transfer-Encoding: base64\n\n"+
		base64.StdEncoding.EncodeToString(invoicePDF)+"\n--outer--\n")
}

type storedMessage struct {
	ID          string  `json:"id"`
	BodyHTML    *string `json:"body_html"`
	Attachments []struct {
		ID          string `json:"id"`
		Filename    string `json:"filename"`
		ContentType string `json:"content_type"`
		Size        int64  `json:"size"`
		SHA256      string `json:"sha256"`
	} `json:"attachments"`
	Email *struct {
		OriginalAttachmentID *string `json:"original_attachment_id"`
	} `json:"email"`
}

func storedMessages(t *testing.T, api http.Handler, token, conversationID string) []storedMessage {
	t.Helper()
	var list struct {
		Items []storedMessage `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+conversationID+"/messages", nil, token, nil), &list)
	return list.Items
}

func download(t *testing.T, api http.Handler, token, attachmentID string) (int, []byte, string) {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/attachments/"+attachmentID, nil, token, nil)
	return response.Code, response.Body.Bytes(), response.Header().Get("X-Content-SHA256")
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestInboundAttachmentsInlineImagesAndRawMessageAreKept(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Files Desk", "admin@files.example.com")
	secret := connectEmail(t, api, admin, "support@files.example.com")
	raw := mailWithFiles("files-1@customer.example")
	result := inboundOK(t, api, secret, raw)
	if result.Status != "created" {
		t.Fatalf("inbound: %+v", result)
	}
	items := storedMessages(t, api, admin, *result.ConversationID)
	if len(items) != 1 || items[0].Email == nil || items[0].Email.OriginalAttachmentID == nil {
		t.Fatalf("message lacks the original: %+v", items)
	}
	files := map[string]int{}
	for index, file := range items[0].Attachments {
		files[file.Filename] = index
	}
	invoice, ok := files["invoice.pdf"]
	if !ok {
		t.Fatalf("invoice not stored: %+v", items[0].Attachments)
	}
	if got := items[0].Attachments[invoice]; got.SHA256 != sha(invoicePDF) || got.Size != int64(len(invoicePDF)) || got.ContentType != "application/pdf" {
		t.Fatalf("invoice metadata: %+v", got)
	}
	status, body, checksum := download(t, api, admin, items[0].Attachments[invoice].ID)
	if status != http.StatusOK || !bytes.Equal(body, invoicePDF) || checksum != sha(invoicePDF) {
		t.Fatalf("invoice download status=%d checksum=%s bytes equal=%v", status, checksum, bytes.Equal(body, invoicePDF))
	}
	logo, ok := files["logo.png"]
	if !ok || items[0].Attachments[logo].SHA256 != sha(logoPNG) {
		t.Fatalf("inline image not stored: %+v", items[0].Attachments)
	}
	html := ""
	if items[0].BodyHTML != nil {
		html = *items[0].BodyHTML
	}
	if !strings.Contains(html, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(logoPNG)) || strings.Contains(html, "cid:") {
		t.Fatalf("inline image not referenced locally: %q", html)
	}
	status, body, _ = download(t, api, admin, *items[0].Email.OriginalAttachmentID)
	if status != http.StatusOK {
		t.Fatalf("original download status=%d", status)
	}
	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	original, err := io.ReadAll(reader)
	if err != nil || string(original) != raw {
		t.Fatalf("original message changed (err %v)", err)
	}

	if again := inboundOK(t, api, secret, raw); again.Status != "duplicate" {
		t.Fatalf("redelivery: %+v", again)
	}
	if after := storedMessages(t, api, admin, *result.ConversationID); len(after) != 1 || len(after[0].Attachments) != len(items[0].Attachments) {
		t.Fatal("a duplicate delivery added attachments")
	}
}

func TestInboundAttachmentDownloadIsWorkspaceScoped(t *testing.T) {
	api, service := testAPI(t)
	adminA := createSession(t, api, service, "Files Alpha", "admin@files-alpha.example.com")
	adminB := createSession(t, api, service, "Files Beta", "admin@files-beta.example.com")
	secretA := connectEmail(t, api, adminA, "support@files.example.com")
	result := inboundOK(t, api, secretA, mailWithFiles("scoped@customer.example"))
	items := storedMessages(t, api, adminA, *result.ConversationID)
	for _, file := range items[0].Attachments {
		if status, _, _ := download(t, api, adminB, file.ID); status != http.StatusNotFound {
			t.Fatalf("workspace B downloaded %s from A: status=%d", file.Filename, status)
		}
	}
}

// storedFiles lists the object files below a storage root, ignoring the temp directory.
func storedFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".incoming" {
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func countRows(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	return queryString(t, pool, `SELECT (SELECT count(*) FROM conversations)||'/'||(SELECT count(*) FROM contacts)||'/'||
		(SELECT count(*) FROM messages)||'/'||(SELECT count(*) FROM attachments)`)
}

func storageAt(t *testing.T) (string, attachments.Storage) {
	t.Helper()
	root := t.TempDir()
	storage, err := attachments.NewLocalDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, storage
}

func TestOversizedInboundMailIsRejectedBeforeAnythingIsStored(t *testing.T) {
	root, storage := storageAt(t)
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Attachments = storage })
	admin := createSession(t, api, service, "Big Desk", "admin@big.example.com")
	secret := connectEmail(t, api, admin, "support@files.example.com")
	big := mail([]string{"From: w@customer.example", "Subject: Huge", "Message-ID: <huge@customer.example>",
		"Content-Type: application/octet-stream", "Content-Disposition: attachment; filename=\"huge.bin\""}, strings.Repeat("A", 26<<20))
	response := inbound(t, api, secret, big)
	if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), "25 MiB") {
		t.Fatalf("oversized status=%d body=%s", response.Code, response.Body.String())
	}
	if rows := countRows(t, pool); rows != "0/0/0/0" {
		t.Fatalf("rows after rejection (conversations/contacts/messages/attachments) = %s", rows)
	}
	if files := storedFiles(t, root); len(files) != 0 {
		t.Fatalf("files after rejection: %v", files)
	}
}

func TestDroppedAndDuplicateMailStoreNoFiles(t *testing.T) {
	root, storage := storageAt(t)
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Attachments = storage })
	admin := createSession(t, api, service, "Drop Files", "admin@drop-files.example.com")
	secret := connectEmail(t, api, admin, "support@files.example.com")
	auto := strings.Replace(mailWithFiles("auto@customer.example"), "Subject: Invoice dispute", "Subject: Out of office\r\nAuto-Submitted: auto-replied", 1)
	if result := inboundOK(t, api, secret, auto); result.Status != "dropped" {
		t.Fatalf("auto reply: %+v", result)
	}
	if files := storedFiles(t, root); len(files) != 0 || countRows(t, pool) != "0/0/0/0" {
		t.Fatalf("dropped mail stored files %v or rows %s", files, countRows(t, pool))
	}
}

// failingStorage stands in for a storage backend outage after the first object.
type failingStorage struct {
	attachments.Storage
	puts      atomic.Int32
	failAfter int32
}

func (f *failingStorage) Put(ctx context.Context, workspaceID string, body io.Reader, size int64) (attachments.Object, error) {
	if f.puts.Add(1) > f.failAfter {
		return attachments.Object{}, errors.New("disk unavailable")
	}
	return f.Storage.Put(ctx, workspaceID, body, size)
}

func TestInboundStorageFailureLeavesNoRowsAndCanBeRetried(t *testing.T) {
	root, disk := storageAt(t)
	flaky := &failingStorage{Storage: disk, failAfter: 1}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Attachments = flaky })
	admin := createSession(t, api, service, "Flaky Desk", "admin@flaky.example.com")
	secret := connectEmail(t, api, admin, "support@files.example.com")
	raw := mailWithFiles("flaky@customer.example")
	if response := inbound(t, api, secret, raw); response.Code != http.StatusInternalServerError {
		t.Fatalf("storage failure status=%d, want 500 so the forwarder retries", response.Code)
	}
	if rows := countRows(t, pool); rows != "0/0/0/0" {
		t.Fatalf("rows after storage failure = %s", rows)
	}
	flaky.failAfter = 1 << 20
	if result := inboundOK(t, api, secret, raw); result.Status != "created" {
		t.Fatalf("retry after recovery: %+v", result)
	}
	// Objects are content-addressed and may be shared, so a failed request never deletes them; the
	// forwarder's retry stores the same bytes under the same keys and references every one.
	files := storedFiles(t, root)
	referenced := queryString(t, pool, `SELECT count(DISTINCT storage_key)::text FROM attachments`)
	if referenced != strconv.Itoa(len(files)) {
		t.Fatalf("%d files on disk but %s referenced after the retry: %v", len(files), referenced, files)
	}
}

func TestInboundRejectsMailWhenStorageIsNotConfigured(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Attachments = nil })
	admin := createSession(t, api, service, "No Storage", "admin@nostorage.example.com")
	secret := connectEmail(t, api, admin, "support@files.example.com")
	if response := inbound(t, api, secret, mailWithFiles("nostorage@customer.example")); response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500", response.Code)
	}
	if rows := countRows(t, pool); rows != "0/0/0/0" {
		t.Fatalf("rows = %s", rows)
	}
}

const platformSecret = "platform-inbound-secret-0123456789abcdef"

func platformConfig(config *httpapi.Config) {
	config.Email.MailFromDomain = "mail.colliedesk.test"
	config.Email.PlatformInboundSecret = platformSecret
}

func replyToken(t *testing.T, api http.Handler, pool *pgxpool.Pool, admin, secret, messageID string) (string, string, string) {
	t.Helper()
	thread := inboundOK(t, api, secret, mail([]string{"From: z@customer.example", "Subject: Platform", "Message-ID: <" + messageID + ">"}, "Hi."))
	reply := create(t, api, admin, "/api/v1/conversations/"+*thread.ConversationID+"/messages", map[string]any{"kind": "outgoing", "body": "Answer."})
	token := queryString(t, pool, `SELECT substr(key,13) FROM conversation_thread_keys WHERE conversation_id=$1 AND channel_type='email'`, *thread.ConversationID)
	return *thread.ConversationID, reply, token
}

func TestPlatformDomainRepliesReachTheirWorkspace(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, platformConfig)
	adminA := createSession(t, api, service, "Platform Alpha", "admin@platform-alpha.example.com")
	adminB := createSession(t, api, service, "Platform Beta", "admin@platform-beta.example.com")
	conversationA, _, tokenA := replyToken(t, api, pool, adminA, connectEmail(t, api, adminA, "support@alpha.example.com"), "pa@customer.example")
	conversationB, _, tokenB := replyToken(t, api, pool, adminB, connectEmail(t, api, adminB, "support@beta.example.com"), "pb@customer.example")

	for _, item := range []struct{ token, conversation, admin string }{{tokenA, conversationA, adminA}, {tokenB, conversationB, adminB}} {
		result := inboundOK(t, api, platformSecret, mail([]string{
			"From: z@customer.example", "To: \"Desk via Colliedesk\" <reply+" + item.token + "@mail.colliedesk.test>",
			"Subject: Re: Platform", "Message-ID: <platform-" + item.token + "@customer.example>",
		}, "Replied to the From address."))
		if result.Status != "threaded" || *result.ConversationID != item.conversation {
			t.Fatalf("token %s: %+v, want threaded into %s", item.token, result, item.conversation)
		}
		if items := timeline(t, api, item.admin, item.conversation); len(items) != 3 || items[2].Body != "Replied to the From address." {
			t.Fatalf("reply not in the owning workspace: %+v", items)
		}
	}
	envelope := inboundOK(t, api, platformSecret, mail([]string{"X-Original-To: reply+" + tokenA + "@mail.colliedesk.test",
		"From: z@customer.example", "To: undisclosed-recipients:;", "Subject: Bcc", "Message-ID: <bcc@customer.example>"}, "Envelope only."))
	if envelope.Status != "threaded" || *envelope.ConversationID != conversationA {
		t.Fatalf("envelope recipient not used: %+v", envelope)
	}
	unknown := inboundOK(t, api, platformSecret, mail([]string{"From: z@customer.example", "To: reply+0123456789abcdef0123456789abcdef@mail.colliedesk.test",
		"Subject: ?", "Message-ID: <unknown@customer.example>"}, "Nobody."))
	if unknown.Status != "dropped" || unknown.Reason == nil || *unknown.Reason != "unknown_recipient" {
		t.Fatalf("unknown token: %+v", unknown)
	}
	foreignDomain := inboundOK(t, api, platformSecret, mail([]string{"From: z@customer.example", "To: reply+" + tokenA + "@elsewhere.example",
		"Subject: ?", "Message-ID: <foreign@customer.example>"}, "Wrong domain."))
	if foreignDomain.Status != "dropped" || *foreignDomain.Reason != "unknown_recipient" {
		t.Fatalf("a token on another domain must not route: %+v", foreignDomain)
	}
	if response := inbound(t, api, platformSecret+"x", mail([]string{"From: z@customer.example", "Subject: x"}, "x")); response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong platform secret status=%d", response.Code)
	}
	withoutSecret, _, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Email.MailFromDomain = "mail.colliedesk.test" })
	if response := inbound(t, withoutSecret, platformSecret, mail([]string{"From: z@customer.example", "Subject: x"}, "x")); response.Code != http.StatusUnauthorized {
		t.Fatalf("platform secret accepted while unset: status=%d", response.Code)
	}
}

func TestPlatformDomainBounceMarksTheReplyFailed(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, platformConfig)
	admin := createSession(t, api, service, "Platform Bounce", "admin@platform-bounce.example.com")
	conversation, reply, token := replyToken(t, api, pool, admin, connectEmail(t, api, admin, "support@bounce.example.com"), "pbounce@customer.example")
	ourID := queryString(t, pool, `SELECT substr(external_id,7) FROM messages WHERE id=$1`, reply)
	if _, err := pool.Exec(t.Context(), `UPDATE channel_outbox SET status='sent',sent_at=now(),attempts=1`); err != nil {
		t.Fatal(err)
	}
	dsn := inboundOK(t, api, platformSecret, mail([]string{
		"From: Mail Delivery System <MAILER-DAEMON@mx.customer.example>", "To: reply+" + token + "@mail.colliedesk.test",
		"Subject: Undelivered Mail Returned to Sender", "Message-ID: <pdsn@mx.customer.example>", "MIME-Version: 1.0",
		`Content-Type: multipart/report; report-type=delivery-status; boundary="r1"`,
	}, "--r1\nContent-Type: text/plain\n\nNo such user.\n--r1\nContent-Type: text/rfc822-headers\n\nMessage-ID: <"+ourID+">\n--r1--\n"))
	if dsn.Status != "dropped" || *dsn.Reason != "mailer_daemon" {
		t.Fatalf("bounce: %+v", dsn)
	}
	items := timeline(t, api, admin, conversation)
	if items[1].Delivery == nil || items[1].Delivery.Status != "failed" {
		t.Fatalf("bounced reply delivery: %+v", items[1].Delivery)
	}
}

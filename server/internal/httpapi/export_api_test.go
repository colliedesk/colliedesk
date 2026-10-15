package httpapi_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

// helloSHA256 is the SHA-256 of the bytes "hello" (FIPS 180-4 test vector).
const helloSHA256 = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

type exportFixture struct {
	api                        http.Handler
	pool                       *pgxpool.Pool
	storage                    attachments.Storage
	adminToken, agentToken     string
	workspaceID, attachmentID  string
	otherToken, otherWorkspace string
	inboundSecret              string
}

// newExportFixture seeds workspace A with 2 contacts, 3 conversations, 5 messages, 1 tag,
// 1 attachment holding "hello" and 1 time entry, and workspace B with data of its own.
func newExportFixture(t *testing.T) exportFixture {
	t.Helper()
	api, service, pool, storage := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Export Acme", "admin@export-acme.example.com")
	inviteURL := func() string {
		_, url := createContractInvite(t, api, admin, "agent@export-acme.example.com", "agent")
		return url
	}()
	redeem := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, inviteURL)}, "", nil)
	var agent struct {
		Token string `json:"token"`
	}
	decode(t, redeem, &agent)

	contactA := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Ann Export", "email": "ann@customer-a.example.com"})
	contactB := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Bob Export", "email": "bob@customer-a.example.com"})
	var conversations []string
	for index, subject := range []string{"First", "Second", "Third"} {
		contact := contactA
		if index == 2 {
			contact = contactB
		}
		conversations = append(conversations, create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": subject}))
	}
	var firstMessage string
	for index, conversation := range []string{conversations[0], conversations[0], conversations[1], conversations[2], conversations[2]} {
		id := create(t, api, admin, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "note", "body": "note " + string(rune('A'+index))})
		if index == 0 {
			firstMessage = id
		}
	}
	tag := create(t, api, admin, "/api/v1/tags", map[string]any{"name": "vip"})
	if response := request(t, api, http.MethodPut, "/api/v1/conversations/"+conversations[0]+"/tags/"+tag, nil, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("tag conversation status=%d", response.Code)
	}
	workspace := workspaceID(t, api, admin)
	attachmentID := seedAttachment(t, pool, storage, admin, api, firstMessage, "hello.txt", "hello")
	seedImportDetails(t, pool, workspace, conversations[0], firstMessage)
	secret := connectEmail(t, api, admin, "support@export-acme.example.com")

	other := createSession(t, api, service, "Export Rival", "admin@export-rival.example.com")
	otherContact := create(t, api, other, "/api/v1/contacts", map[string]any{"name": "Bea Only", "email": "b-only@example.com"})
	otherConversation := create(t, api, other, "/api/v1/conversations", map[string]any{"contact_id": otherContact, "subject": "Rival secret plans"})
	otherMessage := create(t, api, other, "/api/v1/conversations/"+otherConversation+"/messages", map[string]any{"kind": "note", "body": "rival-only body"})
	seedAttachment(t, pool, storage, other, api, otherMessage, "rival.txt", "rival bytes")
	return exportFixture{api: api, pool: pool, storage: storage, adminToken: admin, agentToken: agent.Token,
		workspaceID: workspace, attachmentID: attachmentID, otherToken: other, otherWorkspace: workspaceID(t, api, other), inboundSecret: secret}
}

func unzip(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("response is not a valid zip: %v", err)
	}
	entries := map[string][]byte{}
	for _, file := range reader.File {
		handle, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(handle)
		handle.Close()
		if err != nil {
			t.Fatalf("read %s: %v", file.Name, err)
		}
		entries[file.Name] = content
	}
	return entries
}

func ndjsonLines(t *testing.T, content []byte) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(content), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("bad NDJSON line %q: %v", line, err)
		}
		lines = append(lines, record)
	}
	return lines
}

func TestExportStreamsTheWholeWorkspaceAsZip(t *testing.T) {
	f := newExportFixture(t)
	response := request(t, f.api, http.MethodGet, "/api/v1/export", nil, f.adminToken, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("Content-Type=%q", got)
	}
	if got := response.Header().Get("Content-Disposition"); !regexp.MustCompile(`^attachment; filename="colliedesk-export-acme-\d{8}T\d{6}Z\.zip"$`).MatchString(got) {
		t.Fatalf("Content-Disposition=%q", got)
	}
	entries := unzip(t, response.Body.Bytes())

	var manifest struct {
		Format    string         `json:"format"`
		Version   int            `json:"version"`
		Workspace map[string]any `json:"workspace"`
		Counts    map[string]int `json:"counts"`
		Files     []struct {
			Path   string `json:"path"`
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatalf("manifest.json: %v", err)
	}
	if manifest.Format != "colliedesk-export" || manifest.Version != 1 || manifest.Workspace["slug"] != "export-acme" {
		t.Fatalf("manifest header = %+v", manifest)
	}
	for key, want := range map[string]int{"contacts": 2, "conversations": 3, "messages": 5, "tags": 1, "attachments": 1, "agents": 2, "time_entries": 1} {
		if manifest.Counts[key] != want {
			t.Errorf("counts[%s]=%d, want %d", key, manifest.Counts[key], want)
		}
	}
	path := "attachments/" + f.attachmentID + "/hello.txt"
	if len(manifest.Files) != 1 || manifest.Files[0].Path != path || manifest.Files[0].SHA256 != helloSHA256 || manifest.Files[0].Size != 5 {
		t.Fatalf("manifest files = %+v", manifest.Files)
	}
	if string(entries[path]) != "hello" {
		t.Fatalf("attachment bytes = %q", entries[path])
	}

	if got := len(ndjsonLines(t, entries["contacts.ndjson"])); got != 2 {
		t.Errorf("contacts.ndjson has %d lines", got)
	}
	if got := len(ndjsonLines(t, entries["tags.ndjson"])); got != 1 {
		t.Errorf("tags.ndjson has %d lines", got)
	}
	if got := len(ndjsonLines(t, entries["agents.ndjson"])); got != 2 {
		t.Errorf("agents.ndjson has %d lines", got)
	}
	conversations := ndjsonLines(t, entries["conversations.ndjson"])
	if len(conversations) != 3 {
		t.Fatalf("conversations.ndjson has %d lines", len(conversations))
	}
	messages, tagged, timeEntries, withAttachment := 0, 0, 0, 0
	for _, conversation := range conversations {
		list := conversation["messages"].([]any)
		messages += len(list)
		if ids := conversation["tag_ids"].([]any); len(ids) == 1 {
			tagged++
		}
		timeEntries += len(conversation["time_entries"].([]any))
		for _, item := range list {
			if files := item.(map[string]any)["attachments"].([]any); len(files) == 1 {
				withAttachment++
				if files[0].(map[string]any)["path"] != path {
					t.Errorf("attachment metadata path = %v", files[0])
				}
			}
		}
	}
	if messages != 5 || tagged != 1 || timeEntries != 1 || withAttachment != 1 {
		t.Fatalf("messages=%d tagged=%d timeEntries=%d withAttachment=%d", messages, tagged, timeEntries, withAttachment)
	}
}

func TestExportNeverContainsAnotherWorkspace(t *testing.T) {
	f := newExportFixture(t)
	response := request(t, f.api, http.MethodGet, "/api/v1/export", nil, f.adminToken, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
	var all bytes.Buffer
	for name, content := range unzip(t, response.Body.Bytes()) {
		all.WriteString(name + "\n")
		all.Write(content)
	}
	for _, foreign := range []string{"b-only@example.com", "Bea Only", "Rival secret plans", "rival-only body", "rival.txt", "rival bytes", f.otherWorkspace, "export-rival", "admin@export-rival.example.com"} {
		if strings.Contains(all.String(), foreign) {
			t.Errorf("workspace A export contains %q of workspace B", foreign)
		}
	}
	reverse := request(t, f.api, http.MethodGet, "/api/v1/export", nil, f.otherToken, nil)
	if reverse.Code != http.StatusOK {
		t.Fatalf("workspace B status=%d", reverse.Code)
	}
	entries := unzip(t, reverse.Body.Bytes())
	if got := len(ndjsonLines(t, entries["conversations.ndjson"])); got != 1 {
		t.Fatalf("workspace B export has %d conversations, want 1", got)
	}
	if bytes.Contains(entries["contacts.ndjson"], []byte("customer-a.example.com")) {
		t.Fatal("workspace B export contains a contact of workspace A")
	}
}

func TestExportOmitsSecrets(t *testing.T) {
	f := newExportFixture(t)
	response := request(t, f.api, http.MethodGet, "/api/v1/export", nil, f.adminToken, nil)
	var all bytes.Buffer
	for _, content := range unzip(t, response.Body.Bytes()) {
		all.Write(content)
	}
	for label, secret := range map[string]string{"session token": f.adminToken, "agent session token": f.agentToken, "inbound secret": f.inboundSecret} {
		if secret == "" || strings.Contains(all.String(), secret) {
			t.Errorf("export contains the %s (or it was empty)", label)
		}
	}
	for _, column := range []string{"token_hash", "secret_hash", "email_reply_token", "storage_key"} {
		if strings.Contains(all.String(), column) {
			t.Errorf("export mentions internal column %q", column)
		}
	}
}

func TestExportIsAdminOnly(t *testing.T) {
	f := newExportFixture(t)
	if got := request(t, f.api, http.MethodGet, "/api/v1/export", nil, f.agentToken, nil).Code; got != http.StatusForbidden {
		t.Errorf("agent status=%d, want 403", got)
	}
	if got := request(t, f.api, http.MethodGet, "/api/v1/export", nil, "", nil).Code; got != http.StatusUnauthorized {
		t.Errorf("anonymous status=%d, want 401", got)
	}
	if auth.HasPermission("agent", auth.PermissionWorkspaceExport) || !auth.HasPermission("admin", auth.PermissionWorkspaceExport) {
		t.Error("workspace:export must belong to admins only")
	}
}

func TestExportWorksWhenBillingIsConfigured(t *testing.T) {
	fakePolar := newFakePolarAPI(t)
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Billing = fakePolar.config() })
	admin := createSession(t, api, service, "Export Billed", "admin@export-billed.example.com")
	response := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if unzip(t, response.Body.Bytes())["manifest.json"] == nil {
		t.Fatal("no manifest in an empty workspace export")
	}
}

func TestSecondExportOfTheSameWorkspaceIsRefusedWhileOneRuns(t *testing.T) {
	f := newExportFixture(t)
	conn, err := f.pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_lock(hashtextextended($1,0))`, "export:"+f.workspaceID); err != nil {
		t.Fatal(err)
	}
	response := request(t, f.api, http.MethodGet, "/api/v1/export", nil, f.adminToken, nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s, want 409", response.Code, response.Body.String())
	}
	if got := request(t, f.api, http.MethodGet, "/api/v1/export", nil, f.otherToken, nil).Code; got != http.StatusOK {
		t.Fatalf("another workspace's export status=%d, want 200", got)
	}
	if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1,0))`, "export:"+f.workspaceID); err != nil {
		t.Fatal(err)
	}
	if got := request(t, f.api, http.MethodGet, "/api/v1/export", nil, f.adminToken, nil).Code; got != http.StatusOK {
		t.Fatalf("export after release status=%d, want 200", got)
	}
}

func TestLostAttachmentFileAbortsTheStreamInsteadOfSucceeding(t *testing.T) {
	f := newExportFixture(t)
	var key string
	if err := f.pool.QueryRow(context.Background(), `SELECT storage_key FROM attachments WHERE id=$1`, f.attachmentID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if err := f.storage.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	var aborted any
	func() {
		defer func() { aborted = recover() }()
		request(t, f.api, http.MethodGet, "/api/v1/export", nil, f.adminToken, nil)
	}()
	if aborted != http.ErrAbortHandler {
		t.Fatalf("handler ended with %v, want the connection to be aborted", aborted)
	}
}

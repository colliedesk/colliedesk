package freshdesk_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

func TestTimestampsArePreservedInUTC(t *testing.T) {
	env := newImportEnv(t)
	env.mustRun(t)
	data := store.New(env.pool)
	ctx := context.Background()

	// 23:30Z is already the next calendar day in UTC+7; the session runs in UTC+7.
	conversation, err := data.GetConversation(ctx, env.workspaceID, env.target(t, "ticket", "1"))
	if err != nil {
		t.Fatal(err)
	}
	assertUTC(t, "ticket created_at", conversation.CreatedAt, "2025-01-15T23:30:00Z")
	assertUTC(t, "ticket updated_at", conversation.UpdatedAt, "2025-01-16T08:00:00Z")
	messages, err := data.ListMessages(ctx, env.workspaceID, conversation.ID)
	if err != nil || len(messages) != 4 {
		t.Fatalf("messages=%d err=%v", len(messages), err)
	}
	assertUTC(t, "description", messages[0].CreatedAt, "2025-01-15T23:30:00Z")
	assertUTC(t, "private note", messages[3].CreatedAt, "2025-01-16T07:59:59Z")
	contact, err := data.GetContact(ctx, env.workspaceID, env.target(t, "contact", "201"))
	if err != nil {
		t.Fatal(err)
	}
	assertUTC(t, "contact created_at", contact.CreatedAt, "2024-03-01T12:00:00Z")
	if contact.EmailOf() != "carol@globex.example" || contact.Organization == nil || *contact.Organization != "Globex" {
		t.Fatalf("contact = %+v", contact.Contact)
	}
}

func TestConversationFieldsMapToColliedesk(t *testing.T) {
	env := newImportEnv(t)
	env.mustRun(t)
	data := store.New(env.pool)
	ctx := context.Background()
	want := map[string]string{"1": "open/low/billing,vip", "2": "resolved/high/", "3": "closed/normal/legacy", "6": "pending/urgent/vendor", "7": "closed/low/"}
	for ticket, expected := range want {
		item, err := data.GetConversation(ctx, env.workspaceID, env.target(t, "ticket", ticket))
		if err != nil {
			t.Fatal(err)
		}
		var tags []string
		for _, tag := range item.Tags {
			tags = append(tags, tag.Name)
		}
		if got := item.Status + "/" + item.Priority + "/" + strings.Join(tags, ","); got != expected {
			t.Errorf("ticket %s = %s, want %s", ticket, got, expected)
		}
	}
	messages, err := data.ListMessages(ctx, env.workspaceID, env.target(t, "ticket", "1"))
	if err != nil {
		t.Fatal(err)
	}
	var alice string
	if err := env.pool.QueryRow(ctx, `SELECT id::text FROM agents WHERE workspace_id=$1 AND email='alice@acme.example'`, env.workspaceID).Scan(&alice); err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, message := range messages {
		author := ""
		if message.AuthorName != nil {
			author = *message.AuthorName
		}
		agent := message.AuthorAgentID != nil && *message.AuthorAgentID == alice
		got = append(got, message.Kind+":"+author+":"+map[bool]string{true: "alice", false: "-"}[agent])
	}
	if strings.Join(got, " ") != "incoming:Carol Customer:- incoming:Carol Customer:- outgoing:Alice Agent:alice note:Bob Former:-" {
		t.Fatalf("timeline = %v", got)
	}
	empty, err := data.ListMessages(ctx, env.workspaceID, env.target(t, "ticket", "7"))
	if err != nil || len(empty) != 1 || strings.TrimSpace(empty[0].Body) == "" {
		t.Fatalf("a message without text must keep a visible body: %+v %v", empty, err)
	}
}

func TestTimeEntriesAreImported(t *testing.T) {
	env := newImportEnv(t)
	env.mustRun(t)
	rows, err := env.pool.Query(context.Background(), `SELECT t.minutes,t.billable,t.note,t.agent_name,COALESCE(a.email,''),t.executed_at
		FROM time_entries t LEFT JOIN agents a ON a.id=t.agent_id WHERE t.workspace_id=$1 ORDER BY t.executed_at`, env.workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var minutes int
		var billable bool
		var note, name, email string
		var executed time.Time
		if err := rows.Scan(&minutes, &billable, &note, &name, &email, &executed); err != nil {
			t.Fatal(err)
		}
		got = append(got, strings.Join([]string{itoa(minutes), map[bool]string{true: "billable", false: "free"}[billable], note, name, email, executed.UTC().Format(time.RFC3339)}, "|"))
	}
	want := []string{
		"90|billable|Call with Carol|Alice Agent|alice@acme.example|2025-01-16T02:00:00Z",
		"15|free||Bob Former||2025-01-16T07:00:00Z",
		"120|billable|Vendor follow-up|Alice Agent|alice@acme.example|2025-04-01T06:00:00Z",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("time entries:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestImportedAttachmentChecksumAndWorkspaceScopedDownload(t *testing.T) {
	env := newImportEnv(t)
	env.mustRun(t)
	attachmentID := env.target(t, "attachment", "9001")
	var sha string
	var size int64
	if err := env.pool.QueryRow(context.Background(), `SELECT sha256,size FROM attachments WHERE workspace_id=$1 AND id=$2`, env.workspaceID, attachmentID).Scan(&sha, &size); err != nil {
		t.Fatal(err)
	}
	// sha256sum of the fixture bytes "PDF-BYTES-1".
	if sha != "1f7598a9b8ee2bab9eb6f30e83145344b9a9933ee99a024e76470c692a04d596" || size != 11 {
		t.Fatalf("recorded sha=%s size=%d", sha, size)
	}

	api := httpapi.New(env.pool, httpapi.Config{Origin: testOrigin, Attachments: env.storage})
	tokenA := redeem(t, api, env.pairingURL)
	_, linkB, err := env.auth.CreateWorkspace(context.Background(), "Other Team", "admin@other.example")
	if err != nil {
		t.Fatal(err)
	}
	tokenB := redeem(t, api, linkB)
	if body, status := get(api, "/api/v1/attachments/"+attachmentID, tokenA); status != http.StatusOK || body != "PDF-BYTES-1" {
		t.Fatalf("owner download status=%d body=%q", status, body)
	}
	if body, status := get(api, "/api/v1/attachments/"+attachmentID, tokenB); status != http.StatusNotFound || strings.Contains(body, "PDF-BYTES") {
		t.Fatalf("other workspace download status=%d body=%q", status, body)
	}
}

func assertUTC(t *testing.T, name string, got time.Time, want string) {
	t.Helper()
	if got.Location() != time.UTC || got.Format(time.RFC3339) != want {
		t.Errorf("%s = %s (%s), want %s", name, got.Format(time.RFC3339), got.Location(), want)
	}
}

func redeem(t *testing.T, api http.Handler, link string) string {
	t.Helper()
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.NewReader(`{"token":"` + parsed.Query().Get("token") + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/redeem", body)
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, req)
	var session struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil || session.Token == "" {
		t.Fatalf("redeem: %s", response.Body.String())
	}
	return session.Token
}

func get(api http.Handler, path, token string) (string, int) {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	api.ServeHTTP(response, req)
	return response.Body.String(), response.Code
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

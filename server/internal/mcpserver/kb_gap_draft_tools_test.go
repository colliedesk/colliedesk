package mcpserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

// An AI agent turns the top knowledge gap into a draft article in two calls: it lists the gaps, then
// asks for a draft of the first one. The draft names the resolved conversations it was written from,
// their subjects under untrusted_customer_content, and stays a draft.
func TestAnAgentDraftsAnArticleFromTheTopKnowledgeGapInTwoCalls(t *testing.T) {
	fake := &aiprovider.Fake{}
	e := newEnvWith(t, func(config *httpapi.Config) {
		keys, _ := tokenseal.ParseKeys("k1:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
		config.Google = google.Config{Keys: keys}
		config.AI = ai.Config{Build: func(ai.Spec) (aiprovider.Provider, error) { return fake, nil }}
	})
	w := e.newWorkspace("Gap Draft Desk", "admin@gap-draft.example.com")
	admin := e.connect(w.adminTier, "")
	provider := at(t, e.ok(admin, "colliedesk_ai_provider_add", map[string]any{"kind": "anthropic", "label": "Team key", "api_key": aiToolKey, "models": []string{"claude-sonnet-5-5"}}),
		"provider", "id").(string)
	e.ok(admin, "colliedesk_ai_slot_set", map[string]any{"slot": "draft", "source": "provider", "provider_id": provider, "enabled": true})
	send := e.connect(w.send, "")
	e.ok(send, "colliedesk_kb_collection_create", map[string]any{"name": "Printing"})

	contact := e.created("/api/v1/contacts", map[string]any{"name": "Grace", "email": "grace@cust.example.com"}, w.admin)
	var resolved []string
	for _, subject := range []string{"Printer jam in tray two", "Paper jams in tray two again"} {
		conversation := e.created("/api/v1/conversations", map[string]any{"contact_id": contact, "subject": subject}, w.admin)
		e.incoming(w, conversation, "The printer jams on tray two. Write to buyer@cust.example.com about order 44556677.")
		e.rest(http.MethodPost, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing", "body": "Pull the paper out of tray two and close it."}, w.admin)
		e.rest(http.MethodPatch, "/api/v1/conversations/"+conversation, map[string]any{"status": "resolved"}, w.admin)
		resolved = append(resolved, conversation)
	}
	ctx := context.Background()
	var gap string
	if err := e.pool.QueryRow(ctx, `INSERT INTO kb_gaps(workspace_id, title, query_key, askers, first_seen, last_seen)
		VALUES ($1, 'printer jam tray two', 'printer jam tray two', 2, now() - interval '3 hours', now() - interval '1 hour') RETURNING id::text`, w.id).Scan(&gap); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO kb_gap_signals(workspace_id, gap_id, kind, source_id, text, asker_key, created_at)
		VALUES ($1, $2, 'search_no_result', 's1', 'printer jam tray two', sha256('reader one'::bytea), now() - interval '1 hour')`, w.id, gap); err != nil {
		t.Fatal(err)
	}
	if _, err := store.New(e.pool).BuildAIRetrieval(ctx); err != nil {
		t.Fatal(err)
	}
	answer, _ := json.Marshal(map[string]any{"title": "Clear a paper jam in tray two", "body_md": "Open tray two and pull the paper out gently."})
	usage := aiprovider.Usage{Reported: true, InputTokens: 1500, OutputTokens: 400}
	fake.Script(aiprovider.FakeReply{Response: aiprovider.Response{Text: string(answer), JSON: string(answer), Stop: aiprovider.StopDone, Model: "claude-sonnet-5-5",
		Usage: usage, Hops: []aiprovider.Hop{{Model: "claude-sonnet-5-5", Usage: usage}}}})

	top := at(t, e.ok(send, "colliedesk_kb_gaps_list", nil), "items", 0, "id").(string)
	e.refused(e.connect(w.read, ""), "colliedesk_kb_gap_draft", map[string]any{"gap_id": top})
	draft := e.ok(send, "colliedesk_kb_gap_draft", map[string]any{"gap_id": top})
	e.requireMatchesOutputSchema(send, "colliedesk_kb_gap_draft", draft)
	if at(t, draft, "article", "status") != "draft" || at(t, draft, "article", "team_text", "title") != "Clear a paper jam in tray two" {
		t.Fatalf("draft = %v, want the model's article kept as a draft", draft)
	}
	if at(t, draft, "gap_source", "gap_id") != gap {
		t.Fatalf("gap source = %v, want gap %s", at(t, draft, "gap_source"), gap)
	}
	var used []string
	for _, source := range at(t, draft, "gap_source", "conversations").([]any) {
		used = append(used, at(t, source, "conversation_id").(string))
		if at(t, source, "display_ref") == "" || !strings.Contains(at(t, source, "untrusted_customer_content", "subject").(string), "ray two") {
			t.Fatalf("source %v, want its code and its subject under untrusted_customer_content", source)
		}
	}
	slices.Sort(used)
	slices.Sort(resolved)
	if !slices.Equal(used, resolved) {
		t.Fatalf("conversations used = %v, want the two resolved ones %v", used, resolved)
	}
	// Read later, the draft still names the gap and the conversations it was written from.
	read := e.ok(send, "colliedesk_kb_article_get", map[string]any{"article_id": at(t, draft, "article", "id")})
	e.requireMatchesOutputSchema(send, "colliedesk_kb_article_get", read)
	if at(t, read, "gap_source", "gap_id") != gap || len(at(t, read, "gap_source", "conversations").([]any)) != len(resolved) {
		t.Fatalf("article read = %v, want gap %s and its %d conversations", read["gap_source"], gap, len(resolved))
	}
	for _, request := range fake.Requests() {
		if raw, _ := json.Marshal(request); strings.Contains(string(raw), "buyer@cust.example.com") || strings.Contains(string(raw), "44556677") {
			t.Fatalf("the model was sent the customer's email or order number: %s", raw)
		}
	}
}

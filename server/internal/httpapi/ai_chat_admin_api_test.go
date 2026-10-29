package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type chatSlotView struct {
	Slot       string `json:"slot"`
	Enabled    bool   `json:"enabled"`
	MonthlyCap *int64 `json:"monthly_cap_micro_usd"`
}

// AI chat is a feature like drafts and triage, with a monthly spending limit of its own on a provider
// the workspace pays for: set, kept when left out, removed with null; no other feature takes one.
func TestTheChatFeatureTakesAMonthlyLimitOfItsOwn(t *testing.T) {
	desk := newAIDesk(t, nil)
	put := func(body map[string]any, want int) chatSlotView {
		t.Helper()
		response := request(t, desk.api, http.MethodPut, "/api/v1/ai/slots/chat", body, desk.adminA, nil)
		if response.Code != want {
			t.Fatalf("status=%d body=%s, want %d", response.Code, response.Body.String(), want)
		}
		var slot chatSlotView
		if want == http.StatusOK {
			decode(t, response, &slot)
		}
		return slot
	}
	slot := put(map[string]any{"source": "provider", "provider_id": desk.providerA, "enabled": true, "monthly_cap_micro_usd": 5_000_000}, http.StatusOK)
	if slot.Slot != "chat" || !slot.Enabled || slot.MonthlyCap == nil || *slot.MonthlyCap != 5_000_000 {
		t.Fatalf("slot = %+v", slot)
	}
	if slot := put(map[string]any{"source": "provider", "provider_id": desk.providerA, "enabled": false}, http.StatusOK); slot.MonthlyCap == nil || *slot.MonthlyCap != 5_000_000 {
		t.Fatalf("a change without the limit dropped it: %+v", slot)
	}
	if slot := put(map[string]any{"source": "provider", "provider_id": desk.providerA, "enabled": true, "monthly_cap_micro_usd": nil}, http.StatusOK); slot.MonthlyCap != nil {
		t.Fatalf("null kept the limit: %+v", slot)
	}
	wantResponse(t, request(t, desk.api, http.MethodPut, "/api/v1/ai/slots/draft", map[string]any{"source": "provider", "provider_id": desk.providerA, "enabled": true,
		"monthly_cap_micro_usd": 1000}, desk.adminA, nil), http.StatusBadRequest, "")
	var settings struct {
		Slots []chatSlotView `json:"slots"`
	}
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/ai", nil, desk.adminA, nil), &settings)
	if len(settings.Slots) != 3 || settings.Slots[2].Slot != "chat" || !settings.Slots[2].Enabled {
		t.Fatalf("slots = %+v", settings.Slots)
	}
	// Another workspace's settings never show it.
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/ai", nil, desk.adminB, nil), &settings)
	if settings.Slots[2].Enabled {
		t.Fatal("workspace B sees A's chat on")
	}
}

// seedChat adds a help chat channel (once) and a chat of the workspace with one answered turn, rated
// as given ("" for none).
func seedChat(t *testing.T, pool *pgxpool.Pool, workspaceID, status, question, rating string) string {
	t.Helper()
	var chatID string
	err := pool.QueryRow(context.Background(), `WITH c AS (
				INSERT INTO channels(workspace_id,type,name) SELECT $1,'help_chat','Help chat'
				WHERE NOT EXISTS (SELECT 1 FROM channels WHERE workspace_id=$1 AND type='help_chat') RETURNING id
			), channel AS (SELECT id FROM c UNION ALL SELECT id FROM channels WHERE workspace_id=$1 AND type='help_chat' LIMIT 1),
			s AS (INSERT INTO ai_chat_sessions(workspace_id,channel_id,token_hash,surface,status,ended_at)
				SELECT $1,channel.id,sha256(gen_random_uuid()::text::bytea),'help_center',$2,CASE WHEN $2='ended' THEN now() END FROM channel RETURNING id),
			t AS (INSERT INTO ai_chat_turns(workspace_id,session_id,seq,question,answer,status,finished_at)
				SELECT $1,s.id,1,$3,'Use the reset link [#1].','answered',now() FROM s RETURNING id),
			f AS (INSERT INTO ai_chat_feedback(workspace_id,turn_id,rating) SELECT $1,t.id,$4 FROM t WHERE $4<>'')
		SELECT id::text FROM s`, workspaceID, status, question, rating).Scan(&chatID)
	if err != nil {
		t.Fatal(err)
	}
	return chatID
}

type chatPage struct {
	Items []struct {
		ID         string  `json:"id"`
		Status     string  `json:"status"`
		FirstQ     *string `json:"first_question"`
		NotHelpful bool    `json:"not_helpful"`
	} `json:"items"`
	Next *string `json:"next_cursor"`
}

// The team reads the chats of its own workspace only: list, filter, transcript and delete never reach
// another workspace's chat, every member reads them, and only an admin deletes one, for good.
func TestAIChatsStayInTheirWorkspace(t *testing.T) {
	desk := newAIDesk(t, nil)
	workspaceB := workspaceID(t, desk.api, desk.adminB)
	open := seedChat(t, desk.pool, desk.workspaceA, "open", "How do I reset my password?", "not_helpful")
	ended := seedChat(t, desk.pool, desk.workspaceA, "ended", "Where is my order?", "helpful")
	other := seedChat(t, desk.pool, workspaceB, "open", "B's secret question", "")

	var page chatPage
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/ai/chat/sessions", nil, desk.adminA, nil), &page)
	if len(page.Items) != 2 || page.Next != nil {
		t.Fatalf("A's chats = %+v", page)
	}
	for _, item := range page.Items {
		if item.ID == other {
			t.Fatal("A's list holds B's chat")
		}
	}
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/ai/chat/sessions?feedback=not_helpful", nil, desk.adminA, nil), &page)
	if len(page.Items) != 1 || page.Items[0].ID != open || !page.Items[0].NotHelpful {
		t.Fatalf("not helpful = %+v", page)
	}
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/ai/chat/sessions?status=ended", nil, desk.adminA, nil), &page)
	if len(page.Items) != 1 || page.Items[0].ID != ended {
		t.Fatalf("ended = %+v", page)
	}
	wantResponse(t, request(t, desk.api, http.MethodGet, "/api/v1/ai/chat/sessions?status=gone", nil, desk.adminA, nil), http.StatusBadRequest, "invalid_request")

	var chat struct {
		ID         string `json:"id"`
		Transcript []struct {
			Question string `json:"question"`
			Feedback string `json:"feedback"`
		} `json:"transcript"`
	}
	agent := joinAsAgent(t, desk.api, desk.adminA, "agent@ai-a.example.com")
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/ai/chat/sessions/"+open, nil, agent, nil), &chat)
	if chat.ID != open || len(chat.Transcript) != 1 || chat.Transcript[0].Question != "How do I reset my password?" || chat.Transcript[0].Feedback != "not_helpful" {
		t.Fatalf("transcript = %+v", chat)
	}
	for _, id := range []string{other, "not-an-id", "00000000-0000-4000-8000-000000000000"} {
		wantResponse(t, request(t, desk.api, http.MethodGet, "/api/v1/ai/chat/sessions/"+id, nil, desk.adminA, nil), http.StatusNotFound, "not_found")
		wantResponse(t, request(t, desk.api, http.MethodDelete, "/api/v1/ai/chat/sessions/"+id, nil, desk.adminA, nil), http.StatusNotFound, "not_found")
	}
	wantResponse(t, request(t, desk.api, http.MethodDelete, "/api/v1/ai/chat/sessions/"+open, nil, agent, nil), http.StatusForbidden, "forbidden")
	if response := request(t, desk.api, http.MethodDelete, "/api/v1/ai/chat/sessions/"+open, nil, desk.adminA, nil); response.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d %s", response.Code, response.Body.String())
	}
	wantResponse(t, request(t, desk.api, http.MethodGet, "/api/v1/ai/chat/sessions/"+open, nil, desk.adminA, nil), http.StatusNotFound, "not_found")
	var left int
	if err := desk.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM ai_chat_turns WHERE session_id=$1)+(SELECT count(*) FROM ai_chat_sessions WHERE id=$2)`,
		open, other).Scan(&left); err != nil || left != 1 {
		t.Fatalf("after delete: %d rows (want only B's chat) %v", left, err)
	}
	var events int
	if err := desk.pool.QueryRow(context.Background(), `SELECT count(*) FROM ai_settings_events WHERE workspace_id=$1 AND kind='chat_deleted'`, desk.workspaceA).Scan(&events); err != nil || events != 1 {
		t.Fatalf("chat_deleted events = %d %v", events, err)
	}

	// The month's use counts A's answers and feedback, never B's.
	var usage struct {
		Answers       int    `json:"answers"`
		Chats         int    `json:"chats"`
		Rated         int    `json:"rated"`
		SpentMicroUSD *int64 `json:"spent_micro_usd"`
		AnswerLimit   *int   `json:"answer_limit"`
	}
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/ai/chat/usage", nil, desk.adminA, nil), &usage)
	if usage.Answers != 1 || usage.Chats != 1 || usage.Rated != 1 || usage.AnswerLimit != nil || usage.SpentMicroUSD == nil {
		t.Fatalf("usage = %+v", usage)
	}
	wantResponse(t, request(t, desk.api, http.MethodGet, "/api/v1/ai/chat/usage", nil, agent, nil), http.StatusForbidden, "forbidden")
}

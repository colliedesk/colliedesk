package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// contractAI holds AI drafts for the contract cases, each on a conversation of its own so that asking
// for a new draft on one never retires another's.
type contractAI struct {
	readConversation, discardConversation, discardDraft string
	triageConversation, triageDraft                     string
	cancelConversation, cancelDraft                     string
}

func seedContractAI(t *testing.T, api http.Handler, pool *pgxpool.Pool, admin, contactID string) contractAI {
	t.Helper()
	workspace := workspaceID(t, api, admin)
	conversation := func(subject string) string {
		return create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contactID, "subject": subject})
	}
	draft := func(conversationID, status string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(context.Background(), `INSERT INTO ai_drafts(workspace_id,conversation_id,status,action,body,confidence,confidence_reasons,sources,triage,model,ready_at,auto_send_at)
			VALUES($1,$2,$3,'answer','Contract draft.','high','{}','[{"kind":"kb_article","id":"00000000-0000-4000-8000-000000000001","number":1,"title":"Contract"}]',
				'{"tags":["contract"],"priority":"high","reason":"Contract"}','contract-model',now(),CASE WHEN $3='scheduled' THEN now()+interval '1 hour' END)
			RETURNING id::text`, workspace, conversationID, status).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	ai := contractAI{readConversation: conversation("AI read"), discardConversation: conversation("AI discard"),
		triageConversation: conversation("AI triage"), cancelConversation: conversation("AI cancel")}
	draft(ai.readConversation, "ready")
	ai.discardDraft = draft(ai.discardConversation, "ready")
	ai.triageDraft = draft(ai.triageConversation, "ready")
	ai.cancelDraft = draft(ai.cancelConversation, "scheduled")
	return ai
}

func contractAICases(f contractFixture) map[string]contractCase {
	admin, ai := f.adminToken, f.ai
	return map[string]contractCase{
		"GET /api/v1/conversations/{id}/ai-draft":                               {"/api/v1/conversations/" + ai.readConversation + "/ai-draft", nil, admin, 200, ""},
		"POST /api/v1/conversations/{id}/ai-draft":                              {"/api/v1/conversations/" + f.conversationID + "/ai-draft", nil, admin, 202, ""},
		"POST /api/v1/conversations/{id}/ai-draft/{draftId}/discard":            {"/api/v1/conversations/" + ai.discardConversation + "/ai-draft/" + ai.discardDraft + "/discard", nil, admin, 200, ""},
		"POST /api/v1/conversations/{id}/ai-draft/{draftId}/apply-triage":       {"/api/v1/conversations/" + ai.triageConversation + "/ai-draft/" + ai.triageDraft + "/apply-triage", nil, admin, 200, ""},
		"POST /api/v1/conversations/{id}/ai-draft/{draftId}/cancel-auto-answer": {"/api/v1/conversations/" + ai.cancelConversation + "/ai-draft/" + ai.cancelDraft + "/cancel-auto-answer", nil, admin, 200, ""},
		"GET /api/v1/workspace/ai":                                              {"/api/v1/workspace/ai", nil, admin, 200, ""},
		"PATCH /api/v1/workspace/ai":                                            {"/api/v1/workspace/ai", map[string]any{"drafts_enabled": true, "auto_answer_delay_seconds": 300}, admin, 200, ""},
	}
}

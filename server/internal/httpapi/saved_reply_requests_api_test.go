package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type replyRequestView struct {
	agentRequestView
	SavedReply *struct {
		ID       string `json:"id"`
		Restores *struct {
			Version int    `json:"version"`
			Title   string `json:"title"`
			Body    string `json:"body"`
		} `json:"restores"`
		Before *struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		} `json:"before"`
		Current *struct {
			Title     string  `json:"title"`
			Body      string  `json:"body"`
			DeletedAt *string `json:"deleted_at"`
		} `json:"current"`
	} `json:"saved_reply"`
}

func getReplyRequest(t *testing.T, api http.Handler, token, id string) replyRequestView {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/agent-requests/"+id, nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET request %s status=%d body=%s", id, response.Code, response.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodGet, "/api/v1/agent-requests/"+id, nil, token, nil), response)
	var view replyRequestView
	decode(t, response, &view)
	return view
}

func TestDraftTokenProposesAnEditThatShowsBeforeAndAfterAndAppliesOnApproval(t *testing.T) {
	desk := newRequestDesk(t, "Proposal Desk")
	api, admin := desk.api, desk.admin
	drafter := issueToken(t, api, admin, "Reply improver", "draft").Token
	id := create(t, api, admin, "/api/v1/saved-replies", map[string]any{"title": "Refund", "body": "We refunded you."})

	proposal := map[string]any{"title": "Refund done", "body": "We refunded you today.", "updated_at": seenAt(t, api, admin, id)}
	filed := accepted(t, api, http.MethodPatch, "/api/v1/saved-replies/"+id, proposal, drafter)
	if !strings.Contains(filed.Summary, `"Refund"`) || !strings.Contains(filed.Summary, "Refund done") {
		t.Fatalf("summary = %q, want the reply it edits and the new title", filed.Summary)
	}
	if listed := listSavedReplies(t, api, admin); listed[0].Body != "We refunded you." {
		t.Fatalf("a proposal changed the reply before approval: %+v", listed)
	}

	// Someone edits the reply after the token asked: the request keeps the text the token saw.
	patchReply(t, api, admin, id, map[string]any{"body": "We refunded you (edited by a person)."})
	view := getReplyRequest(t, api, admin, filed.RequestID)
	if view.SavedReply == nil || view.SavedReply.ID != id || view.SavedReply.Before == nil || view.SavedReply.Current == nil {
		t.Fatalf("saved_reply = %+v, want before and current", view.SavedReply)
	}
	if view.SavedReply.Before.Title != "Refund" || view.SavedReply.Before.Body != "We refunded you." {
		t.Fatalf("before = %+v, want the reply as it was when the token asked", view.SavedReply.Before)
	}
	if view.SavedReply.Current.Body != "We refunded you (edited by a person)." {
		t.Fatalf("current = %+v, want the reply as it is now", view.SavedReply.Current)
	}
	var proposed map[string]any
	if err := json.Unmarshal(view.Body, &proposed); err != nil || proposed["body"] != "We refunded you today." {
		t.Fatalf("proposed body = %s", view.Body)
	}

	// The reply changed since the token read it, so approving refuses instead of overwriting.
	var stale approvedView
	decode(t, approve(t, api, admin, filed.RequestID, nil), &stale)
	if stale.Request.Status != "failed" || stale.Request.ResultStatus == nil || *stale.Request.ResultStatus != http.StatusConflict {
		t.Fatalf("approving a stale edit = %+v, want failed with 409", stale.Request)
	}

	fresh := accepted(t, api, http.MethodPatch, "/api/v1/saved-replies/"+id,
		map[string]any{"body": "We refunded you today.", "updated_at": seenAt(t, api, admin, id)}, drafter)
	var done approvedView
	decode(t, approve(t, api, admin, fresh.RequestID, nil), &done)
	if done.Request.Status != "approved" {
		t.Fatalf("approve = %+v", done.Request)
	}
	versions := replyVersions(t, api, admin, id)
	latest := versions[0]
	if latest.Kind != "edit" || latest.Body != "We refunded you today." || latest.AgentRequestID == nil || *latest.AgentRequestID != fresh.RequestID {
		t.Fatalf("latest version = %+v, want the approved edit naming its request", latest)
	}
	if latest.Token == nil || latest.Token.Label != "Reply improver" || latest.ChangedBy == nil || latest.ChangedBy.Name == "" {
		t.Fatalf("latest version attribution = %+v, want the approver via the Reply improver token", latest)
	}
}

func TestLowerTiersProposeCreateDeleteAndRestoreWithReadableSummaries(t *testing.T) {
	desk := newRequestDesk(t, "Lower Tier Desk")
	api, admin := desk.api, desk.admin
	reader := issueToken(t, api, admin, "Reader", "read").Token
	drafter := issueToken(t, api, admin, "Drafter", "draft").Token
	id := create(t, api, admin, "/api/v1/saved-replies", map[string]any{"title": "Closing", "body": "Thanks for writing."})

	createRequest := accepted(t, api, http.MethodPost, "/api/v1/saved-replies", map[string]any{"title": "Greeting", "body": "Hello there."}, reader)
	if !strings.Contains(createRequest.Summary, "Greeting") || !strings.Contains(createRequest.Summary, "Hello there.") {
		t.Fatalf("create summary = %q, want the new title and text", createRequest.Summary)
	}
	deleteRequest := accepted(t, api, http.MethodDelete, deletePath(t, api, admin, id), nil, drafter)
	if !strings.Contains(deleteRequest.Summary, `"Closing"`) || !strings.Contains(deleteRequest.Summary, "trash") {
		t.Fatalf("delete summary = %q, want the reply it deletes and that it goes to the trash", deleteRequest.Summary)
	}
	if view := getReplyRequest(t, api, admin, deleteRequest.RequestID); view.SavedReply == nil || view.SavedReply.Before == nil || view.SavedReply.Before.Body != "Thanks for writing." {
		t.Fatalf("delete request saved_reply = %+v, want what would be deleted", view.SavedReply)
	}
	if listed := listSavedReplies(t, api, admin); len(listed) != 1 {
		t.Fatalf("proposals changed the list before approval: %+v", listed)
	}

	var created, deleted approvedView
	decode(t, approve(t, api, admin, createRequest.RequestID, nil), &created)
	decode(t, approve(t, api, admin, deleteRequest.RequestID, nil), &deleted)
	if created.Request.Status != "approved" || deleted.Request.Status != "approved" {
		t.Fatalf("approvals = %s, %s", created.Request.Status, deleted.Request.Status)
	}
	trash := replyTrash(t, api, admin)
	if len(trash) != 1 || trash[0].ID != id || trash[0].LastChange == nil || trash[0].LastChange.AgentRequestID == nil || *trash[0].LastChange.AgentRequestID != deleteRequest.RequestID {
		t.Fatalf("trash = %+v, want the approved delete naming its request", trash)
	}

	undelete := accepted(t, api, http.MethodPost, "/api/v1/saved-replies/"+id+"/restore", undeleteBody(t, api, admin, id), drafter)
	if !strings.Contains(undelete.Summary, `"Closing"`) || !strings.Contains(undelete.Summary, "trash") {
		t.Fatalf("undelete summary = %q", undelete.Summary)
	}
	restoreVersion := accepted(t, api, http.MethodPost, "/api/v1/saved-replies/"+create(t, api, admin, "/api/v1/saved-replies", map[string]any{"title": "Hours", "body": "9 to 5"})+"/versions/1/restore",
		map[string]any{"updated_at": "2026-10-17T09:00:00Z"}, reader)
	if !strings.Contains(restoreVersion.Summary, "version 1") || !strings.Contains(restoreVersion.Summary, `"Hours"`) {
		t.Fatalf("restore version summary = %q", restoreVersion.Summary)
	}

	missing := request(t, api, http.MethodPatch, "/api/v1/saved-replies/00000000-0000-4000-8000-000000000000", map[string]any{"body": "x", "updated_at": "2026-10-17T09:00:00Z"}, drafter, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("a proposal for a reply that does not exist status=%d body=%s, want 404 now, not a request", missing.Code, missing.Body.String())
	}
}

func TestStaleDeleteProposalFailsAndDeletesNothing(t *testing.T) {
	desk := newRequestDesk(t, "Stale Delete Desk")
	api, admin := desk.api, desk.admin
	drafter := issueToken(t, api, admin, "Cleaner", "draft").Token
	id := create(t, api, admin, "/api/v1/saved-replies", map[string]any{"title": "Shipping", "body": "Ships in 2 days."})

	if missing := request(t, api, http.MethodDelete, "/api/v1/saved-replies/"+id, nil, admin, nil); missing.Code != http.StatusBadRequest {
		t.Fatalf("delete without updated_at status=%d body=%s, want 400", missing.Code, missing.Body.String())
	}
	if unfiled := request(t, api, http.MethodDelete, "/api/v1/saved-replies/"+id, nil, drafter, nil); unfiled.Code != http.StatusBadRequest {
		t.Fatalf("a delete proposal without updated_at status=%d body=%s, want 400 before anything is filed", unfiled.Code, unfiled.Body.String())
	}
	filed := accepted(t, api, http.MethodDelete, deletePath(t, api, admin, id), nil, drafter)
	stalePath := deletePath(t, api, admin, id)
	patchReply(t, api, admin, id, map[string]any{"body": "Ships in 3 days, after a person's edit."})

	var outcome approvedView
	decode(t, approve(t, api, admin, filed.RequestID, nil), &outcome)
	if outcome.Request.Status != "failed" || outcome.Request.ResultStatus == nil || *outcome.Request.ResultStatus != http.StatusConflict {
		t.Fatalf("approving a delete proposed before an edit = %+v, want failed with 409", outcome.Request)
	}
	if listed := listSavedReplies(t, api, admin); len(listed) != 1 || listed[0].Body != "Ships in 3 days, after a person's edit." {
		t.Fatalf("a stale delete changed the replies: %+v", listed)
	}
	if direct := request(t, api, http.MethodDelete, stalePath, nil, admin, nil); direct.Code != http.StatusConflict || !strings.Contains(direct.Body.String(), "changed_elsewhere") {
		t.Fatalf("a person's delete with a stale updated_at status=%d body=%s, want 409 changed_elsewhere", direct.Code, direct.Body.String())
	}

	// Undelete is guarded the same way: a reply deleted again after the proposal is not restored blind.
	if response := request(t, api, http.MethodDelete, deletePath(t, api, admin, id), nil, admin, nil); response.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d", response.Code)
	}
	if stale := request(t, api, http.MethodPost, "/api/v1/saved-replies/"+id+"/restore", map[string]any{"updated_at": anyTime}, admin, nil); stale.Code != http.StatusConflict {
		t.Fatalf("undelete with a stale updated_at status=%d body=%s, want 409", stale.Code, stale.Body.String())
	}
}

func TestVersionRestoreProposalShowsTheVersionItApplies(t *testing.T) {
	desk := newRequestDesk(t, "Version Proposal Desk")
	api, admin := desk.api, desk.admin
	reader := issueToken(t, api, admin, "Historian", "read").Token
	id := create(t, api, admin, "/api/v1/saved-replies", map[string]any{"title": "Hours", "body": "9 to 5"})
	patchReply(t, api, admin, id, map[string]any{"title": "Opening hours", "body": "8 to 6"})

	filed := accepted(t, api, http.MethodPost, "/api/v1/saved-replies/"+id+"/versions/1/restore", map[string]any{"updated_at": seenAt(t, api, admin, id)}, reader)
	view := getReplyRequest(t, api, admin, filed.RequestID)
	if view.SavedReply == nil || view.SavedReply.Restores == nil || view.SavedReply.Restores.Version != 1 ||
		view.SavedReply.Restores.Title != "Hours" || view.SavedReply.Restores.Body != "9 to 5" || view.SavedReply.Before.Body != "8 to 6" {
		t.Fatalf("saved_reply = %+v, want before 8 to 6 and the restored version 1", view.SavedReply)
	}
	if missing := request(t, api, http.MethodPost, "/api/v1/saved-replies/"+id+"/versions/7/restore", map[string]any{"updated_at": seenAt(t, api, admin, id)}, reader, nil); missing.Code != http.StatusNotFound {
		t.Fatalf("a proposal to restore a version that does not exist status=%d body=%s, want 404", missing.Code, missing.Body.String())
	}
}

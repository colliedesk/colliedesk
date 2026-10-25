package httpapi_test

import (
	"context"
	"html"
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/mailimage"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5/pgxpool"
)

type contractFixture struct {
	api                       http.Handler
	adminToken, agentToken    string
	agentID                   string
	contactID, conversationID string
	// mergeDuplicateID is merged by the contract case; undoMergeID is a merge the undo case reverts.
	mergeDuplicateID, undoMergeID string
	inviteID, sessionID           string
	updateTagID, deleteTagID      string
	updateReplyID, deleteReplyID  string
	updateReplySeen               string
	trashReplyID, restoreReplyID  string
	restoreReplySeen              string
	deleteReplySeen               string
	trashReplySeen                string
	updateViewID, deleteViewID    string
	updateViewSeen                string
	trashViewID                   string
	attachmentID                  string
	mailImagePath                 string
	redeemToken, logoutToken      string
	inboundSecret                 string
	failedReplyID                 string
	deleteChannelToken            string
	portalToken, webhookBody      string
	// requestToken filed three pending agent requests, one for each decision.
	requestToken                                       string
	approveRequestID, rejectRequestID, cancelRequestID string
	kb                                                 contractKB
	imports                                            importJobs
	// aiProviderID is changed, checked and finally deleted by the AI cases.
	aiProviderID string
	// aiDraftID is a draft of conversationID, still queued: read, then discarded by the cases.
	aiDraftID string
	// aiTriageID is a ready suggestion of conversationID (high priority), accepted by the cases;
	// aiTriageOtherID a ready one of another conversation, dismissed.
	aiTriageID, aiTriageOtherID, aiTriageOtherConversation, aiTriageNewConversation, aiDraftsProviderID string
	// aiTriageAutoID is a suggestion AI applied by itself on aiTriageAutoConversation, undone by its case.
	aiTriageAutoID, aiTriageAutoConversation string
	// webFormChannelID is changed and checked by the channel cases; emailChannelID is removed by one.
	webFormChannelID, emailChannelID string
	// secondEmailChannelID is a second email channel of the admin's workspace.
	secondEmailChannelID string
}

func newContractFixture(t *testing.T) contractFixture {
	t.Helper()
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fakePolar := newFakePolarAPI(t)
	imageHost := newImageHost(t, serveBytes("image/png", tinyPNG))
	api, service, pool, dns := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Attachments = storage
		config.MailImages = mailimage.NewFetcher(allowing(imageHost))
		config.Billing = fakePolar.config()
		hostedConfig(config)
		config.Email.ResendWebhookSecret = resendWebhookSecret
		config.ProblemReportWorkspace = "contract-support"
		keys, _ := tokenseal.ParseKeys("k1:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
		config.Google = google.Config{ClientID: "contract-client", ClientSecret: "not-a-secret", Keys: keys, Base: "http://127.0.0.1:1"}
		config.AI = fakeAIConfig(&aiprovider.Fake{})
	})
	createSession(t, api, service, "Contract Support", "support@contract-support.example.com")
	adminToken := createSession(t, api, service, "Contract All", "admin@contract-all.example.com")
	contactID := create(t, api, adminToken, "/api/v1/contacts", map[string]any{
		"name": "Contract Contact", "email": "contact@contract-all.example.com",
	})
	conversationID := create(t, api, adminToken, "/api/v1/conversations", map[string]any{
		"contact_id": contactID, "subject": "Contract conversation",
	})
	mergeDuplicateID := create(t, api, adminToken, "/api/v1/contacts", map[string]any{"name": "Merge duplicate", "email": "merge@contract-all.example.com"})
	undoDuplicateID := create(t, api, adminToken, "/api/v1/contacts", map[string]any{"name": "Undo duplicate", "email": "undo@contract-all.example.com"})
	undoMergeID := create(t, api, adminToken, "/api/v1/contacts/"+contactID+"/merges", map[string]any{"contact_id": undoDuplicateID})
	noteID := create(t, api, adminToken, "/api/v1/conversations/"+conversationID+"/messages", map[string]any{
		"kind": "note", "body": "Contract note",
	})
	attachmentID := seedAttachment(t, pool, storage, adminToken, api, noteID, "contract.txt", "contract bytes")
	seedImportDetails(t, pool, workspaceID(t, api, adminToken), conversationID, noteID)
	updateTagID := create(t, api, adminToken, "/api/v1/tags", map[string]any{"name": "Update contract tag"})
	deleteTagID := create(t, api, adminToken, "/api/v1/tags", map[string]any{"name": "Delete contract tag"})
	updateReplyID := create(t, api, adminToken, "/api/v1/saved-replies", map[string]any{"title": "Update contract reply", "body": "Hello {{customer.name}}"})
	updateReplySeen := seenAt(t, api, adminToken, updateReplyID)
	deleteReplyID := create(t, api, adminToken, "/api/v1/saved-replies", map[string]any{"title": "Delete contract reply", "body": "Bye"})
	deleteReplySeen := seenAt(t, api, adminToken, deleteReplyID)
	trashReplyID := create(t, api, adminToken, "/api/v1/saved-replies", map[string]any{"title": "Trashed contract reply", "body": "Back soon"})
	if response := request(t, api, http.MethodDelete, deletePath(t, api, adminToken, trashReplyID), nil, adminToken, nil); response.Code != http.StatusNoContent {
		t.Fatalf("trash contract reply status=%d body=%s", response.Code, response.Body.String())
	}
	trashReplySeen := undeleteBody(t, api, adminToken, trashReplyID)["updated_at"].(string)
	restoreReplyID := create(t, api, adminToken, "/api/v1/saved-replies", map[string]any{"title": "Restore contract reply", "body": "First"})
	if response := patchReply(t, api, adminToken, restoreReplyID, map[string]any{"body": "Second"}); response.Code != http.StatusOK {
		t.Fatalf("edit contract reply status=%d body=%s", response.Code, response.Body.String())
	}
	restoreReplySeen := seenAt(t, api, adminToken, restoreReplyID)
	kb := seedContractKB(t, api, adminToken)
	updateViewID := create(t, api, adminToken, "/api/v1/saved-views", map[string]any{"name": "Update contract view", "scope": "shared", "filters": map[string]any{"status": []string{"open"}}})
	updateViewSeen := viewSeen(t, api, adminToken, updateViewID)
	deleteViewID := create(t, api, adminToken, "/api/v1/saved-views", map[string]any{"name": "Delete contract view", "scope": "personal", "filters": map[string]any{}})
	trashViewID := create(t, api, adminToken, "/api/v1/saved-views", map[string]any{"name": "Trashed contract view", "scope": "shared", "filters": map[string]any{"has_pending_draft": true}})
	if response := request(t, api, http.MethodDelete, "/api/v1/saved-views/"+trashViewID, nil, adminToken, nil); response.Code != http.StatusOK {
		t.Fatalf("trash contract view status=%d body=%s", response.Code, response.Body.String())
	}

	inviteID, agentURL := createContractInvite(t, api, adminToken, "agent@contract-all.example.com", "agent")
	agentRedeem := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, agentURL)}, "", nil)
	var agentSession struct {
		Token string `json:"token"`
	}
	decode(t, agentRedeem, &agentSession)

	extraURL, err := service.Pair(context.Background(), "contract-all", "admin@contract-all.example.com")
	if err != nil {
		t.Fatal(err)
	}
	extraRedeem := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{
		"token": linkToken(t, extraURL), "label": "Contract revoke target",
	}, "", nil)
	var extraSession struct {
		Token string `json:"token"`
	}
	decode(t, extraRedeem, &extraSession)
	sessionID := findSessionID(t, api, adminToken, "Contract revoke target")

	redeemURL, err := service.Pair(context.Background(), "contract-all", "admin@contract-all.example.com")
	if err != nil {
		t.Fatal(err)
	}
	logoutURL, err := service.Pair(context.Background(), "contract-all", "admin@contract-all.example.com")
	if err != nil {
		t.Fatal(err)
	}
	logoutRedeem := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, logoutURL)}, "", nil)
	var logout struct {
		Token string `json:"token"`
	}
	decode(t, logoutRedeem, &logout)

	api, inboundSecret, failedReplyID := withFailedEmailReply(t, api, pool, adminToken, conversationID)
	mailImagePath := seedMailImage(t, api, pool, adminToken, conversationID, imageHost.URL+"/contract.png")
	record := domainState(t, api, adminToken).Channel
	dns.set(record.DNSRecordName, record.DNSRecordValue)
	deleteChannelToken := createSession(t, api, service, "Contract Disconnect", "admin@contract-disconnect.example.com")
	_ = connectEmail(t, api, deleteChannelToken, "support@contract-disconnect.example.com")

	// A workspace with a paid subscription, so the portal has a Polar customer to open.
	portalToken := createSession(t, api, service, "Contract Billing", "admin@contract-billing.example.com")
	subscription := activeTeam(workspaceID(t, api, portalToken), "sub_contract", time.Now())
	if response := serve(api, signedWebhookRequest("msg_contract_seed", subscription.event("subscription.active"))); response.Code != http.StatusOK {
		t.Fatalf("seed subscription webhook status=%d body=%s", response.Code, response.Body.String())
	}

	if response := request(t, api, http.MethodPut, "/api/v1/channels/web-form", map[string]any{"enabled": true, "allowed_origins": []string{}}, adminToken, nil); response.Code != http.StatusOK {
		t.Fatalf("enable web form status=%d body=%s", response.Code, response.Body.String())
	}
	channelIDs := map[string]string{}
	var channelList struct {
		Items []struct{ ID, Type string } `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/channels", nil, adminToken, nil), &channelList)
	for _, item := range channelList.Items {
		if channelIDs[item.Type] == "" {
			channelIDs[item.Type] = item.ID
		}
	}
	// A second mailbox on the same domain, for the routes of one email channel of several.
	var billing struct {
		ID string `json:"id"`
	}
	decode(t, request(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "email", "name": "Billing",
		"config": map[string]any{"support_address": "billing@contract-all.example.com"}}, adminToken, nil), &billing)
	if billing.ID == "" {
		t.Fatal("add a second email channel")
	}

	agentID := memberIDByEmail(t, api, adminToken, "agent@contract-all.example.com")
	requestToken := issueToken(t, api, adminToken, "Contract requester", "read").Token
	fileRequest := func() string {
		response := request(t, api, http.MethodPatch, "/api/v1/conversations/"+conversationID, map[string]any{"priority": "high"}, requestToken, nil)
		var accepted struct {
			RequestID string `json:"request_id"`
		}
		decode(t, response, &accepted)
		if accepted.RequestID == "" {
			t.Fatalf("file agent request status=%d body=%s", response.Code, response.Body.String())
		}
		return accepted.RequestID
	}
	approveRequestID, rejectRequestID, cancelRequestID := fileRequest(), fileRequest(), fileRequest()
	// A teammate writing and an agent token have the conversation open, so the presence answers and the
	// activity poll carry viewers of both kinds.
	for _, viewer := range []string{agentSession.Token, requestToken} {
		body := map[string]any{"client_id": "contract-viewer", "writing": viewer == agentSession.Token}
		if response := request(t, api, http.MethodPut, "/api/v1/conversations/"+conversationID+"/presence", body, viewer, nil); response.Code != http.StatusOK {
			t.Fatalf("announce presence status=%d body=%s", response.Code, response.Body.String())
		}
	}
	imports := seedImportJobs(t, pool, workspaceID(t, api, adminToken))
	aiProviderID := create(t, api, adminToken, "/api/v1/ai/providers", map[string]any{"kind": "anthropic", "label": "Contract key", "api_key": "sk-contract-0001", "models": []string{"claude-sonnet-5-5", "claude-haiku-5-5"}})
	// Reply drafts run on a provider of their own, which the provider cases leave alone.
	draftsProvider := create(t, api, adminToken, "/api/v1/ai/providers", map[string]any{"kind": "anthropic", "label": "Contract drafts", "api_key": "sk-contract-0004", "models": []string{"claude-sonnet-5-5"}})
	if response := request(t, api, http.MethodPut, "/api/v1/ai/slots/draft", map[string]any{"source": "provider", "provider_id": draftsProvider, "enabled": true}, adminToken, nil); response.Code != http.StatusOK {
		t.Fatalf("turn reply drafts on: %d %s", response.Code, response.Body.String())
	}
	queued := request(t, api, http.MethodPost, "/api/v1/conversations/"+conversationID+"/ai-drafts", map[string]any{"prompt": "Contract draft"}, adminToken, nil)
	var draft struct {
		ID string `json:"id"`
	}
	decode(t, queued, &draft)
	if queued.Code != http.StatusAccepted || draft.ID == "" {
		t.Fatalf("ask for an AI draft: %d %s", queued.Code, queued.Body.String())
	}
	aiDraftID := draft.ID
	if response := request(t, api, http.MethodPut, "/api/v1/ai/slots/triage", map[string]any{"source": "provider", "provider_id": draftsProvider, "enabled": true}, adminToken, nil); response.Code != http.StatusOK {
		t.Fatalf("turn triage on: %d %s", response.Code, response.Body.String())
	}
	readyTriage := func(conversation string) string {
		var id string
		if err := pool.QueryRow(context.Background(), `INSERT INTO ai_triage_suggestions(workspace_id,conversation_id,status,priority,reason,model,ready_at)
			SELECT workspace_id,id,'ready','high','Printer down','claude-haiku-5-5',now() FROM conversations WHERE id=$1 RETURNING id::text`, conversation).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	aiTriageID := readyTriage(conversationID)
	otherTriageConversation := create(t, api, adminToken, "/api/v1/conversations", map[string]any{"contact_id": contactID, "subject": "Triage contract"})
	aiTriageOtherID := readyTriage(otherTriageConversation)
	// One AI applied by itself (the priority), on a conversation of its own: the undo case takes it back.
	autoTriageConversation := create(t, api, adminToken, "/api/v1/conversations", map[string]any{"contact_id": contactID, "subject": "AI applied"})
	var aiTriageAutoID string
	if err := pool.QueryRow(context.Background(), `WITH c AS (UPDATE conversations SET priority='urgent' WHERE id=$1 RETURNING workspace_id,id)
		INSERT INTO ai_triage_suggestions(workspace_id,conversation_id,status,automatic,priority,reason,base,ready_at,applied,auto_applied,auto_applied_at,outcome,decided_at)
		SELECT workspace_id,id,'ready',true,'urgent','Printer down','{"priority":"normal","assignee_agent_id":null}',now(),
			'{"tag_ids":[],"priority":"urgent","assignee_agent_id":null}','{"tag_ids":[],"priority":"urgent","assignee_agent_id":null}',now(),'auto_applied',now() FROM c
		RETURNING id::text`, autoTriageConversation).Scan(&aiTriageAutoID); err != nil {
		t.Fatal(err)
	}
	newTriageConversation := create(t, api, adminToken, "/api/v1/conversations", map[string]any{"contact_id": contactID, "subject": "Triage asked for"})
	return contractFixture{webFormChannelID: channelIDs["web_form"], emailChannelID: channelIDs["email"], secondEmailChannelID: billing.ID, aiTriageAutoID: aiTriageAutoID, aiTriageAutoConversation: autoTriageConversation, aiDraftID: aiDraftID, aiTriageID: aiTriageID, aiTriageOtherID: aiTriageOtherID, aiTriageOtherConversation: otherTriageConversation, aiTriageNewConversation: newTriageConversation, aiDraftsProviderID: draftsProvider, aiProviderID: aiProviderID, imports: imports, requestToken: requestToken, approveRequestID: approveRequestID, rejectRequestID: rejectRequestID, cancelRequestID: cancelRequestID, agentID: agentID, api: api, portalToken: portalToken, webhookBody: string(subscription.event("subscription.updated")), inboundSecret: inboundSecret, failedReplyID: failedReplyID, deleteChannelToken: deleteChannelToken, adminToken: adminToken, agentToken: agentSession.Token,
		contactID: contactID, conversationID: conversationID, mergeDuplicateID: mergeDuplicateID, undoMergeID: undoMergeID, inviteID: inviteID, sessionID: sessionID,
		updateTagID: updateTagID, deleteTagID: deleteTagID, updateReplyID: updateReplyID, updateReplySeen: updateReplySeen, deleteReplyID: deleteReplyID, trashReplyID: trashReplyID, restoreReplyID: restoreReplyID, restoreReplySeen: restoreReplySeen, deleteReplySeen: deleteReplySeen, trashReplySeen: trashReplySeen, updateViewID: updateViewID, updateViewSeen: updateViewSeen, deleteViewID: deleteViewID, trashViewID: trashViewID, attachmentID: attachmentID, mailImagePath: mailImagePath, redeemToken: linkToken(t, redeemURL), logoutToken: logout.Token, kb: kb}
}

// seedMailImage gives the conversation's incoming message HTML with one remote image and returns the
// proxy path the timeline offers for it.
func seedMailImage(t *testing.T, api http.Handler, pool *pgxpool.Pool, admin, conversationID, imageURL string) string {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `UPDATE messages SET body_html=$2 WHERE conversation_id=$1 AND kind='incoming'`, conversationID, `<p>Hi <img src="`+imageURL+`"></p>`); err != nil {
		t.Fatal(err)
	}
	for _, item := range storedMessages(t, api, admin, conversationID) {
		if item.BodyHTML != nil {
			if match := proxySrc.FindStringSubmatch(*item.BodyHTML); match != nil {
				return html.UnescapeString(match[1])
			}
		}
	}
	t.Fatal("the timeline offers no proxy path")
	return ""
}

func createContractInvite(t *testing.T, api http.Handler, token, email, role string) (string, string) {
	t.Helper()
	response := request(t, api, http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": email, "role_ceiling": role}, token, nil)
	var created struct {
		Invite struct {
			ID string `json:"id"`
		} `json:"invite"`
		URL string `json:"url"`
	}
	decode(t, response, &created)
	return created.Invite.ID, created.URL
}

func findSessionID(t *testing.T, api http.Handler, token, label string) string {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/auth/sessions", nil, token, nil)
	var list struct {
		Items []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"items"`
	}
	decode(t, response, &list)
	for _, session := range list.Items {
		if session.Label == label {
			return session.ID
		}
	}
	t.Fatalf("session %q not found", label)
	return ""
}

// withFailedEmailReply connects the email channel and leaves one reply whose delivery failed, so retry has a target.
func withFailedEmailReply(t *testing.T, api http.Handler, pool *pgxpool.Pool, admin, conversationID string) (http.Handler, string, string) {
	t.Helper()
	secret := connectEmail(t, api, admin, "support@contract-all.example.com")
	// The workspace is on a trial, which replies only to customers who wrote first.
	if _, err := pool.Exec(context.Background(), `INSERT INTO messages(workspace_id,conversation_id,kind,body,email_meta)
		SELECT workspace_id,id,'incoming','Customer question','{"from":"contact@contract-all.example.com","authentication":"pass","via":"platform"}' FROM conversations WHERE id=$1`, conversationID); err != nil {
		t.Fatal(err)
	}
	replyID := create(t, api, admin, "/api/v1/conversations/"+conversationID+"/messages", map[string]any{"kind": "outgoing", "body": "Contract reply"})
	if _, err := pool.Exec(context.Background(), `UPDATE channel_outbox SET status='failed', last_error='550 mailbox unavailable' WHERE message_id=$1`, replyID); err != nil {
		t.Fatal(err)
	}
	return api, secret, replyID
}

func memberIDByEmail(t *testing.T, api http.Handler, token, email string) string {
	t.Helper()
	var body struct {
		Items []teamMemberView `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/agents", nil, token, nil), &body)
	for _, item := range body.Items {
		if item.Email == email {
			return item.ID
		}
	}
	t.Fatalf("no agent %s", email)
	return ""
}

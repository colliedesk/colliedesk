package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

type contractFixture struct {
	api                       http.Handler
	adminToken, agentToken    string
	contactID, conversationID string
	inviteID, sessionID       string
	updateTagID, deleteTagID  string
	attachmentID              string
	redeemToken, logoutToken  string
	inboundSecret             string
	failedReplyID             string
	deleteChannelToken        string
	portalToken, webhookBody  string
}

func newContractFixture(t *testing.T) contractFixture {
	t.Helper()
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fakePolar := newFakePolarAPI(t)
	api, service, pool, dns := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Attachments = storage
		config.Billing = fakePolar.config()
	})
	adminToken := createSession(t, api, service, "Contract All", "admin@contract-all.example.com")
	contactID := create(t, api, adminToken, "/api/v1/contacts", map[string]any{
		"name": "Contract Contact", "email": "contact@contract-all.example.com",
	})
	conversationID := create(t, api, adminToken, "/api/v1/conversations", map[string]any{
		"contact_id": contactID, "subject": "Contract conversation",
	})
	noteID := create(t, api, adminToken, "/api/v1/conversations/"+conversationID+"/messages", map[string]any{
		"kind": "note", "body": "Contract note",
	})
	attachmentID := seedAttachment(t, pool, storage, adminToken, api, noteID, "contract.txt", "contract bytes")
	seedImportDetails(t, pool, workspaceID(t, api, adminToken), conversationID, noteID)
	updateTagID := create(t, api, adminToken, "/api/v1/tags", map[string]any{"name": "Update contract tag"})
	deleteTagID := create(t, api, adminToken, "/api/v1/tags", map[string]any{"name": "Delete contract tag"})

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

	return contractFixture{api: api, portalToken: portalToken, webhookBody: string(subscription.event("subscription.updated")), inboundSecret: inboundSecret, failedReplyID: failedReplyID, deleteChannelToken: deleteChannelToken, adminToken: adminToken, agentToken: agentSession.Token,
		contactID: contactID, conversationID: conversationID, inviteID: inviteID, sessionID: sessionID,
		updateTagID: updateTagID, deleteTagID: deleteTagID, attachmentID: attachmentID, redeemToken: linkToken(t, redeemURL), logoutToken: logout.Token}
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
	if _, err := pool.Exec(context.Background(), `INSERT INTO messages(workspace_id,conversation_id,kind,body)
		SELECT workspace_id,id,'incoming','Customer question' FROM conversations WHERE id=$1`, conversationID); err != nil {
		t.Fatal(err)
	}
	replyID := create(t, api, admin, "/api/v1/conversations/"+conversationID+"/messages", map[string]any{"kind": "outgoing", "body": "Contract reply"})
	if _, err := pool.Exec(context.Background(), `UPDATE email_outbox SET status='failed', last_error='550 mailbox unavailable' WHERE message_id=$1`, replyID); err != nil {
		t.Fatal(err)
	}
	return api, secret, replyID
}

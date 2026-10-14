package httpapi_test

import (
	"context"
	"net/http"
	"testing"
)

type contractFixture struct {
	api                       http.Handler
	adminToken, agentToken    string
	contactID, conversationID string
	inviteID, sessionID       string
	updateTagID, deleteTagID  string
	redeemToken, logoutToken  string
}

func newContractFixture(t *testing.T) contractFixture {
	t.Helper()
	api, service := testAPI(t)
	adminToken := createSession(t, api, service, "Contract All", "admin@contract-all.example.com")
	contactID := create(t, api, adminToken, "/api/v1/contacts", map[string]any{
		"name": "Contract Contact", "email": "contact@contract-all.example.com",
	})
	conversationID := create(t, api, adminToken, "/api/v1/conversations", map[string]any{
		"contact_id": contactID, "subject": "Contract conversation",
	})
	_ = create(t, api, adminToken, "/api/v1/conversations/"+conversationID+"/messages", map[string]any{
		"kind": "note", "body": "Contract note",
	})
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

	return contractFixture{api: api, adminToken: adminToken, agentToken: agentSession.Token,
		contactID: contactID, conversationID: conversationID, inviteID: inviteID, sessionID: sessionID,
		updateTagID: updateTagID, deleteTagID: deleteTagID, redeemToken: linkToken(t, redeemURL), logoutToken: logout.Token}
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

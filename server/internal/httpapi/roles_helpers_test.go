package httpapi_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

// joinAsAgent invites email with the Agent role and returns the session of the person who redeems it.
func joinAsAgent(t *testing.T, api http.Handler, admin, email string) string {
	t.Helper()
	_, link := createContractInvite(t, api, admin, email, "agent")
	response := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, link)}, "", nil)
	var session struct {
		Token string `json:"token"`
	}
	decode(t, response, &session)
	if session.Token == "" {
		t.Fatalf("redeem agent invite status=%d body=%s", response.Code, response.Body.String())
	}
	return session.Token
}

// requestDesk is a workspace with an admin, a contact and a conversation, on a clock the test moves.
type requestDesk struct {
	api                   http.Handler
	admin                 string
	conversation, contact string
	clock                 *testClock
	service               *auth.Service
	pool                  *pgxpool.Pool
}

func newRequestDesk(t *testing.T, name string) requestDesk {
	t.Helper()
	clock := &testClock{now: time.Date(2026, time.October, 17, 9, 0, 0, 0, time.UTC)}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Now = clock.Now })
	slug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	admin := createSession(t, api, service, name, "admin@"+slug+".example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@" + slug + ".example.com"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Printer jam"})
	return requestDesk{api: api, admin: admin, conversation: conversation, contact: contact, clock: clock, service: service, pool: pool}
}

func conversationStatus(t *testing.T, api http.Handler, token, id string) string {
	t.Helper()
	var conversation struct {
		Status string `json:"status"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+id, nil, token, nil), &conversation)
	return conversation.Status
}

// refused asserts the 403 permission_missing a call without the permission gets, checks it against the
// contract and that it names permission, and returns from whom it is missing (token or person).
func refused(t *testing.T, api http.Handler, method, path string, body any, token, permission string) string {
	t.Helper()
	response := serve(api, newRequest(t, method, path, body, token, nil))
	missing, names, from := permissionMissing(t, response)
	if !missing || !slices.Contains(names, permission) {
		t.Fatalf("%s %s status=%d body=%s, want 403 permission_missing naming %s", method, path, response.Code, response.Body.String(), permission)
	}
	validateContractResponse(t, newRequest(t, method, path, body, token, nil), response)
	return from
}

// roleList reads the roles of an agent or a member.
type roleList []struct {
	Name   string  `json:"name"`
	System *string `json:"system"`
}

// main is the built-in role the list holds, admin before agent; "" when it holds neither.
func (r roleList) main() string {
	found := ""
	for _, role := range r {
		if role.System != nil && *role.System == "admin" {
			return "admin"
		}
		if role.System != nil && *role.System == "agent" {
			found = "agent"
		}
	}
	return found
}

func ptr[T any](value T) *T { return &value }

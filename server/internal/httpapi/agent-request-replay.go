package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
)

type approvalKey struct{}

// approval is an approved agent request being replayed. Only replayApproved puts it in a context, and
// the key is unexported, so no client can claim it.
type approval struct {
	approver  auth.Principal
	requestID string
	tokenID   string
	// textEdited is true when the approver changed the words of a reply draft.
	textEdited bool
}

func approvalOf(ctx context.Context) (approval, bool) {
	value, ok := ctx.Value(approvalKey{}).(approval)
	return value, ok
}

// withheldResults are the approval routes whose answer is a credential, a one-time link or a payment
// page. The approver sees it once in the approve answer; the request keeps only the status, so the
// token (or anyone reading the request later) never receives it.
var withheldResults = map[string]bool{
	"POST /api/v1/auth/tokens":           true,
	"POST /api/v1/auth/invites":          true,
	"POST /api/v1/channels/email/secret": true,
	"GET /api/v1/export":                 true,
	"POST /api/v1/export/link":           true,
	"POST /api/v1/billing/checkout":      true,
	"POST /api/v1/billing/portal":        true,
}

// approvalRunsAs replays a route as another one with the same effect. A whole export streamed into a
// request record would be useless and huge, so approving one gives the approver the download link the
// settings page uses: the same data, leaving through the same door.
var approvalRunsAs = map[string][2]string{
	"GET /api/v1/export": {http.MethodPost, "/api/v1/export/link"},
}

// replayApproved runs the stored call through the API's own routes as the approving person: their
// role, the lock and every handler check apply as if they had made the call. It returns the status,
// the answer and whether the answer fit in domain.MaxAgentRequestBody.
func (a *API) replayApproved(ctx context.Context, approver auth.Principal, item domain.AgentRequest) (int, []byte, bool) {
	method, path := item.Method, item.Path
	call := item.Body
	if len(item.EditedBody) > 0 {
		call = item.EditedBody
	}
	if replacement, ok := approvalRunsAs[item.Method+" "+item.Route]; ok {
		method, path = replacement[0], replacement[1]
	}
	var body io.Reader = http.NoBody
	if len(call) > 0 {
		body = bytes.NewReader(call)
	}
	replay := approval{approver: approver, requestID: item.ID, textEdited: replyText(item.EditedBody) != "" && replyText(item.EditedBody) != replyText(item.Body)}
	if item.RequestedBy.TokenID != nil {
		replay.tokenID = *item.RequestedBy.TokenID
	}
	request, err := http.NewRequestWithContext(context.WithValue(ctx, approvalKey{}, replay), method, path, body)
	if err != nil {
		return http.StatusBadRequest, nil, true
	}
	if len(call) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	answer := &cappedRecorder{header: http.Header{}, limit: domain.MaxAgentRequestBody}
	a.mux.ServeHTTP(answer, request)
	if answer.status == 0 {
		answer.status = http.StatusOK
	}
	return answer.status, answer.body.Bytes(), !answer.overflow
}

// requestedScope is the scope a token creation call asks for.
func requestedScope(call json.RawMessage) string {
	var fields struct {
		Scope string `json:"scope"`
	}
	_ = json.Unmarshal(call, &fields)
	return fields.Scope
}

// replyText is the text of a reply draft's call; empty for anything else.
func replyText(call json.RawMessage) string {
	var fields struct {
		Body string `json:"body"`
	}
	if json.Unmarshal(call, &fields) != nil {
		return ""
	}
	return fields.Body
}

// routePermissions maps "METHOD pattern" to the role permission the route needs.
var routePermissions = func() map[string]string {
	permissions := map[string]string{}
	for _, route := range Routes() {
		permissions[route.Method+" "+route.Pattern] = route.Permission
	}
	return permissions
}()

// approvalPermission is the permission the person approving item needs: that of the route its call
// runs on.
func approvalPermission(item domain.AgentRequest) string {
	key := item.Method + " " + item.Route
	// Only an admin can create an admin token, so only an admin can approve asking for one.
	if key == "POST /api/v1/auth/tokens" && requestedScope(item.Body) == auth.ScopeAdmin {
		return auth.PermissionAccessWrite
	}
	if replacement, ok := approvalRunsAs[key]; ok {
		key = replacement[0] + " " + replacement[1]
	}
	return routePermissions[key]
}

// ownerRoutes act on the person the token belongs to (the "me" of the path). Approving one replays it
// as the approver, so only that person may approve it; anyone else would change their own settings.
var ownerRoutes = map[string]bool{"PATCH /api/v1/agents/me/timezone": true, "PATCH /api/v1/agents/me/display-name": true}

// ownerOnly reports whether only the token's person may approve item.
func ownerOnly(item domain.AgentRequest) bool { return ownerRoutes[item.Method+" "+item.Route] }

// approvableBy is who may approve item: "owner" (only the person the token belongs to), or the lowest
// role, "agent" or "admin".
func approvableBy(item domain.AgentRequest) string {
	if ownerOnly(item) {
		return "owner"
	}
	if auth.HasPermission("agent", approvalPermission(item)) {
		return "agent"
	}
	return "admin"
}

var errAnswerTooLarge = errors.New("the answer of an approved request is larger than the record keeps")

// cappedRecorder keeps what a replayed handler wrote, up to limit bytes.
type cappedRecorder struct {
	header   http.Header
	status   int
	body     bytes.Buffer
	limit    int
	overflow bool
}

func (r *cappedRecorder) Header() http.Header { return r.header }

func (r *cappedRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}

func (r *cappedRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if r.body.Len()+len(data) > r.limit {
		r.overflow = true
		return 0, errAnswerTooLarge
	}
	return r.body.Write(data)
}

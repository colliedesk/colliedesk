package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type restAgentRequest struct {
	ID             string          `json:"id"`
	Kind           string          `json:"kind"`
	Status         string          `json:"status"`
	Method         string          `json:"method"`
	Path           string          `json:"path"`
	Body           json.RawMessage `json:"body"`
	Summary        string          `json:"summary"`
	ConversationID *string         `json:"conversation_id"`
	RequestedBy    struct {
		TokenLabel string `json:"token_label"`
	} `json:"requested_by"`
	DecidedBy *struct {
		Name string `json:"name"`
	} `json:"decided_by"`
	DecidedAt    *time.Time `json:"decided_at"`
	Reason       *string    `json:"reason"`
	ResultStatus *int       `json:"result_status"`
	ResultNote   *string    `json:"result_note"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
}

// requestOut is a request as a model reads it. The answer of the approved call is left out: it can
// hold customer text, and result_status says whether the call worked.
type requestOut struct {
	ID             string          `json:"id"`
	Kind           string          `json:"kind"`
	Status         string          `json:"status"`
	Summary        string          `json:"summary"`
	Call           string          `json:"call"`
	Body           json.RawMessage `json:"body,omitempty"`
	ConversationID string          `json:"conversation_id,omitempty"`
	RequestedBy    string          `json:"requested_by"`
	DecidedBy      string          `json:"decided_by,omitempty"`
	DecidedAt      string          `json:"decided_at,omitempty"`
	Reason         string          `json:"reason,omitempty"`
	ResultStatus   *int            `json:"result_status,omitempty"`
	ResultNote     string          `json:"result_note,omitempty"`
	CreatedAt      string          `json:"created_at"`
	ExpiresAt      string          `json:"expires_at"`
}

func requestView(item restAgentRequest) requestOut {
	out := requestOut{ID: item.ID, Kind: item.Kind, Status: item.Status, Summary: item.Summary, Call: item.Method + " " + item.Path,
		RequestedBy: item.RequestedBy.TokenLabel, ResultStatus: item.ResultStatus, CreatedAt: utc(item.CreatedAt), ExpiresAt: utc(item.ExpiresAt)}
	if len(item.Body) > 0 && string(item.Body) != "null" {
		out.Body = item.Body
	}
	if item.ConversationID != nil {
		out.ConversationID = *item.ConversationID
	}
	if item.DecidedBy != nil {
		out.DecidedBy = item.DecidedBy.Name
	}
	if item.DecidedAt != nil {
		out.DecidedAt = utc(*item.DecidedAt)
	}
	if item.Reason != nil {
		out.Reason = *item.Reason
	}
	if item.ResultNote != nil {
		out.ResultNote = *item.ResultNote
	}
	return out
}

type filedOut struct {
	AgentRequest filedRequest `json:"agent_request"`
	Next         string       `json:"next"`
}

type filedRequest struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Summary   string `json:"summary"`
	ExpiresAt string `json:"expires_at"`
}

// requestedOutput is what a model reads when its call was filed as an agent request instead of run.
// It is a result, not an error: the call did what Colliedesk allows, and the model must know nothing
// changed yet and how to follow the request.
func requestedOutput(c *call, filed *agentRequested) filedOut {
	next := []string{"Colliedesk did not do this yet: a person must approve it in Colliedesk (Requests) first, within seven days. " +
		"Tell the user it is waiting for approval. Follow it with colliedesk_request_get (request_id " + filed.RequestID + "); withdraw it with colliedesk_request_cancel. " +
		"Do not repeat the call: each call files another request."}
	if filed.Kind == "reply_draft" {
		next = append(next, "The reply is a draft: a person can edit it, send it or discard it.")
	}
	if c.wrote {
		next = append(next, "Changes this call made before this step are already saved.")
	}
	next = append(next, "If this call asked for more than this one change, the rest did not run: call the tool again after the request is approved.")
	return filedOut{AgentRequest: filedRequest{ID: filed.RequestID, Kind: filed.Kind, Status: filed.Status, Summary: filed.Summary, ExpiresAt: filed.ExpiresAt},
		Next: strings.Join(next, " ")}
}

// outputSchema is the advertised output schema of a tool. A tool that changes data may answer with an
// agent request instead of its usual result, so its schema allows either shape.
func outputSchema(spec Spec) string {
	if spec.ReadOnly || strings.Contains(spec.Output, `"agent_request"`) {
		return spec.Output
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(spec.Output), &schema); err != nil {
		return spec.Output
	}
	properties, _ := schema["properties"].(map[string]any)
	if properties == nil {
		properties = map[string]any{}
	}
	var filed map[string]any
	_ = json.Unmarshal([]byte(filedSchema), &filed)
	for name, property := range filed["properties"].(map[string]any) {
		properties[name] = property
	}
	schema["properties"] = properties
	required, hasRequired := schema["required"]
	delete(schema, "required")
	usual := map[string]any{}
	if hasRequired {
		usual["required"] = required
	}
	schema["anyOf"] = []any{usual, map[string]any{"required": filed["required"]}}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return spec.Output
	}
	return string(encoded)
}

func requestsList(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Status string `json:"status"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if args.Status != "" {
		query.Set("status", args.Status)
	}
	var page struct {
		Items []restAgentRequest `json:"items"`
	}
	if err := c.get("/api/v1/agent-requests", query, &page); err != nil {
		return nil, err
	}
	out := make([]requestOut, len(page.Items))
	for index, item := range page.Items {
		out[index] = requestView(item)
	}
	return map[string]any{"requests": out}, nil
}

func requestID(arguments map[string]any) (string, error) {
	args, err := decodeArguments[struct {
		RequestID string `json:"request_id"`
	}](arguments)
	return args.RequestID, err
}

func requestGet(c *call, arguments map[string]any) (any, error) {
	id, err := requestID(arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No request " + id + " filed by this token. List yours with colliedesk_requests_list."
	var item restAgentRequest
	if err := c.get("/api/v1/agent-requests/"+url.PathEscape(id), nil, &item); err != nil {
		return nil, err
	}
	return map[string]any{"request": requestView(item)}, nil
}

func requestCancel(c *call, arguments map[string]any) (any, error) {
	id, err := requestID(arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No request " + id + " filed by this token. List yours with colliedesk_requests_list."
	var item restAgentRequest
	if err := c.send(http.MethodPost, "/api/v1/agent-requests/"+url.PathEscape(id)+"/cancel", nil, &item); err != nil {
		return nil, err
	}
	return map[string]any{"request": requestView(item)}, nil
}

func replyDraft(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ConversationID string   `json:"conversation_id"`
		Body           string   `json:"body"`
		CC             []string `json:"cc"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Body) == "" {
		return nil, &toolError{"The draft is empty. Write the reply text in body."}
	}
	c.notFound = notFoundConversation(args.ConversationID)
	call := map[string]any{"kind": "outgoing", "body": args.Body, "draft": true}
	if len(args.CC) > 0 {
		call["cc"] = args.CC
	}
	return ask(c, http.MethodPost, "/api/v1/conversations/"+url.PathEscape(args.ConversationID)+"/messages", call)
}

// ask makes a call that Colliedesk always files as an agent request for a token.
func ask(c *call, method, path string, body any) (any, error) {
	err := c.send(method, path, body, nil)
	if err == nil {
		return nil, &toolError{"Colliedesk ran this call without asking a person, which it never does for an agent token. Tell a workspace admin."}
	}
	return nil, err
}

func tokenCreateRequest(c *call, arguments map[string]any) (any, error) {
	return ask(c, http.MethodPost, "/api/v1/auth/tokens", arguments)
}

func workspaceExportRequest(c *call, arguments map[string]any) (any, error) {
	if delivery, _ := arguments["delivery"].(string); delivery == "download" {
		return ask(c, http.MethodGet, "/api/v1/export", nil)
	}
	return ask(c, http.MethodPost, "/api/v1/export/link", nil)
}

func billingRequest(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Action string `json:"action"`
		Plan   string `json:"plan"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if args.Action == "portal" {
		return ask(c, http.MethodPost, "/api/v1/billing/portal", nil)
	}
	if args.Plan == "" {
		return nil, &toolError{"A checkout needs a plan: solo_monthly, solo_yearly, small_monthly, small_yearly, team_monthly, team_yearly, team25_monthly, team25_yearly or founding."}
	}
	return ask(c, http.MethodPost, "/api/v1/billing/checkout", map[string]any{"plan": args.Plan})
}

func emailChannelDisconnectRequest(c *call, _ map[string]any) (any, error) {
	return ask(c, http.MethodDelete, "/api/v1/channels/email", nil)
}

func emailSecretRotateRequest(c *call, arguments map[string]any) (any, error) {
	return ask(c, http.MethodPost, emailChannelPath(c, arguments, "/secret"), nil)
}

func gmailConnectRequest(c *call, arguments map[string]any) (any, error) {
	return ask(c, http.MethodPost, emailChannelPath(c, arguments, "/gmail"), nil)
}

func gmailDisconnectRequest(c *call, arguments map[string]any) (any, error) {
	return ask(c, http.MethodDelete, emailChannelPath(c, arguments, "/gmail"), nil)
}

func microsoftConnectRequest(c *call, arguments map[string]any) (any, error) {
	return ask(c, http.MethodPost, emailChannelPath(c, arguments, "/microsoft"), nil)
}

func microsoftDisconnectRequest(c *call, arguments map[string]any) (any, error) {
	return ask(c, http.MethodDelete, emailChannelPath(c, arguments, "/microsoft"), nil)
}

func workspaceSlugRequest(c *call, arguments map[string]any) (any, error) {
	return ask(c, http.MethodPatch, "/api/v1/workspace/slug", arguments)
}

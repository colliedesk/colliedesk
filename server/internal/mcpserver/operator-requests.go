package mcpserver

// Operator tools still file operator requests: an operator approves each change in the operator
// console with their authenticator code. Workspace tools never do; a workspace token acts at once or
// is refused.

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

// requestedOutput is what a model reads when an operator call was filed instead of run: a result, not
// an error, that says nothing changed yet.
func requestedOutput(filed *agentRequested) filedOut {
	return filedOut{AgentRequest: filedRequest{ID: filed.RequestID, Kind: "operator", Status: filed.Status, Summary: filed.Summary, ExpiresAt: filed.ExpiresAt},
		Next: "Nothing changed yet: an operator must approve this in the operator console (Requests), within seven days. Tell the user it is waiting. Do not repeat the call: each call files another request."}
}

// ask makes an operator call that Colliedesk always files as an operator request.
func ask(c *call, method, path string, body any) (any, error) {
	err := c.send(method, path, body, nil)
	if err == nil {
		return nil, &toolError{"Colliedesk ran this call without asking an operator, which it never does for an operator token. Tell an operator."}
	}
	return nil, err
}

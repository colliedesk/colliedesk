package mcpserver

import (
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

// The operator tools run the whole server, not a workspace (docs/internals/operator-console.md). They are
// served on OperatorPath to operator tokens (cdop_) only; Path never lists or runs them. An operator token
// reads; every change it asks for waits for an operator to approve it in the console.

const operatorToolNote = "\n\nTier: operator (an operator token on " + OperatorPath + "; a change waits for an operator's approval)."

func operatorWhoamiSpec() Spec {
	return Spec{
		Name:  "colliedesk_operator_whoami",
		Title: "Show which operator this token acts as",
		Description: `Show the operator this token acts as, what the token may ask for, and where the operator console is. Call it first.` +
			operatorToolNote,
		Tier: auth.ScopeOperator, ReadOnly: true,
		REST:   []string{"GET /api/v1/operator/auth/me"},
		Input:  noArguments,
		Output: `{"type":"object","required":["operator","session","console"],"properties":{"operator":{"type":"object"},"session":{"type":"object"},"console":{"type":"object"}}}`,
		run: func(c *call, _ map[string]any) (any, error) {
			var me map[string]any
			err := c.get("/api/v1/operator/auth/me", nil, &me)
			return me, err
		},
	}
}

func operatorSignOutSpec() Spec {
	return Spec{
		Name:        "colliedesk_operator_sign_out",
		Title:       "End this operator token",
		Description: `End this operator token at once. Nothing else changes; an operator makes a new token in the console.` + operatorToolNote,
		Tier:        auth.ScopeOperator, Destructive: true,
		REST:   []string{"POST /api/v1/operator/auth/logout"},
		Input:  noArguments,
		Output: `{"type":"object","required":["signed_out"],"properties":{"signed_out":{"type":"boolean"}}}`,
		run: func(c *call, _ map[string]any) (any, error) {
			if err := c.send(http.MethodPost, "/api/v1/operator/auth/logout", nil, nil); err != nil {
				return nil, err
			}
			return map[string]any{"signed_out": true}, nil
		},
	}
}

func operatorTokenCreateSpec() Spec {
	return Spec{
		Name:  "colliedesk_operator_token_create",
		Title: "Ask for another operator token",
		Description: `Ask an operator for another operator token (for another AI client). It always becomes a request: an operator approves it ` +
			`in the console with their authenticator code and sees the token once; you never receive it. Say why in note.` + operatorToolNote,
		Tier: auth.ScopeOperator,
		REST: []string{"POST /api/v1/operator/tokens"},
		Input: `{"type":"object","additionalProperties":false,"required":["label"],"properties":{` +
			`"label":{"type":"string","minLength":1,"maxLength":100,"description":"What the token is for."},` +
			`"permissions":{"type":"array","items":{"type":"string","enum":["operator_requests:cancel","operator_config:write","operator:manage"]},"description":"Besides reading, which it always has."},` +
			`"note":{"type":"string","maxLength":500,"description":"Why you ask, shown to the operator who decides."}}}`,
		Output: `{"type":"object","required":["agent_request"],"properties":{"agent_request":{"type":"object"},"next":{"type":"string"}}}`,
		run: func(c *call, arguments map[string]any) (any, error) {
			return ask(c, http.MethodPost, "/api/v1/operator/tokens", arguments)
		},
	}
}

// operatorSpecs are the tools of OperatorPath.
func operatorSpecs() []Spec {
	return []Spec{operatorWhoamiSpec(), operatorSignOutSpec(), operatorTokenCreateSpec()}
}

package mcpserver

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

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
	return []Spec{operatorWhoamiSpec(), operatorSignOutSpec(), operatorTokenCreateSpec(),
		operatorRead("colliedesk_operator_status", "Show the server's status", "Show the server's release, the configuration version each process runs, what needs attention and how many requests wait.", "/api/v1/operator/status", nil),
		operatorRead("colliedesk_operator_config_get", "Read the server's settings", "Read every setting: its value (a secret only says whether it is set), default, where it came from and what changing it asks for.", "/api/v1/operator/config", nil),
		operatorCheckSpec(), operatorConfigChangeSpec(),
		operatorRead("colliedesk_operator_config_history", "List configuration versions", "List the configuration versions, newest first, with what each changed (a secret only says it was replaced).", "/api/v1/operator/config/versions", []string{"limit"}),
		operatorRestoreSpec(),
		operatorRead("colliedesk_operator_audit", "Read the operator audit log", "Read who changed the server's settings and operators, newest first; filter by setting (key) or person (actor).", "/api/v1/operator/audit", []string{"key", "actor", "since", "limit"}),
		operatorRead("colliedesk_operator_requests", "List your operator requests", "List the changes this token asked for and what became of them.", "/api/v1/operator/requests", nil),
		operatorRequestGetSpec(), operatorRequestCancelSpec(),
		operatorRead("colliedesk_operator_team", "List operators and sessions", "List the operators and their sessions and tokens (never a token or a link).", "/api/v1/operator/operators", nil),
		operatorInviteSpec(), operatorRevokeSpec(), operatorSessionRevokeSpec(),
	}
}

// operatorRead is a read-only tool that answers one GET as it is; query names the arguments it passes on.
func operatorRead(name, title, description, path string, query []string) Spec {
	properties := []string{}
	for _, argument := range query {
		kind := `"string"`
		if argument == "limit" {
			kind = `"integer","minimum":1,"maximum":1000`
		}
		properties = append(properties, `"`+argument+`":{"type":`+kind+`}`)
	}
	input := noArguments
	if len(properties) > 0 {
		input = `{"type":"object","additionalProperties":false,"properties":{` + strings.Join(properties, ",") + `}}`
	}
	return Spec{
		Name: name, Title: title, Description: description + operatorToolNote,
		Tier: auth.ScopeOperator, ReadOnly: true,
		REST:   []string{"GET " + path},
		Input:  input,
		Output: `{"type":"object"}`,
		run: func(c *call, arguments map[string]any) (any, error) {
			values := url.Values{}
			for _, argument := range query {
				if value, ok := arguments[argument]; ok {
					values.Set(argument, fmt.Sprint(value))
				}
			}
			var out map[string]any
			err := c.get(path, values, &out)
			return out, err
		},
	}
}

const changeProperties = `"set":{"type":"object","additionalProperties":{"type":"string"},"description":"Settings to save, by dotted name (never a secret)."},` +
	`"unset":{"type":"array","items":{"type":"string"},"description":"Settings back to their default."},` +
	`"secrets":{"type":"object","additionalProperties":{"type":"null"},"description":"Secrets the change needs, each as null: the operator who approves types them. Never send a secret's value."}`

func operatorCheckSpec() Spec {
	return Spec{
		Name: "colliedesk_operator_config_check", Title: "Check a settings change",
		Description: `Check a change of the server's settings without saving it: whether it builds, and with test "smtp" a test email to the operator. Nothing is saved.` + operatorToolNote,
		Tier:        auth.ScopeOperator, ReadOnly: true,
		REST:   []string{"POST /api/v1/operator/config/check"},
		Input:  `{"type":"object","additionalProperties":false,"properties":{` + changeProperties + `,"test":{"type":"string","enum":["smtp"]}}}`,
		Output: `{"type":"object","required":["ok","errors"],"properties":{"ok":{"type":"boolean"},"errors":{"type":"object"},"test":{"type":"object"}}}`,
		run: func(c *call, arguments map[string]any) (any, error) {
			var out map[string]any
			err := c.send(http.MethodPost, "/api/v1/operator/config/check", arguments, &out)
			return out, err
		},
	}
}

func operatorConfigChangeSpec() Spec {
	return Spec{
		Name: "colliedesk_operator_config_change", Title: "Ask to change the server's settings",
		Description: `Ask an operator to change the server's settings. It always becomes a request an operator approves with their code. ` +
			`base_version is the version you read (colliedesk_operator_config_get). Name a secret the change needs with null; never send its value. Say why in note.` + operatorToolNote,
		Tier: auth.ScopeOperator,
		REST: []string{"PATCH /api/v1/operator/config"},
		Input: `{"type":"object","additionalProperties":false,"required":["base_version"],"properties":{"base_version":{"type":"integer","minimum":0},` + changeProperties +
			`,"note":{"type":"string","maxLength":500}}}`,
		Output: `{"type":"object","required":["agent_request"],"properties":{"agent_request":{"type":"object"},"next":{"type":"string"}}}`,
		run: func(c *call, arguments map[string]any) (any, error) {
			return ask(c, http.MethodPatch, "/api/v1/operator/config", arguments)
		},
	}
}

func operatorRestoreSpec() Spec {
	return Spec{
		Name: "colliedesk_operator_config_restore", Title: "Ask to restore a configuration version",
		Description: `Ask an operator to save an older configuration version again as the newest one. It always becomes a request an operator approves.` + operatorToolNote,
		Tier:        auth.ScopeOperator,
		REST:        []string{"POST /api/v1/operator/config/versions/{id}/restore"},
		Input:       `{"type":"object","additionalProperties":false,"required":["version","base_version"],"properties":{"version":{"type":"integer","minimum":1},"base_version":{"type":"integer","minimum":0},"note":{"type":"string","maxLength":500}}}`,
		Output:      `{"type":"object","required":["agent_request"],"properties":{"agent_request":{"type":"object"},"next":{"type":"string"}}}`,
		run: func(c *call, arguments map[string]any) (any, error) {
			version := fmt.Sprint(arguments["version"])
			delete(arguments, "version")
			return ask(c, http.MethodPost, "/api/v1/operator/config/versions/"+url.PathEscape(version)+"/restore", arguments)
		},
	}
}

var operatorRequestIDInput = `{"type":"object","additionalProperties":false,"required":["request_id"],"properties":{"request_id":{"type":"string",` + uuid + `}}}`

func operatorRequestGetSpec() Spec {
	return Spec{
		Name: "colliedesk_operator_request_get", Title: "Show one of your operator requests",
		Description: `Show one operator request this token made: whether it waits, was approved (and the call's answer) or was refused.` + operatorToolNote,
		Tier:        auth.ScopeOperator, ReadOnly: true,
		REST:   []string{"GET /api/v1/operator/requests/{id}"},
		Input:  operatorRequestIDInput,
		Output: `{"type":"object"}`,
		run: func(c *call, arguments map[string]any) (any, error) {
			var out map[string]any
			err := c.get("/api/v1/operator/requests/"+url.PathEscape(fmt.Sprint(arguments["request_id"])), nil, &out)
			return out, err
		},
	}
}

func operatorRequestCancelSpec() Spec {
	return Spec{
		Name: "colliedesk_operator_request_cancel", Title: "Withdraw one of your operator requests",
		Description: `Withdraw an operator request this token made that still waits.` + operatorToolNote,
		Tier:        auth.ScopeOperator, Idempotent: true,
		REST:   []string{"POST /api/v1/operator/requests/{id}/cancel"},
		Input:  operatorRequestIDInput,
		Output: `{"type":"object"}`,
		run: func(c *call, arguments map[string]any) (any, error) {
			var out map[string]any
			err := c.send(http.MethodPost, "/api/v1/operator/requests/"+url.PathEscape(fmt.Sprint(arguments["request_id"]))+"/cancel", nil, &out)
			return out, err
		},
	}
}

func operatorInviteSpec() Spec {
	return Spec{
		Name: "colliedesk_operator_invite", Title: "Ask to invite an operator",
		Description: `Ask an operator to invite another operator by email. It always becomes a request; the approver sees the one-time link, you never do.` + operatorToolNote,
		Tier:        auth.ScopeOperator,
		REST:        []string{"POST /api/v1/operator/operators"},
		Input:       `{"type":"object","additionalProperties":false,"required":["email"],"properties":{"email":{"type":"string","minLength":3,"maxLength":320},"note":{"type":"string","maxLength":500}}}`,
		Output:      `{"type":"object","required":["agent_request"],"properties":{"agent_request":{"type":"object"},"next":{"type":"string"}}}`,
		run: func(c *call, arguments map[string]any) (any, error) {
			return ask(c, http.MethodPost, "/api/v1/operator/operators", arguments)
		},
	}
}

func operatorRevokeSpec() Spec {
	return Spec{
		Name: "colliedesk_operator_revoke", Title: "Ask to revoke an operator",
		Description: `Ask an operator to revoke another operator: every session and token of theirs would stop at once (inviting them again lets them back in). It always becomes a request.` + operatorToolNote,
		Tier:        auth.ScopeOperator,
		REST:        []string{"DELETE /api/v1/operator/operators/{id}"},
		Input:       `{"type":"object","additionalProperties":false,"required":["operator_id"],"properties":{"operator_id":{"type":"string",` + uuid + `}}}`,
		Output:      `{"type":"object","required":["agent_request"],"properties":{"agent_request":{"type":"object"},"next":{"type":"string"}}}`,
		run: func(c *call, arguments map[string]any) (any, error) {
			return ask(c, http.MethodDelete, "/api/v1/operator/operators/"+url.PathEscape(fmt.Sprint(arguments["operator_id"])), nil)
		},
	}
}

func operatorSessionRevokeSpec() Spec {
	return Spec{
		Name: "colliedesk_operator_session_revoke", Title: "Ask to end an operator session or token",
		Description: `Ask an operator to end one operator session or token. It always becomes a request.` + operatorToolNote,
		Tier:        auth.ScopeOperator,
		REST:        []string{"DELETE /api/v1/operator/sessions/{id}"},
		Input:       `{"type":"object","additionalProperties":false,"required":["session_id"],"properties":{"session_id":{"type":"string",` + uuid + `}}}`,
		Output:      `{"type":"object","required":["agent_request"],"properties":{"agent_request":{"type":"object"},"next":{"type":"string"}}}`,
		run: func(c *call, arguments map[string]any) (any, error) {
			return ask(c, http.MethodDelete, "/api/v1/operator/sessions/"+url.PathEscape(fmt.Sprint(arguments["session_id"])), nil)
		},
	}
}

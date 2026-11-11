package mcpserver

import (
	"errors"
	"log/slog"
	"maps"
	"slices"
	"strings"
)

// toolError is a failure the model should read and act on. It becomes an isError result, never a
// protocol error, so the model can correct itself.
type toolError struct{ message string }

func (e *toolError) Error() string { return e.message }

// refuse turns a failure of a REST call into the message a model sees. notFound is the text for a
// 404, which only the tool knows how to word.
func (c *call) refuse(err error, notFound string) error {
	if errors.Is(err, errTooLarge) {
		return &toolError{"The answer is too large to return in one call (over 16 MiB). Open this in the web app, or ask an admin to export the data."}
	}
	failure, ok := err.(*restError)
	if !ok {
		return err // nil, a *toolError or an unplanned failure: nothing to translate
	}
	switch {
	case failure.Status == 403 && (failure.Code == "permission_missing" || failure.Code == "role_exceeds_yours" || failure.Code == "member_outranks_you"):
		return &toolError{permissionText(c, failure)}
	case failure.Status == 403 && failure.Code == "person_only":
		return &toolError{personOnlyText(failure)}
	case failure.Status == 401:
		return &toolError{"This Colliedesk token is no longer valid: it was revoked or it expired. Ask the person you work for to create a new one in Settings → External AI agents."}
	case failure.Status == 402 && failure.Code == "workspace_locked":
		return &toolError{"This workspace is locked: its trial ended and no plan is active. Only exporting the data works until someone with billing:manage chooses a plan, so no inbox tool can read or change anything now."}
	case failure.Status == 402:
		return &toolError{"Colliedesk refused because of the plan: " + failure.Message}
	case failure.Status == 403:
		return &toolError{"Colliedesk refused this: " + failure.Message}
	case failure.Status == 404 && notFound != "":
		return &toolError{notFound}
	case failure.Status == 404:
		return &toolError{"Colliedesk could not find that."}
	case failure.Status == 409:
		return &toolError{"Colliedesk refused this: " + failure.Message}
	case failure.Status == 400 || failure.Status == 422:
		// Each refused field with why, so the next call fixes it.
		text := "Colliedesk rejected the request: " + failure.Message
		for _, field := range slices.Sorted(maps.Keys(failure.Fields)) {
			text += " " + field + ": " + failure.Fields[field]
		}
		return &toolError{text}
	case failure.Status == 503 && failure.Message != "":
		return &toolError{"Colliedesk cannot do this right now: " + failure.Message}
	case failure.Status == 429:
		wait := ""
		if failure.RetryAfter != "" {
			wait = " Try again in " + failure.RetryAfter + " seconds."
		}
		return &toolError{"Colliedesk is rate limiting this workspace." + wait}
	}
	return failure
}

// permissionText keeps Colliedesk's own sentence (it names the permission and how to get it) and adds
// what a model needs to act on it: the permissions by name, and whether the token or its person lacks them.
func permissionText(c *call, failure *restError) string {
	text := "Colliedesk refused this: " + failure.Message
	if len(failure.MissingPermissions) > 0 {
		text += " Missing: " + strings.Join(failure.MissingPermissions, ", ") + "."
	}
	switch failure.MissingFrom {
	case "token":
		text += " Your person holds it but this token was not given it: tell them, so they can make a token that includes it (Settings → External AI agents). Do not try the call again with this token."
	case "person":
		text += " The person you act for does not hold it either"
		if len(failure.Ask) > 0 {
			text += "; " + strings.Join(failure.Ask, ", ") + " can give them a role that includes it"
		}
		text += ". Tell them; do not try the call again until they have it."
	}
	return text
}

// personOnlyText says that only a person does this, and where in the web app.
func personOnlyText(failure *restError) string {
	text := "Colliedesk refused this: " + failure.Message
	if failure.Where != "" {
		text += " A person does it in the web app at " + failure.Where + "."
	}
	return text + " No agent token can do it; tell the person you work for."
}

// failureText is what the model reads when a tool failed. A tool that changed data before it failed
// says so: the model must not believe that nothing changed, and must know whether repeating is safe.
func failureText(c *call, err error) string {
	message, planned := "", false
	if toolErr, ok := err.(*toolError); ok {
		message, planned = toolErr.message, true
	} else {
		slog.Error("MCP tool failed", "tool", c.spec.Name, "error", err)
		message = "Colliedesk hit an internal problem while running " + c.spec.Name + "."
	}
	switch {
	case c.wrote && c.spec.Idempotent:
		message += " Some changes of this call may already be applied. It is safe to repeat the call: you state the result you want, so a repeat applies only what is missing."
	case c.wrote:
		message += " Part of this call may already be saved. Read the conversation before you call this tool again, so nothing is added twice."
	case !planned:
		message += " Nothing is known to have changed. Try again; if it keeps failing, tell a workspace admin."
	}
	return message
}

package mcpserver

import (
	"errors"
	"fmt"
	"log/slog"
	"regexp"
)

// toolError is a failure the model should read and act on. It becomes an isError result, never a
// protocol error, so the model can correct itself.
type toolError struct{ message string }

func (e *toolError) Error() string { return e.message }

var scopeMessage = regexp.MustCompile("has the (read|draft|send) scope; this action needs the (read|draft|send) scope")

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
	case failure.Status == 403 && failure.Code == "scope_insufficient":
		return &toolError{c.scopeText(failure)}
	case failure.Status == 401:
		return &toolError{"This Colliedesk token is no longer valid: it was revoked or it expired. Ask a workspace admin for a new agent token."}
	case failure.Status == 402 && failure.Code == "workspace_locked":
		return &toolError{"This workspace is locked: its trial ended and no plan is active. Only exporting the data works until an admin chooses a plan, so no inbox tool can read or change anything now."}
	case failure.Status == 402:
		return &toolError{"Colliedesk refused because of the plan: " + failure.Message}
	case failure.Status == 403:
		return &toolError{"Colliedesk refused this: " + failure.Message}
	case failure.Status == 404 && notFound != "":
		return &toolError{notFound}
	case failure.Status == 404:
		return &toolError{"Colliedesk could not find that."}
	case failure.Status == 400:
		return &toolError{"Colliedesk rejected the request: " + failure.Message}
	case failure.Status == 429:
		wait := ""
		if failure.RetryAfter != "" {
			wait = " Try again in " + failure.RetryAfter + " seconds."
		}
		return &toolError{"Colliedesk is rate limiting this workspace." + wait}
	}
	return failure
}

func (c *call) scopeText(failure *restError) string {
	have, need := "your", c.spec.Tier
	if found := scopeMessage.FindStringSubmatch(failure.Message); found != nil {
		have, need = "`"+found[1]+"`", found[2]
	}
	return fmt.Sprintf("This token has the %s tier, but %s needs the `%s` tier. Ask a workspace admin to create an agent token with the `%s` tier "+
		"(Settings, Sessions and devices) and connect again with it.", have, c.spec.Name, need, need)
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

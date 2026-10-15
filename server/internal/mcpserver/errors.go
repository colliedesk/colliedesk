package mcpserver

import (
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

// problem is what the model reads for a failure nobody planned for. The cause goes to the log.
func problem(spec Spec, err error) string {
	slog.Error("MCP tool failed", "tool", spec.Name, "error", err)
	return "Colliedesk hit an internal problem while running " + spec.Name + ". Nothing is known to have changed. Try again; if it keeps failing, tell a workspace admin."
}

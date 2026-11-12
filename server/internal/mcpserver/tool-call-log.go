package mcpserver

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// callerKey carries the caller of an authenticated MCP request to the tool call log line.
type callerKey struct{}

// caller is what the tool call log line says about who called: never the token itself, its label or a name.
type caller struct {
	workspace string // the workspace id; empty on the operator endpoint
	token     string // oauth (an AI app's OAuth grant), agent_token, or operator
	requestID string // the X-Request-Id that httpapi.WithRequestID gave the request
}

// withCaller hands the caller on to the tool handlers. The SDK's stateless handler runs each call in a
// session made from the request's context, so the value reaches execute.
func withCaller(response http.ResponseWriter, request *http.Request, who caller) *http.Request {
	who.requestID = response.Header().Get("X-Request-Id")
	return request.WithContext(context.WithValue(request.Context(), callerKey{}, who))
}

func callerOf(ctx context.Context) caller {
	who, _ := ctx.Value(callerKey{}).(caller)
	return who
}

// Outcomes of a tool call in its log line.
const (
	outcomeOK           = "ok"
	outcomeAgentRequest = "agent_request" // filed for a person to approve; nothing ran yet
	outcomeRefused      = "refused"       // a planned refusal the model reads and acts on
	outcomeError        = "error"         // an unplanned failure
)

// ending is how a tool call ended. errorType is one of a small fixed set (see refuse and execute), so
// the log can be counted by it; err is set only for an unplanned failure.
type ending struct {
	outcome   string
	errorType string
	err       error
}

func refusedAs(errorType string) ending { return ending{outcome: outcomeRefused, errorType: errorType} }

// ended says how a call that returned err ended: a refusal of the kind refuse recorded, another planned
// refusal, or an unplanned failure.
func (c *call) ended(err error) ending {
	if toolErr, ok := err.(*toolError); ok {
		if kind, known := c.refusals[toolErr]; known {
			return refusedAs(kind)
		}
		return refusedAs("refused")
	}
	return ending{outcome: outcomeError, errorType: "internal", err: err}
}

// log writes the one line of a tool call. It names the workspace, the tool, the kind of token and the
// outcome, never the arguments, the answer or anything a customer wrote. Planned outcomes are INFO; an
// unplanned failure is ERROR with its error, the only log line of that failure.
func (e ending) log(ctx context.Context, who caller, tool string, took time.Duration) {
	level := slog.LevelInfo
	attributes := []any{"workspace", who.workspace, "tool", tool, "token", who.token, "outcome", e.outcome,
		"error_type", e.errorType, "duration_ms", took.Milliseconds(), "request_id", who.requestID}
	if e.err != nil {
		level = slog.LevelError
		attributes = append(attributes, "error", e.err.Error())
	}
	slog.Log(ctx, level, "mcp tool call", attributes...)
}

package mcpserver

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// presenceClientID names the one row an agent token keeps per conversation; the token itself tells
// tokens apart, so every token uses the same name.
const presenceClientID = "mcp-agent"

type restViewer struct {
	AgentID      string     `json:"agent_id"`
	Name         string     `json:"name"`
	Writing      bool       `json:"writing"`
	WritingSince *time.Time `json:"writing_since"`
	EditedAt     *time.Time `json:"edited_at"`
	AgentToken   *struct {
		Label string `json:"label"`
	} `json:"agent_token"`
}

type viewerOut struct {
	AgentID      string `json:"agent_id"`
	Name         string `json:"name"`
	Writing      bool   `json:"writing"`
	WritingSince string `json:"writing_since,omitempty"`
	EditedAt     string `json:"edited_at,omitempty"`
	// AgentToken is the label of the token an AI agent works through; empty for a person.
	AgentToken *tokenLabelOut `json:"agent_token"`
}

type tokenLabelOut struct {
	Label string `json:"label"`
}

func viewersOut(viewers []restViewer) []viewerOut {
	out := make([]viewerOut, 0, len(viewers))
	for _, viewer := range viewers {
		row := viewerOut{AgentID: viewer.AgentID, Name: viewer.Name, Writing: viewer.Writing}
		if viewer.WritingSince != nil {
			row.WritingSince = utc(*viewer.WritingSince)
		}
		if viewer.EditedAt != nil {
			row.EditedAt = utc(*viewer.EditedAt)
		}
		if viewer.AgentToken != nil {
			row.AgentToken = &tokenLabelOut{Label: viewer.AgentToken.Label}
		}
		out = append(out, row)
	}
	return out
}

// writingHint tells the model who is writing a reply on the conversation, so it does not answer the
// customer over a person: it is a warning, never a refusal (a forgotten tab must not block the queue).
func writingHint(viewers []viewerOut) string {
	var people []string
	for _, viewer := range viewers {
		if viewer.Writing && viewer.AgentToken == nil {
			people = append(people, viewer.Name)
		}
	}
	if len(people) == 0 {
		return ""
	}
	return strings.Join(people, ", ") + ` has a reply to this customer open and not sent yet. Do not send a reply over it: ` +
		`leave a private note with colliedesk_note_add, or propose your reply with colliedesk_reply_draft for a person to send.`
}

func conversationPresence(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ConversationID string `json:"conversation_id"`
		Action         string `json:"action"`
		Writing        bool   `json:"writing"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = notFoundConversation(args.ConversationID)
	path := "/api/v1/conversations/" + url.PathEscape(args.ConversationID) + "/presence"
	if args.Action == "leave" {
		if err := c.send(http.MethodDelete, path+"/"+presenceClientID, nil, nil); err != nil {
			return nil, err
		}
		return map[string]any{"conversation_id": args.ConversationID, "action": "leave"}, nil
	}
	var answer struct {
		Viewers          []restViewer `json:"viewers"`
		HeartbeatSeconds int          `json:"heartbeat_seconds"`
	}
	if err := c.send(http.MethodPut, path, map[string]any{"client_id": presenceClientID, "writing": args.Writing}, &answer); err != nil {
		return nil, err
	}
	viewers := viewersOut(answer.Viewers)
	out := map[string]any{"conversation_id": args.ConversationID, "action": "announce", "viewers": viewers, "listed_for_seconds": answer.HeartbeatSeconds}
	if hint := writingHint(viewers); hint != "" {
		out["hint"] = hint
	}
	return out, nil
}

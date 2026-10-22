package mcpserver

import (
	"errors"
	"net/http"
	"net/url"
)

func aiDraftGet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ConversationID string `json:"conversation_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = notFoundConversation(args.ConversationID)
	var answer struct {
		Draft         map[string]any `json:"draft"`
		Available     bool           `json:"available"`
		DraftsEnabled bool           `json:"drafts_enabled"`
	}
	if err := c.get("/api/v1/conversations/"+url.PathEscape(args.ConversationID)+"/ai-draft", nil, &answer); err != nil {
		return nil, err
	}
	return map[string]any{"conversation_id": args.ConversationID, "available": answer.Available, "drafts_enabled": answer.DraftsEnabled, "draft": answer.Draft}, nil
}

var aiDraftActions = map[string]string{"discard": "/discard", "apply_triage": "/apply-triage", "cancel_auto_answer": "/cancel-auto-answer"}

func aiDraftUpdate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ConversationID string `json:"conversation_id"`
		Action         string `json:"action"`
		DraftID        string `json:"draft_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = notFoundConversation(args.ConversationID)
	path := "/api/v1/conversations/" + url.PathEscape(args.ConversationID) + "/ai-draft"
	if args.Action != "request_new" {
		suffix, ok := aiDraftActions[args.Action]
		if !ok || args.DraftID == "" {
			return nil, errors.New("draft_id is required for " + args.Action + ": read it with colliedesk_ai_draft_get")
		}
		path += "/" + url.PathEscape(args.DraftID) + suffix
	}
	var answer struct {
		Draft map[string]any `json:"draft"`
	}
	if err := c.send(http.MethodPost, path, nil, &answer); err != nil {
		return nil, err
	}
	return map[string]any{"conversation_id": args.ConversationID, "action": args.Action, "draft": answer.Draft}, nil
}

func aiSettingsGet(c *call, _ map[string]any) (any, error) {
	var answer map[string]any
	if err := c.get("/api/v1/workspace/ai", nil, &answer); err != nil {
		return nil, err
	}
	return answer, nil
}

func aiSettingsUpdate(c *call, arguments map[string]any) (any, error) {
	var settings map[string]any
	if err := c.send(http.MethodPatch, "/api/v1/workspace/ai", arguments, &settings); err != nil {
		return nil, err
	}
	return map[string]any{"settings": settings}, nil
}

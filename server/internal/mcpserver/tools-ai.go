package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

const aiProviderNotFound = "No AI provider with that id in this workspace. List them with colliedesk_ai_settings_get."

func aiProviderPath(id, rest string) string {
	return "/api/v1/ai/providers/" + url.PathEscape(id) + rest
}

func aiSettingsGet(c *call, _ map[string]any) (any, error) {
	var settings json.RawMessage
	if err := c.get("/api/v1/ai", nil, &settings); err != nil {
		return nil, err
	}
	return map[string]any{"settings": settings}, nil
}

// aiProviderAnswer runs a change and returns the provider it answers.
func aiProviderAnswer(c *call, method, path string, body any) (any, error) {
	c.notFound = aiProviderNotFound
	var provider json.RawMessage
	if err := c.send(method, path, body, &provider); err != nil {
		return nil, err
	}
	return map[string]any{"provider": provider}, nil
}

func aiModelsList(c *call, arguments map[string]any) (any, error) {
	var list struct {
		Models []string `json:"models"`
	}
	if err := c.send(http.MethodPost, "/api/v1/ai/models", arguments, &list); err != nil {
		return nil, err
	}
	return map[string]any{"models": list.Models}, nil
}

func aiProviderAdd(c *call, arguments map[string]any) (any, error) {
	return aiProviderAnswer(c, http.MethodPost, "/api/v1/ai/providers", arguments)
}

// withoutProviderID is the tool's arguments as the route's body: the id goes in the path.
func withoutProviderID(arguments map[string]any) (string, map[string]any, error) {
	args, err := decodeArguments[struct {
		ID string `json:"provider_id"`
	}](map[string]any{"provider_id": arguments["provider_id"]})
	if err != nil {
		return "", nil, err
	}
	body := map[string]any{}
	for key, value := range arguments {
		if key != "provider_id" {
			body[key] = value
		}
	}
	return args.ID, body, nil
}

func aiProviderUpdate(c *call, arguments map[string]any) (any, error) {
	id, body, err := withoutProviderID(arguments)
	if err != nil {
		return nil, err
	}
	return aiProviderAnswer(c, http.MethodPatch, aiProviderPath(id, ""), body)
}

func aiProviderKeySet(c *call, arguments map[string]any) (any, error) {
	id, body, err := withoutProviderID(arguments)
	if err != nil {
		return nil, err
	}
	return aiProviderAnswer(c, http.MethodPut, aiProviderPath(id, "/key"), body)
}

func aiProviderCheck(c *call, arguments map[string]any) (any, error) {
	id, _, err := withoutProviderID(arguments)
	if err != nil {
		return nil, err
	}
	return aiProviderAnswer(c, http.MethodPost, aiProviderPath(id, "/check"), nil)
}

func aiProviderRemove(c *call, arguments map[string]any) (any, error) {
	id, _, err := withoutProviderID(arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = aiProviderNotFound
	var removed json.RawMessage
	if err := c.send(http.MethodDelete, aiProviderPath(id, ""), nil, &removed); err != nil {
		return nil, err
	}
	return removed, nil
}

func aiSlotSetRequest(c *call, arguments map[string]any) (any, error) {
	slot, _ := arguments["slot"].(string)
	body := map[string]any{}
	for key, value := range arguments {
		if key != "slot" {
			body[key] = value
		}
	}
	return ask(c, http.MethodPut, "/api/v1/ai/slots/"+url.PathEscape(slot), body)
}

func aiUsageGet(c *call, arguments map[string]any) (any, error) {
	query := url.Values{}
	if month, _ := arguments["month"].(string); month != "" {
		query.Set("month", month)
	}
	var usage json.RawMessage
	if err := c.get("/api/v1/ai/usage", query, &usage); err != nil {
		return nil, err
	}
	return map[string]any{"usage": usage}, nil
}

func aiAutomaticLimitsSet(c *call, arguments map[string]any) (any, error) {
	var limits json.RawMessage
	if err := c.send(http.MethodPut, "/api/v1/ai/automatic-limits", arguments, &limits); err != nil {
		return nil, err
	}
	return map[string]any{"automatic": limits}, nil
}

const aiDraftNotFound = "No AI draft with that id on this conversation. List them with colliedesk_ai_drafts_list."

type aiDraftArguments struct {
	ConversationID string `json:"conversation_id"`
	DraftID        string `json:"draft_id"`
	Prompt         string `json:"prompt"`
	Wait           int    `json:"wait"`
}

func aiDraftsPath(conversationID, rest string) string {
	return "/api/v1/conversations/" + url.PathEscape(conversationID) + "/ai-drafts" + rest
}

func aiDraftCreate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[aiDraftArguments](arguments)
	if err != nil {
		return nil, err
	}
	var draft json.RawMessage
	if err := c.send(http.MethodPost, aiDraftsPath(args.ConversationID, ""), map[string]any{"prompt": args.Prompt}, &draft); err != nil {
		return nil, err
	}
	return map[string]any{"draft": draft}, nil
}

func aiDraftGet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[aiDraftArguments](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = aiDraftNotFound
	query := url.Values{}
	if args.Wait > 0 {
		query.Set("wait", strconv.Itoa(min(args.Wait, 20)))
	}
	var draft json.RawMessage
	if err := c.get(aiDraftsPath(args.ConversationID, "/"+url.PathEscape(args.DraftID)), query, &draft); err != nil {
		return nil, err
	}
	return map[string]any{"draft": draft}, nil
}

func aiDraftsList(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[aiDraftArguments](arguments)
	if err != nil {
		return nil, err
	}
	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := c.get(aiDraftsPath(args.ConversationID, ""), nil, &list); err != nil {
		return nil, err
	}
	if list.Items == nil {
		list.Items = []json.RawMessage{}
	}
	return map[string]any{"drafts": list.Items}, nil
}

func aiDraftDiscard(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[aiDraftArguments](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = aiDraftNotFound
	var draft json.RawMessage
	if err := c.send(http.MethodPost, aiDraftsPath(args.ConversationID, "/"+url.PathEscape(args.DraftID)+"/discard"), nil, &draft); err != nil {
		return nil, err
	}
	return map[string]any{"draft": draft}, nil
}

const aiTriageNotFound = "No AI triage suggestion with that id on this conversation. Read it with colliedesk_ai_triage_get."

type aiTriageArguments struct {
	ConversationID  string   `json:"conversation_id"`
	SuggestionID    string   `json:"suggestion_id"`
	TagIDs          []string `json:"tag_ids"`
	Priority        *string  `json:"priority"`
	AssigneeAgentID *string  `json:"assignee_agent_id"`
	Parts           []string `json:"parts"`
}

func aiTriagePath(conversationID, rest string) string {
	return "/api/v1/conversations/" + url.PathEscape(conversationID) + "/ai-triage" + rest
}

func aiTriageGet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[aiTriageArguments](arguments)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Suggestion  json.RawMessage `json:"suggestion"`
		AutoApplied json.RawMessage `json:"auto_applied"`
	}
	if err := c.get(aiTriagePath(args.ConversationID, ""), nil, &answer); err != nil {
		return nil, err
	}
	return map[string]any{"suggestion": answer.Suggestion, "auto_applied": answer.AutoApplied}, nil
}

func aiTriageCreate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[aiTriageArguments](arguments)
	if err != nil {
		return nil, err
	}
	var suggestion json.RawMessage
	if err := c.send(http.MethodPost, aiTriagePath(args.ConversationID, ""), nil, &suggestion); err != nil {
		return nil, err
	}
	return map[string]any{"suggestion": suggestion}, nil
}

func aiTriageAccept(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[aiTriageArguments](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = aiTriageNotFound
	var body any
	if args.TagIDs != nil || args.Priority != nil || args.AssigneeAgentID != nil {
		tags := args.TagIDs
		if tags == nil {
			tags = []string{}
		}
		body = map[string]any{"tag_ids": tags, "priority": args.Priority, "assignee_agent_id": args.AssigneeAgentID}
	}
	var suggestion json.RawMessage
	if err := c.send(http.MethodPost, aiTriagePath(args.ConversationID, "/"+url.PathEscape(args.SuggestionID)+"/accept"), body, &suggestion); err != nil {
		return nil, err
	}
	return map[string]any{"suggestion": suggestion}, nil
}

func aiTriageUndo(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[aiTriageArguments](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = aiTriageNotFound
	var body any
	if len(args.Parts) > 0 {
		body = map[string]any{"parts": args.Parts}
	}
	var suggestion json.RawMessage
	if err := c.send(http.MethodPost, aiTriagePath(args.ConversationID, "/"+url.PathEscape(args.SuggestionID)+"/undo"), body, &suggestion); err != nil {
		return nil, err
	}
	return map[string]any{"suggestion": suggestion}, nil
}

func aiTriageDismiss(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[aiTriageArguments](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = aiTriageNotFound
	var suggestion json.RawMessage
	if err := c.send(http.MethodPost, aiTriagePath(args.ConversationID, "/"+url.PathEscape(args.SuggestionID)+"/dismiss"), nil, &suggestion); err != nil {
		return nil, err
	}
	return map[string]any{"suggestion": suggestion}, nil
}

const aiChatNotFound = "No chat with that id in this workspace. List them with colliedesk_ai_chat_sessions_list."

func aiChatUsageGet(c *call, arguments map[string]any) (any, error) {
	query := url.Values{}
	if month, _ := arguments["month"].(string); month != "" {
		query.Set("month", month)
	}
	var usage json.RawMessage
	if err := c.get("/api/v1/ai/chat/usage", query, &usage); err != nil {
		return nil, err
	}
	return map[string]any{"usage": usage}, nil
}

func aiChatSessionsList(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ChannelID      string `json:"channel_id"`
		Status         string `json:"status"`
		OnlyNotHelpful bool   `json:"only_not_helpful"`
		ConversationID string `json:"conversation_id"`
		Cursor         string `json:"cursor"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	for key, value := range map[string]string{"channel_id": args.ChannelID, "status": args.Status, "conversation_id": args.ConversationID, "cursor": args.Cursor} {
		if value != "" {
			query.Set(key, value)
		}
	}
	if args.OnlyNotHelpful {
		query.Set("feedback", "not_helpful")
	}
	var page json.RawMessage
	if err := c.get("/api/v1/ai/chat/sessions", query, &page); err != nil {
		return nil, err
	}
	return page, nil
}

func aiChatPath(arguments map[string]any) (string, error) {
	args, err := decodeArguments[struct {
		ID string `json:"chat_id"`
	}](arguments)
	if err != nil {
		return "", err
	}
	return "/api/v1/ai/chat/sessions/" + url.PathEscape(args.ID), nil
}

func aiChatSessionGet(c *call, arguments map[string]any) (any, error) {
	path, err := aiChatPath(arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = aiChatNotFound
	var chat json.RawMessage
	if err := c.get(path, nil, &chat); err != nil {
		return nil, err
	}
	return map[string]any{"chat": chat}, nil
}

func aiChatSessionDeleteRequest(c *call, arguments map[string]any) (any, error) {
	path, err := aiChatPath(arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = aiChatNotFound
	return ask(c, http.MethodDelete, path, nil)
}

func aiChatPreview(c *call, arguments map[string]any) (any, error) {
	var answer json.RawMessage
	if err := c.send(http.MethodPost, "/api/v1/ai/chat/preview", arguments, &answer); err != nil {
		return nil, err
	}
	return answer, nil
}

const channelNotFound = "No channel of this workspace has that id. List them with colliedesk_channels_list."

func channelAIGet(c *call, arguments map[string]any) (any, error) {
	id, _ := arguments["channel_id"].(string)
	c.notFound = channelNotFound
	var levels json.RawMessage
	if err := c.get("/api/v1/channels/"+url.PathEscape(id)+"/ai", nil, &levels); err != nil {
		return nil, err
	}
	return map[string]any{"channel_ai": levels}, nil
}

func channelAISet(c *call, arguments map[string]any) (any, error) {
	id, _ := arguments["channel_id"].(string)
	slot, _ := arguments["slot"].(string)
	body := map[string]any{}
	for key, value := range arguments {
		if key != "channel_id" && key != "slot" {
			body[key] = value
		}
	}
	c.notFound = channelNotFound
	var levels json.RawMessage
	if err := c.send(http.MethodPut, "/api/v1/channels/"+url.PathEscape(id)+"/ai/"+url.PathEscape(slot), body, &levels); err != nil {
		return nil, err
	}
	return map[string]any{"channel_ai": levels}, nil
}

func aiSourceConsentRequest(c *call, arguments map[string]any) (any, error) {
	class, _ := arguments["source_class"].(string)
	return ask(c, http.MethodPut, "/api/v1/ai/source-consents/"+url.PathEscape(class), nil)
}

func aiSourceConsentRemove(c *call, arguments map[string]any) (any, error) {
	class, _ := arguments["source_class"].(string)
	var removed json.RawMessage
	if err := c.send(http.MethodDelete, "/api/v1/ai/source-consents/"+url.PathEscape(class), nil, &removed); err != nil {
		return nil, err
	}
	return removed, nil
}

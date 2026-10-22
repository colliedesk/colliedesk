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

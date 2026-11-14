package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/url"
)

type restMacro struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	Body         string          `json:"body"`
	Scope        string          `json:"scope"`
	OwnerAgentID *string         `json:"owner_agent_id"`
	Actions      json.RawMessage `json:"actions"`
	UpdatedAt    string          `json:"updated_at"`
	PurgesAt     *string         `json:"purges_at"`
}

type restMacroResult struct {
	Application struct {
		ID       string   `json:"id"`
		Title    string   `json:"title"`
		Body     string   `json:"body"`
		Restored []string `json:"restored"`
		Kept     []string `json:"kept"`
	} `json:"application"`
	Conversation struct {
		ID          string  `json:"id"`
		Subject     string  `json:"subject"`
		ContactName string  `json:"contact_name"`
		Status      string  `json:"status"`
		Priority    string  `json:"priority"`
		AssigneeID  *string `json:"assignee_id"`
		Tags        []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"tags"`
	} `json:"conversation"`
}

func (c *call) macroOut(macro restMacro) (map[string]any, error) {
	me, err := c.me()
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"id": macro.ID, "title": macro.Title, "body": macro.Body, "scope": macro.Scope, "actions": json.RawMessage(`{}`),
		"updated_at": macro.UpdatedAt, "mine": macro.OwnerAgentID != nil && *macro.OwnerAgentID == me.Agent.ID,
		"team_text": map[string]string{"title": macro.Title, "body": macro.Body},
	}
	if len(macro.Actions) > 0 {
		out["actions"] = macro.Actions
	}
	if macro.PurgesAt != nil {
		out["purges_at"] = *macro.PurgesAt
	}
	return out, nil
}

func macrosList(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Trash bool `json:"trash"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	path := "/api/v1/macros"
	if args.Trash {
		path += "/trash"
	}
	var list struct {
		Items []restMacro `json:"items"`
	}
	if err := c.get(path, nil, &list); err != nil {
		return nil, err
	}
	out := make([]any, 0, len(list.Items))
	for _, macro := range list.Items {
		item, err := c.macroOut(macro)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return map[string]any{"macros": out}, nil
}

func macroSave(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string          `json:"macro_id"`
		UpdatedAt string          `json:"updated_at"`
		Title     string          `json:"title"`
		Body      string          `json:"body"`
		Scope     string          `json:"scope"`
		Actions   json.RawMessage `json:"actions"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"title": args.Title, "body": args.Body, "scope": args.Scope}
	if len(args.Actions) > 0 {
		body["actions"] = args.Actions
	}
	var macro restMacro
	if args.ID == "" {
		c.notFound = "No macro with that id in this workspace. List them with colliedesk_macros_list."
		if err := c.send(http.MethodPost, "/api/v1/macros", body, &macro); err != nil {
			return nil, err
		}
	} else {
		if args.UpdatedAt == "" {
			return nil, &toolError{"Pass updated_at from colliedesk_macros_list. If someone saved the macro since, read it again."}
		}
		body["updated_at"] = args.UpdatedAt
		c.notFound = "No macro " + args.ID + " that this person can edit. List them with colliedesk_macros_list."
		if err := c.send(http.MethodPatch, "/api/v1/macros/"+url.PathEscape(args.ID), body, &macro); err != nil {
			return nil, err
		}
	}
	out, err := c.macroOut(macro)
	if err != nil {
		return nil, err
	}
	return map[string]any{"macro": out}, nil
}

func macroDelete(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string `json:"macro_id"`
		UpdatedAt string `json:"updated_at"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No macro " + args.ID + " that this person can delete. List them with colliedesk_macros_list."
	var macro restMacro
	path := "/api/v1/macros/" + url.PathEscape(args.ID) + "?updated_at=" + url.QueryEscape(args.UpdatedAt)
	if err := c.send(http.MethodDelete, path, nil, &macro); err != nil {
		return nil, err
	}
	out := map[string]any{"macro_id": macro.ID, "title": macro.Title, "in_trash": true}
	if macro.PurgesAt != nil {
		out["purges_at"] = *macro.PurgesAt
	}
	return out, nil
}

func macroRestore(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID string `json:"macro_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No macro " + args.ID + " in the trash. List it with colliedesk_macros_list (trash true)."
	var macro restMacro
	if err := c.send(http.MethodPost, "/api/v1/macros/"+url.PathEscape(args.ID)+"/restore", nil, &macro); err != nil {
		return nil, err
	}
	out, err := c.macroOut(macro)
	if err != nil {
		return nil, err
	}
	return map[string]any{"macro": out}, nil
}

func macroApply(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ConversationID string `json:"conversation_id"`
		MacroID        string `json:"macro_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No conversation " + args.ConversationID + " or no macro " + args.MacroID + " this person can apply. List macros with colliedesk_macros_list."
	var result restMacroResult
	path := "/api/v1/conversations/" + url.PathEscape(args.ConversationID) + "/macros/" + url.PathEscape(args.MacroID) + "/apply"
	if err := c.send(http.MethodPost, path, nil, &result); err != nil {
		return nil, err
	}
	return map[string]any{
		"application_id": result.Application.ID, "title": result.Application.Title, "body": result.Application.Body,
		"conversation": macroConversation(result),
	}, nil
}

func macroUndo(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ConversationID string `json:"conversation_id"`
		ApplicationID  string `json:"application_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No macro application " + args.ApplicationID + " on conversation " + args.ConversationID + "."
	var result restMacroResult
	path := "/api/v1/conversations/" + url.PathEscape(args.ConversationID) + "/macro-applications/" + url.PathEscape(args.ApplicationID) + "/undo"
	if err := c.send(http.MethodPost, path, nil, &result); err != nil {
		return nil, err
	}
	restored, kept := result.Application.Restored, result.Application.Kept
	if restored == nil {
		restored = []string{}
	}
	if kept == nil {
		kept = []string{}
	}
	return map[string]any{
		"application_id": result.Application.ID, "title": result.Application.Title, "restored": restored, "kept": kept,
		"conversation": macroConversation(result),
	}, nil
}

func macroConversation(result restMacroResult) map[string]any {
	tags := make([]any, 0, len(result.Conversation.Tags))
	for _, tag := range result.Conversation.Tags {
		tags = append(tags, map[string]any{"id": tag.ID, "name": tag.Name})
	}
	return map[string]any{
		"id": result.Conversation.ID, "subject": result.Conversation.Subject, "contact_name": result.Conversation.ContactName,
		"status": result.Conversation.Status, "priority": result.Conversation.Priority, "assignee_id": result.Conversation.AssigneeID, "tags": tags,
	}
}

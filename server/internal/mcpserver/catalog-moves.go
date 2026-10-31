package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/url"
)

// Moving customer messages between conversations: what a move returns names messages and conversations
// by id only, never what anyone wrote.

const (
	messageMoveSchema = `{"type":"object","required":["id","message_ids","from_conversation_id","to_conversation_id","undoable"],"properties":{` +
		`"id":{"type":"string"},"message_ids":{"type":"array","items":{"type":"string"}},"from_conversation_id":{"type":"string"},` +
		`"to_conversation_id":{"type":["string","null"],"description":"Null for a move back to the intake queue."},` +
		`"created_conversation":{"type":"boolean","description":"The move opened to_conversation_id (a split)."},` +
		`"closed_source":{"type":"boolean","description":"The move took every customer message of the source and closed it."},` +
		`"gathering_id":{"type":["string","null"],"description":"The intake card a move to the queue made."},"decision_id":{"type":["string","null"]},` +
		`"moved_by_agent_id":{"type":["string","null"]},"via_token":{"type":"boolean"},"moved_at":{"type":"string"},"undone_at":{"type":["string","null"]},` +
		`"undone_by_agent_id":{"type":["string","null"]},"undoable":{"type":"boolean"}}}`
	moveNotFound = "No conversation or move with that id in this workspace. Read a conversation's messages with colliedesk_conversation_get."
)

func messagesMoveSpec() Spec {
	return Spec{
		Name: "colliedesk_messages_move", Title: "Move customer messages",
		Description: `Move customer messages (with their files) of a conversation: to another conversation of the workspace (to.conversation_id), ` +
			`into a new conversation (to.new.subject, a split), or, for a chat channel's group or chat, back to the intake queue (to.queue: true; email ` +
			`and web forms have none). Replies and notes of the team stay. A resolved or closed conversation that gets them reopens. close_source with ` +
			`to.conversation_id moves every customer message and closes the conversation as no new request, refused once someone replied, wrote a note or ` +
			`assigned it. Nothing is sent to the customer. warnings: other_contact (replies go to the target's customer, not the writer), other_channel. ` +
			`Undo with colliedesk_message_move_undo.` + "\n\nTier: draft.",
		Tier: "draft",
		REST: []string{"POST /api/v1/conversations/{id}/messages/move"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id","message_ids","to"],"properties":{` +
			`"conversation_id":{"type":"string",` + uuid + `,"description":"The conversation the messages are in."},` +
			`"message_ids":{"type":"array","minItems":1,"maxItems":100,"uniqueItems":true,"items":{"type":"string",` + uuid + `}},` +
			`"to":{"type":"object","additionalProperties":false,"description":"Exactly one of conversation_id, new or queue.","properties":{` +
			`"conversation_id":{"type":"string",` + uuid + `},"new":{"type":"object","additionalProperties":false,"required":["subject"],` +
			`"properties":{"subject":{"type":"string","minLength":1,"maxLength":200}}},"queue":{"type":"boolean","enum":[true]}}},` +
			`"close_source":{"type":"boolean"}}}`,
		Output: `{"type":"object","properties":{"move":` + messageMoveSchema + `,"warnings":{"type":"array","items":{"type":"string","enum":["other_contact","other_channel"]}},` +
			`"agent_request":` + filedRequestSchema + `,"next":{"type":"string"}},"anyOf":[{"required":["move","warnings"]},{"required":["agent_request","next"]}]}`,
		run: messagesMove,
	}
}

func messageMovesListSpec() Spec {
	return Spec{
		Name: "colliedesk_message_moves_list", Title: "List moves of messages",
		Description: `List the newest 100 moves of customer messages out of or into a conversation, newest first, each with whether it can still be undone.` +
			"\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/conversations/{id}/message-moves"},
		Input:  `{"type":"object","additionalProperties":false,"required":["conversation_id"],"properties":{"conversation_id":{"type":"string",` + uuid + `}}}`,
		Output: `{"type":"object","required":["items"],"properties":{"items":{"type":"array","items":` + messageMoveSchema + `}}}`,
		run:    messageMovesList,
	}
}

func messageMoveUndoSpec() Spec {
	return Spec{
		Name: "colliedesk_message_move_undo", Title: "Undo a move of messages",
		Description: `Put the messages of a move back where they were, with the statuses it changed while nobody changed them since. Undoing it again changes ` +
			`nothing. Only the newest move of a message is undone (move_superseded); a move to the intake queue whose card was decided since is not (move_decided).` +
			"\n\nTier: draft.",
		Tier: "draft", Idempotent: true,
		REST:   []string{"POST /api/v1/message-moves/{id}/undo"},
		Input:  `{"type":"object","additionalProperties":false,"required":["move_id"],"properties":{"move_id":{"type":"string",` + uuid + `,"description":"From colliedesk_message_moves_list."}}}`,
		Output: messageMoveSchema,
		run:    messageMoveUndo,
	}
}

func messagesMove(c *call, arguments map[string]any) (any, error) {
	id, _ := arguments["conversation_id"].(string)
	body := map[string]any{}
	for key, value := range arguments {
		if key != "conversation_id" {
			body[key] = value
		}
	}
	c.notFound = moveNotFound
	var result json.RawMessage
	if err := c.send(http.MethodPost, "/api/v1/conversations/"+url.PathEscape(id)+"/messages/move", body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func messageMovesList(c *call, arguments map[string]any) (any, error) {
	id, _ := arguments["conversation_id"].(string)
	c.notFound = moveNotFound
	var page json.RawMessage
	if err := c.get("/api/v1/conversations/"+url.PathEscape(id)+"/message-moves", nil, &page); err != nil {
		return nil, err
	}
	return page, nil
}

func messageMoveUndo(c *call, arguments map[string]any) (any, error) {
	id, _ := arguments["move_id"].(string)
	c.notFound = moveNotFound
	var move json.RawMessage
	if err := c.send(http.MethodPost, "/api/v1/message-moves/"+url.PathEscape(id)+"/undo", nil, &move); err != nil {
		return nil, err
	}
	return move, nil
}

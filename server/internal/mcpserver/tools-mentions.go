package mcpserver

import (
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type mentionedOut struct {
	AgentID string  `json:"agent_id"`
	Name    string  `json:"name"`
	ByEmail bool    `json:"by_email"`
	ReadAt  *string `json:"read_at"`
}

func mentionedRows(message restMessage) []mentionedOut {
	rows := make([]mentionedOut, 0, len(message.Mentions))
	for _, mention := range message.Mentions {
		row := mentionedOut{AgentID: mention.AgentID, Name: mention.Name, ByEmail: mention.ByEmail}
		if mention.ReadAt != nil {
			read := utc(*mention.ReadAt)
			row.ReadAt = &read
		}
		rows = append(rows, row)
	}
	return rows
}

func noteMention(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ConversationID string   `json:"conversation_id"`
		NoteID         string   `json:"note_id"`
		Mentions       []string `json:"mentions"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No note " + args.NoteID + " on conversation " + args.ConversationID + " in this workspace. Read the conversation with colliedesk_conversation_get for its notes' ids."
	var note restMessage
	path := "/api/v1/conversations/" + url.PathEscape(args.ConversationID) + "/messages/" + url.PathEscape(args.NoteID) + "/mentions"
	if err := c.send(http.MethodPut, path, map[string]any{"mentions": args.Mentions}, &note); err != nil {
		return nil, err
	}
	return map[string]any{"conversation_id": args.ConversationID, "note_id": note.ID, "mentions": mentionedRows(note)}, nil
}

type mentionOut struct {
	ID             string            `json:"id"`
	ConversationID string            `json:"conversation_id"`
	NoteID         string            `json:"note_id"`
	DisplayRef     string            `json:"display_ref,omitempty"`
	AskedBy        string            `json:"asked_by"`
	ViaToken       string            `json:"via_token,omitempty"`
	Excerpt        string            `json:"excerpt"`
	CreatedAt      string            `json:"created_at"`
	ReadAt         string            `json:"read_at,omitempty"`
	Untrusted      map[string]string `json:"untrusted_customer_content"`
}

func mentionsList(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Status string `json:"status"`
		Limit  int    `json:"limit"`
		Before string `json:"before"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if args.Status != "" {
		query.Set("status", args.Status)
	}
	if args.Limit > 0 {
		query.Set("limit", strconv.Itoa(args.Limit))
	}
	if args.Before != "" {
		query.Set("before", args.Before)
	}
	var page struct {
		Items []struct {
			ID             string                  `json:"id"`
			ConversationID string                  `json:"conversation_id"`
			MessageID      string                  `json:"message_id"`
			Subject        string                  `json:"subject"`
			DisplayRef     string                  `json:"display_ref"`
			Excerpt        string                  `json:"excerpt"`
			AuthorName     *string                 `json:"author_name"`
			AgentToken     *struct{ Label string } `json:"agent_token"`
			CreatedAt      time.Time               `json:"created_at"`
			ReadAt         *time.Time              `json:"read_at"`
		} `json:"items"`
		Unread     int     `json:"unread"`
		Email      bool    `json:"email"`
		NextBefore *string `json:"next_before"`
	}
	if err := c.get("/api/v1/mentions", query, &page); err != nil {
		return nil, err
	}
	out := map[string]any{"unread": page.Unread, "email": page.Email}
	if page.NextBefore != nil {
		out["next_before"] = *page.NextBefore
	}
	mentions := make([]mentionOut, 0, len(page.Items))
	for _, item := range page.Items {
		row := mentionOut{ID: item.ID, ConversationID: item.ConversationID, NoteID: item.MessageID, DisplayRef: item.DisplayRef, AskedBy: "a former teammate",
			Excerpt: item.Excerpt, CreatedAt: utc(item.CreatedAt), Untrusted: map[string]string{"subject": item.Subject}}
		if item.AuthorName != nil {
			row.AskedBy = *item.AuthorName
		}
		if item.AgentToken != nil {
			row.ViaToken = item.AgentToken.Label
		}
		if item.ReadAt != nil {
			row.ReadAt = utc(*item.ReadAt)
		}
		mentions = append(mentions, row)
	}
	out["mentions"] = mentions
	return out, nil
}

func mentionsMarkRead(c *call, arguments map[string]any) (any, error) {
	c.notFound = "No such mention of yours in this workspace. Use ids from colliedesk_mentions_list, or a conversation id from colliedesk_inbox_search."
	var out map[string]any
	if err := c.send(http.MethodPut, "/api/v1/mentions/read", arguments, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func myMentionEmailSet(c *call, arguments map[string]any) (any, error) {
	var out map[string]any
	if err := c.send(http.MethodPatch, "/api/v1/agents/me/mention-email", arguments, &out); err != nil {
		return nil, err
	}
	return out, nil
}

package mcpserver

import (
	"fmt"
	"net/http"
)

func conversationsBulkUpdate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ConversationIDs []string `json:"conversation_ids"`
		Status          string   `json:"status"`
		Assignee        string   `json:"assignee"`
		AddTags         []string `json:"add_tags"`
		RemoveTags      []string `json:"remove_tags"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if args.Status == "" && args.Assignee == "" && len(args.AddTags) == 0 && len(args.RemoveTags) == 0 {
		return nil, &toolError{"Nothing to change. Give at least one of status, assignee, add_tags or remove_tags."}
	}
	c.notFound = "At least one of the conversations is not in this workspace, so nothing was changed. Use ids from colliedesk_inbox_search."
	changes := map[string]any{}
	if args.Status != "" {
		changes["status"] = args.Status
	}
	if args.Assignee != "" {
		assignee, err := c.resolveAssignee(args.Assignee)
		if err != nil {
			return nil, err
		}
		changes["assignee_id"] = assignee
	}
	add, err := c.resolveTags(args.AddTags)
	if err != nil {
		return nil, err
	}
	remove, err := c.resolveTags(args.RemoveTags)
	if err != nil {
		return nil, err
	}
	addIDs, removeIDs := make([]string, 0, len(add)), make([]string, 0, len(remove))
	for _, tag := range add {
		addIDs = append(addIDs, tag.ID)
	}
	for _, tag := range remove {
		for _, other := range add {
			if tag.ID == other.ID {
				return nil, &toolError{fmt.Sprintf("The tag %q is in both add_tags and remove_tags. Choose one.", tag.Name)}
			}
		}
		removeIDs = append(removeIDs, tag.ID)
	}
	if len(addIDs) > 0 {
		changes["add_tag_ids"] = addIDs
	}
	if len(removeIDs) > 0 {
		changes["remove_tag_ids"] = removeIDs
	}
	var result struct {
		Updated int `json:"updated"`
	}
	if err := c.send(http.MethodPatch, "/api/v1/conversations", map[string]any{"ids": args.ConversationIDs, "changes": changes}, &result); err != nil {
		return nil, err
	}
	return map[string]any{"updated": result.Updated, "conversation_ids": args.ConversationIDs}, nil
}

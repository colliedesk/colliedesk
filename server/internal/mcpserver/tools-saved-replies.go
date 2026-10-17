package mcpserver

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type restPerson struct {
	Name string `json:"name"`
}

type restSavedReplyChange struct {
	Version             int         `json:"version"`
	Kind                string      `json:"kind"`
	RestoredFromVersion *int        `json:"restored_from_version"`
	ChangedBy           *restPerson `json:"changed_by"`
	Token               *struct {
		Label string `json:"label"`
	} `json:"token"`
	AgentRequestID *string `json:"agent_request_id"`
	CreatedAt      string  `json:"created_at"`
}

// restSavedReply is a saved reply, a trashed one, or a version: the fields each answer has.
type restSavedReply struct {
	ID         string                `json:"id"`
	Title      string                `json:"title"`
	Body       string                `json:"body"`
	UpdatedAt  string                `json:"updated_at"`
	LastChange *restSavedReplyChange `json:"last_change"`
	DeletedAt  string                `json:"deleted_at"`
	DeletedBy  *restPerson           `json:"deleted_by"`
	PurgesAt   string                `json:"purges_at"`
}

type teamText struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type savedReplyChangeOut struct {
	Version             int     `json:"version"`
	Kind                string  `json:"kind"`
	RestoredFromVersion *int    `json:"restored_from_version"`
	By                  *string `json:"by"`
	ViaToken            *string `json:"via_token"`
	AgentRequestID      *string `json:"agent_request_id"`
	At                  string  `json:"at"`
}

type savedReplyOut struct {
	ID            string               `json:"id"`
	UpdatedAt     string               `json:"updated_at"`
	TeamText      teamText             `json:"team_text"`
	BodyTruncated *bool                `json:"body_truncated,omitempty"`
	LastChange    *savedReplyChangeOut `json:"last_change"`
	DeletedAt     string               `json:"deleted_at,omitempty"`
	DeletedBy     *string              `json:"deleted_by,omitempty"`
	PurgesAt      string               `json:"purges_at,omitempty"`
}

func changeOut(change *restSavedReplyChange) *savedReplyChangeOut {
	if change == nil {
		return nil
	}
	out := &savedReplyChangeOut{Version: change.Version, Kind: change.Kind, RestoredFromVersion: change.RestoredFromVersion, AgentRequestID: change.AgentRequestID, At: change.CreatedAt}
	if change.ChangedBy != nil {
		out.By = &change.ChangedBy.Name
	}
	if change.Token != nil {
		out.ViaToken = &change.Token.Label
	}
	return out
}

func savedReplyView(reply restSavedReply) savedReplyOut {
	out := savedReplyOut{ID: reply.ID, UpdatedAt: reply.UpdatedAt, TeamText: teamText{reply.Title, reply.Body}, LastChange: changeOut(reply.LastChange),
		DeletedAt: reply.DeletedAt, PurgesAt: reply.PurgesAt}
	if reply.DeletedBy != nil {
		out.DeletedBy = &reply.DeletedBy.Name
	}
	return out
}

func (c *call) savedReplyList(path string) ([]restSavedReply, error) {
	var page struct {
		Items []restSavedReply `json:"items"`
	}
	if err := c.get(path, nil, &page); err != nil {
		return nil, err
	}
	return page.Items, nil
}

// trashPage reads one page of the trash.
func (c *call) trashPage(query url.Values) ([]restSavedReply, *string, error) {
	var page struct {
		Items      []restSavedReply `json:"items"`
		NextCursor *string          `json:"next_cursor"`
	}
	if err := c.get("/api/v1/saved-replies/trash", query, &page); err != nil {
		return nil, nil, err
	}
	return page.Items, page.NextCursor, nil
}

// cutReplyBody shortens text to replyBodyLimit characters and says whether it did.
func cutReplyBody(text string) (string, bool) {
	runes := []rune(text)
	if len(runes) <= replyBodyLimit {
		return text, false
	}
	return string(runes[:replyBodyLimit]), true
}

func savedReplyPath(id string, rest ...string) string {
	return "/api/v1/saved-replies/" + url.PathEscape(id) + strings.Join(rest, "")
}

// forReply words a 404 for the saved reply the tool names.
func (c *call) forReply(id string) {
	c.notFound = "No saved reply " + id + " in this workspace. List them with colliedesk_saved_replies_list or colliedesk_saved_replies_trash."
}

func savedRepliesList(c *call, _ map[string]any) (any, error) {
	return listOut(c, "/api/v1/saved-replies")
}

func savedRepliesTrash(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Limit  int    `json:"limit"`
		Cursor string `json:"cursor"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if args.Limit > 0 {
		query.Set("limit", strconv.Itoa(args.Limit))
	}
	if args.Cursor != "" {
		query.Set("cursor", args.Cursor)
	}
	items, next, err := c.trashPage(query)
	if err != nil {
		return nil, err
	}
	out := make([]savedReplyOut, 0, len(items))
	for _, item := range items {
		view := savedReplyView(item)
		var truncated bool
		view.TeamText.Body, truncated = cutReplyBody(view.TeamText.Body)
		view.BodyTruncated = &truncated
		out = append(out, view)
	}
	return map[string]any{"saved_replies": out, "next_cursor": next}, nil
}

func listOut(c *call, path string) (any, error) {
	items, err := c.savedReplyList(path)
	if err != nil {
		return nil, err
	}
	out := make([]savedReplyOut, 0, len(items))
	for _, item := range items {
		out = append(out, savedReplyView(item))
	}
	return map[string]any{"saved_replies": out}, nil
}

func savedReplyGet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID    string `json:"saved_reply_id"`
		Title string `json:"title"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if (args.ID == "") == (args.Title == "") {
		return nil, &toolError{"Give saved_reply_id or title, not both."}
	}
	matches := func(reply restSavedReply) bool {
		return reply.ID == args.ID || (args.Title != "" && strings.EqualFold(reply.Title, strings.TrimSpace(args.Title)))
	}
	live, err := c.savedReplyList("/api/v1/saved-replies")
	if err != nil {
		return nil, err
	}
	for _, item := range live {
		if matches(item) {
			return map[string]any{"saved_reply": savedReplyView(item), "in_trash": false}, nil
		}
	}
	// The trash holds at most as many replies as the workspace limit, so a few pages cover it.
	query := url.Values{"limit": {"200"}}
	for {
		items, next, err := c.trashPage(query)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if matches(item) {
				return map[string]any{"saved_reply": savedReplyView(item), "in_trash": true, "purges_at": item.PurgesAt}, nil
			}
		}
		if next == nil {
			break
		}
		query.Set("cursor", *next)
	}
	if args.ID != "" {
		c.forReply(args.ID)
		return nil, &toolError{c.notFound}
	}
	return nil, &toolError{"No saved reply titled " + strconv.Quote(args.Title) + " in this workspace or its trash. List them with colliedesk_saved_replies_list."}
}

func savedReplyCreate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	var created restSavedReply
	if err := c.send(http.MethodPost, "/api/v1/saved-replies", map[string]any{"title": args.Title, "body": args.Body}, &created); err != nil {
		return nil, err
	}
	return map[string]any{"saved_reply": savedReplyView(created)}, nil
}

func savedReplyUpdate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string  `json:"saved_reply_id"`
		UpdatedAt string  `json:"updated_at"`
		Title     *string `json:"title"`
		Body      *string `json:"body"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if args.Title == nil && args.Body == nil {
		return nil, &toolError{"Give a new title, a new body, or both."}
	}
	c.forReply(args.ID)
	change := map[string]any{"updated_at": args.UpdatedAt}
	if args.Title != nil {
		change["title"] = *args.Title
	}
	if args.Body != nil {
		change["body"] = *args.Body
	}
	var updated restSavedReply
	if err := c.send(http.MethodPatch, savedReplyPath(args.ID), change, &updated); err != nil {
		return nil, err
	}
	return map[string]any{"saved_reply": savedReplyView(updated)}, nil
}

func savedReplyDelete(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string `json:"saved_reply_id"`
		UpdatedAt string `json:"updated_at"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forReply(args.ID)
	if err := c.send(http.MethodDelete, savedReplyPath(args.ID, "?updated_at=", url.QueryEscape(args.UpdatedAt)), nil, nil); err != nil {
		return nil, err
	}
	return map[string]any{"saved_reply_id": args.ID, "in_trash": true,
		"next": "The saved reply is in the trash for 30 days. Bring it back with colliedesk_saved_reply_undelete (saved_reply_id " + args.ID + ")."}, nil
}

func savedReplyUndelete(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string `json:"saved_reply_id"`
		UpdatedAt string `json:"updated_at"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No saved reply " + args.ID + " in the trash of this workspace. List the trash with colliedesk_saved_replies_trash."
	var restored restSavedReply
	if err := c.send(http.MethodPost, savedReplyPath(args.ID, "/restore"), map[string]any{"updated_at": args.UpdatedAt}, &restored); err != nil {
		return nil, err
	}
	return map[string]any{"saved_reply": savedReplyView(restored)}, nil
}

func savedReplyHistory(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID            string `json:"saved_reply_id"`
		Limit         int    `json:"limit"`
		BeforeVersion int    `json:"before_version"`
		Version       int    `json:"version"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forReply(args.ID)
	query := url.Values{}
	switch {
	case args.Version > 0:
		query.Set("limit", "1")
		query.Set("before_version", strconv.Itoa(args.Version+1))
	default:
		if args.Limit > 0 {
			query.Set("limit", strconv.Itoa(args.Limit))
		}
		if args.BeforeVersion > 0 {
			query.Set("before_version", strconv.Itoa(args.BeforeVersion))
		}
	}
	var page struct {
		Items []struct {
			restSavedReplyChange
			Title string `json:"title"`
			Body  string `json:"body"`
		} `json:"items"`
		NextBeforeVersion *int `json:"next_before_version"`
	}
	if err := c.get(savedReplyPath(args.ID, "/versions"), query, &page); err != nil {
		return nil, err
	}
	type versionOut struct {
		savedReplyChangeOut
		TeamText      teamText `json:"team_text"`
		BodyTruncated bool     `json:"body_truncated"`
	}
	versions := make([]versionOut, 0, len(page.Items))
	for _, item := range page.Items {
		body, truncated := item.Body, false
		if args.Version == 0 {
			body, truncated = cutReplyBody(body)
		}
		versions = append(versions, versionOut{*changeOut(&item.restSavedReplyChange), teamText{item.Title, body}, truncated})
	}
	if args.Version > 0 {
		if len(versions) == 0 || versions[0].Version != args.Version {
			return nil, &toolError{fmt.Sprintf("Saved reply %s has no version %d. See colliedesk_saved_reply_history.", args.ID, args.Version)}
		}
		page.NextBeforeVersion = nil
	}
	return map[string]any{"saved_reply_id": args.ID, "versions": versions, "next_before_version": page.NextBeforeVersion}, nil
}

func savedReplyVersionRestore(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID        string `json:"saved_reply_id"`
		Version   int    `json:"version"`
		UpdatedAt string `json:"updated_at"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No saved reply " + args.ID + " with version " + strconv.Itoa(args.Version) + " in this workspace (a reply in the trash must be undeleted first). See colliedesk_saved_reply_history."
	var restored restSavedReply
	if err := c.send(http.MethodPost, savedReplyPath(args.ID, "/versions/", strconv.Itoa(args.Version), "/restore"), map[string]any{"updated_at": args.UpdatedAt}, &restored); err != nil {
		return nil, err
	}
	return map[string]any{"saved_reply": savedReplyView(restored)}, nil
}

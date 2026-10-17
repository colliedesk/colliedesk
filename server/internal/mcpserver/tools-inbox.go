package mcpserver

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultInboxLimit = 20
	conciseMessages   = 20
	conciseBodyRunes  = 1500
)

type inboxArgs struct {
	Status   string `json:"status"`
	Assignee string `json:"assignee"`
	Tag      string `json:"tag"`
	Q        string `json:"q"`
	Limit    int    `json:"limit"`
	Cursor   string `json:"cursor"`
	Detail   string `json:"detail"`
}

type inboxOut struct {
	Conversations []conversationOut `json:"conversations"`
	Count         int               `json:"count"`
	NextCursor    *string           `json:"next_cursor"`
	Timezone      string            `json:"timezone"`
	Detail        string            `json:"detail"`
}

func inboxSearch(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[inboxArgs](arguments)
	if err != nil {
		return nil, err
	}
	if args.Limit == 0 {
		args.Limit = defaultInboxLimit
	}
	if args.Detail == "" {
		args.Detail = "concise"
	}
	fingerprint := searchFingerprint(args)
	page, err := pageOfCursor(args.Cursor, fingerprint)
	if err != nil {
		return nil, err
	}
	me, err := c.me()
	if err != nil {
		return nil, err
	}
	query := url.Values{"page": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(args.Limit)}}
	if args.Status != "" {
		query.Set("status", args.Status)
	}
	if args.Assignee != "" {
		query.Set("assignee", args.Assignee)
	}
	if args.Q != "" {
		query.Set("search", args.Q)
	}
	if args.Tag != "" {
		tag, err := c.tagByName(args.Tag)
		if err != nil {
			return nil, err
		}
		query.Set("tag", tag.ID)
	}
	var found struct {
		Items []restConversation `json:"items"`
	}
	if err := c.get("/api/v1/conversations", query, &found); err != nil {
		return nil, err
	}
	agents, err := c.agentsByID()
	if err != nil {
		return nil, err
	}
	contacts, err := c.contactsByID(contactIDs(found.Items))
	if err != nil {
		return nil, err
	}
	out := inboxOut{Conversations: make([]conversationOut, 0, len(found.Items)), Count: len(found.Items), Timezone: me.Workspace.Timezone, Detail: args.Detail}
	for _, conversation := range found.Items {
		var contact *restContact
		if known, ok := contacts[conversation.ContactID]; ok {
			contact = &known
		}
		out.Conversations = append(out.Conversations, conversationRow(conversation, contact, agents, args.Detail == "full"))
	}
	if len(found.Items) == args.Limit {
		next := cursorOf(page+1, fingerprint)
		out.NextCursor = &next
	}
	return out, nil
}

func contactIDs(items []restConversation) []string {
	seen := map[string]bool{}
	ids := []string{}
	for _, item := range items {
		if !seen[item.ContactID] {
			seen[item.ContactID] = true
			ids = append(ids, item.ContactID)
		}
	}
	return ids
}

// searchFingerprint ties a cursor to the search that made it, so a cursor reused with other
// filters is refused instead of returning a confusing page.
func searchFingerprint(args inboxArgs) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{args.Status, args.Assignee, strings.ToLower(args.Tag), args.Q, strconv.Itoa(args.Limit)}, "\x00")))
	return hex.EncodeToString(sum[:4])
}

func cursorOf(page int, fingerprint string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(page) + "." + fingerprint))
}

func pageOfCursor(cursor, fingerprint string) (int, error) {
	if cursor == "" {
		return 1, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	pageText, got, found := strings.Cut(string(decoded), ".")
	page, parseErr := strconv.Atoi(pageText)
	if err != nil || !found || parseErr != nil || page < 2 {
		return 0, &toolError{"That cursor is not valid. Pass the next_cursor of a previous colliedesk_inbox_search result unchanged."}
	}
	if got != fingerprint {
		return 0, &toolError{"That cursor belongs to a different search. Repeat the search that returned it with the same status, assignee, tag, q and limit, or start again without a cursor."}
	}
	return page, nil
}

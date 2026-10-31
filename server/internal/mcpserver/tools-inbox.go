package mcpserver

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultInboxLimit = 20
	conciseMessages   = 20
	conciseBodyRunes  = 1500
)

// stringList takes one value or a list, so "status":"open" keeps working next to "status":["open","pending"].
type stringList []string

func (l *stringList) UnmarshalJSON(data []byte) error {
	var one string
	if json.Unmarshal(data, &one) == nil {
		*l = nil
		if one != "" {
			*l = stringList{one}
		}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*l = many
	return nil
}

type inboxArgs struct {
	SavedViewID     string     `json:"saved_view_id"`
	Status          stringList `json:"status"`
	Priority        stringList `json:"priority"`
	Assignee        stringList `json:"assignee"`
	Tag             stringList `json:"tag"`
	TagMatch        string     `json:"tag_match"`
	ContactID       string     `json:"contact_id"`
	Ref             string     `json:"ref"`
	Organization    string     `json:"organization"`
	Awaiting        string     `json:"awaiting"`
	HasPendingDraft *bool      `json:"has_pending_draft"`
	DeliveryFailed  *bool      `json:"delivery_failed"`
	Source          string     `json:"source"`
	Channel         stringList `json:"channel"`
	ChannelID       stringList `json:"channel_id"`
	CreatedAfter    string     `json:"created_after"`
	CreatedBefore   string     `json:"created_before"`
	UpdatedAfter    string     `json:"updated_after"`
	UpdatedBefore   string     `json:"updated_before"`
	Sort            string     `json:"sort"`
	Q               string     `json:"q"`
	Result          string     `json:"result"`
	Limit           int        `json:"limit"`
	Cursor          string     `json:"cursor"`
	Detail          string     `json:"detail"`
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
	query, err := c.inboxQuery(args)
	if err != nil {
		return nil, err
	}
	query.Set("page", strconv.Itoa(page))
	query.Set("page_size", strconv.Itoa(args.Limit))
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

// inboxQuery is the query of GET /conversations: the saved view's filters first, then every argument
// given, which replaces the view's value of the same filter. Tags are names here and ids there.
func (c *call) inboxQuery(args inboxArgs) (url.Values, error) {
	query := url.Values{}
	if args.SavedViewID != "" {
		filters, err := c.savedViewFilters(args.SavedViewID)
		if err != nil {
			return nil, err
		}
		query = filters
	}
	set := func(key, value string) {
		if value != "" {
			query.Set(key, value)
		}
	}
	for key, values := range map[string]stringList{"status": args.Status, "priority": args.Priority, "assignee": args.Assignee, "channel": args.Channel, "channel_id": args.ChannelID} {
		set(key, strings.Join(values, ","))
	}
	if len(args.Tag) > 0 {
		ids := make([]string, 0, len(args.Tag))
		for _, name := range args.Tag {
			tag, err := c.tagByName(name)
			if err != nil {
				return nil, err
			}
			ids = append(ids, tag.ID)
		}
		set("tag", strings.Join(ids, ","))
	}
	for key, value := range map[string]string{"tag_match": args.TagMatch, "contact": args.ContactID, "ref": args.Ref, "organization": args.Organization,
		"awaiting": args.Awaiting, "source": args.Source, "created_after": args.CreatedAfter, "created_before": args.CreatedBefore,
		"updated_after": args.UpdatedAfter, "updated_before": args.UpdatedBefore, "sort": args.Sort, "search": args.Q, "result": args.Result} {
		set(key, value)
	}
	for key, value := range map[string]*bool{"has_pending_draft": args.HasPendingDraft, "delivery_failed": args.DeliveryFailed} {
		if value != nil {
			query.Set(key, strconv.FormatBool(*value))
		}
	}
	return query, nil
}

// savedViewFilters reads a live view the caller can see and turns its filters into query values.
func (c *call) savedViewFilters(id string) (url.Values, error) {
	views, err := c.savedViews(false)
	if err != nil {
		return nil, err
	}
	for _, view := range views {
		if !strings.EqualFold(view.ID, id) {
			continue
		}
		var filters map[string]any
		if err := json.Unmarshal(view.Filters, &filters); err != nil {
			return nil, fmt.Errorf("decode saved view filters: %w", err)
		}
		query := url.Values{}
		for key, value := range filters {
			switch typed := value.(type) {
			case []any:
				parts := make([]string, len(typed))
				for index, part := range typed {
					parts[index] = fmt.Sprint(part)
				}
				query.Set(key, strings.Join(parts, ","))
			default:
				query.Set(key, fmt.Sprint(typed))
			}
		}
		return query, nil
	}
	return nil, &toolError{"No saved view with that id that this token's person can see. List them with colliedesk_saved_views_list."}
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
// filters is refused instead of returning a confusing page. Every argument but the cursor and the
// detail counts.
func searchFingerprint(args inboxArgs) string {
	args.Cursor, args.Detail = "", ""
	tags := make(stringList, len(args.Tag))
	for index, name := range args.Tag {
		tags[index] = strings.ToLower(name)
	}
	args.Tag = tags
	encoded, _ := json.Marshal(args)
	sum := sha256.Sum256(encoded)
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
		return 0, &toolError{"That cursor belongs to a different search. Repeat the search that returned it with the same arguments, or start again without a cursor."}
	}
	return page, nil
}

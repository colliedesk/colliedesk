package mcpserver

import (
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type activityContent struct {
	Subject     string `json:"subject"`
	ContactName string `json:"contact_name"`
}

type activityOut struct {
	ID                       string          `json:"id"`
	CreatedAt                string          `json:"created_at,omitempty"`
	AssignedAt               string          `json:"assigned_at,omitempty"`
	UntrustedCustomerContent activityContent `json:"untrusted_customer_content"`
}

type activityUpdatedOut struct {
	ID                       string          `json:"id"`
	Assignee                 *personOut      `json:"assignee"`
	UpdatedAt                string          `json:"updated_at"`
	IncomingAt               *string         `json:"incoming_at"`
	UntrustedCustomerContent activityContent `json:"untrusted_customer_content"`
}

func inboxActivity(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Since string `json:"since"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if since := strings.TrimSpace(args.Since); since != "" {
		query.Set("since", since)
	}
	var activity restActivity
	if err := c.get("/api/v1/inbox/activity", query, &activity); err != nil {
		return nil, err
	}
	me, err := c.me()
	if err != nil {
		return nil, err
	}
	agents, err := c.agentsByID()
	if err != nil {
		return nil, err
	}
	created, assigned, updated := []activityOut{}, []activityOut{}, []activityUpdatedOut{}
	for _, item := range activity.NewConversations {
		created = append(created, activityOut{ID: item.ID, CreatedAt: utc(item.CreatedAt), UntrustedCustomerContent: activityContent{item.Subject, item.ContactName}})
	}
	for _, item := range activity.AssignedToMe {
		assigned = append(assigned, activityOut{ID: item.ID, AssignedAt: utc(item.AssignedAt), UntrustedCustomerContent: activityContent{item.Subject, item.ContactName}})
	}
	for _, item := range activity.Updated {
		row := activityUpdatedOut{ID: item.ID, Assignee: person(agents, item.AssigneeID), UpdatedAt: utc(item.UpdatedAt),
			UntrustedCustomerContent: activityContent{item.Subject, item.ContactName}}
		if item.IncomingAt != nil {
			at := utc(*item.IncomingAt)
			row.IncomingAt = &at
		}
		updated = append(updated, row)
	}
	// now keeps its fractions of a second: it is a cursor, not a time to read.
	return map[string]any{"now": activity.Now.UTC().Format(time.RFC3339Nano), "new_conversations": created, "assigned_to_me": assigned,
		"updated": updated, "timezone": me.Workspace.Timezone}, nil
}

type timeEntryOut struct {
	ID                       string     `json:"id"`
	Agent                    *personOut `json:"agent,omitempty"`
	Minutes                  int        `json:"minutes"`
	Billable                 bool       `json:"billable"`
	ExecutedAt               string     `json:"executed_at"`
	UntrustedCustomerContent struct {
		AgentName string `json:"agent_name,omitempty"`
		Note      string `json:"note,omitempty"`
	} `json:"untrusted_customer_content"`
}

func timeEntriesList(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ConversationID string `json:"conversation_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = notFoundConversation(args.ConversationID)
	var listed struct {
		Items []restTimeEntry `json:"items"`
	}
	if err := c.get("/api/v1/conversations/"+url.PathEscape(args.ConversationID)+"/time-entries", nil, &listed); err != nil {
		return nil, err
	}
	agents, err := c.agentsByID()
	if err != nil {
		return nil, err
	}
	entries, total := make([]timeEntryOut, 0, len(listed.Items)), 0
	for _, entry := range listed.Items {
		row := timeEntryOut{ID: entry.ID, Agent: person(agents, entry.AgentID), Minutes: entry.Minutes, Billable: entry.Billable, ExecutedAt: utc(entry.ExecutedAt)}
		row.UntrustedCustomerContent.AgentName, row.UntrustedCustomerContent.Note = entry.AgentName, entry.Note
		entries = append(entries, row)
		total += entry.Minutes
	}
	return map[string]any{"conversation_id": args.ConversationID, "entries": entries, "total_minutes": total}, nil
}

const (
	// attachmentReadBytes is the most of a file the text path keeps: enough for attachmentTextLimit characters.
	attachmentReadBytes = 128 << 10
	// attachmentImageLimit is the largest image a call shows; a call reads one byte more to tell a larger one.
	attachmentImageLimit      = 2 << 20
	attachmentImageLimitWords = "2 MiB"
	// attachmentTextLimit is the most text of a file a call returns.
	attachmentTextLimit      = 20000
	attachmentTextLimitWords = "20,000"
)

type attachmentOut struct {
	ID                       string `json:"id"`
	Size                     int64  `json:"size"`
	SHA256                   string `json:"sha256"`
	DetectedType             string `json:"detected_type"`
	IsText                   bool   `json:"is_text"`
	IsImage                  bool   `json:"is_image"`
	TextTruncated            bool   `json:"text_truncated"`
	Hint                     string `json:"hint,omitempty"`
	UntrustedCustomerContent struct {
		Filename string  `json:"filename"`
		Text     *string `json:"text,omitempty"`
	} `json:"untrusted_customer_content"`
}

func attachmentGet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		AttachmentID string `json:"attachment_id"`
		Image        *bool  `json:"image"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No attachment " + args.AttachmentID + " in this workspace. Use an attachment id from colliedesk_conversation_get."
	header, body, err := c.download("/api/v1/attachments/"+url.PathEscape(args.AttachmentID), attachmentImageLimit+1)
	if err != nil {
		return nil, err
	}
	out := attachmentOut{ID: args.AttachmentID, SHA256: header.Get("X-Content-SHA256")}
	out.Size, _ = strconv.ParseInt(header.Get("Content-Length"), 10, 64)
	if _, params, err := mime.ParseMediaType(header.Get("Content-Disposition")); err == nil {
		out.UntrustedCustomerContent.Filename = params["filename"]
	}
	// The type comes from the bytes, never from the name or the declared type: SVG is sniffed as text, so
	// it is never shown as an image.
	out.DetectedType, _, _ = strings.Cut(http.DetectContentType(body), ";")
	if shownImageTypes[out.DetectedType] {
		switch {
		case len(body) > attachmentImageLimit || int64(len(body)) < out.Size:
			out.Hint = "This image is larger than " + attachmentImageLimitWords + ", the most a call shows. Open it in the web app to see it."
		case args.Image != nil && !*args.Image:
			out.Hint = "image was false, so the picture is left out."
		default:
			out.IsImage = true
			return &imageResult{Data: body, MIMEType: out.DetectedType, Meta: out}, nil
		}
		return out, nil
	}
	if len(body) > attachmentReadBytes {
		body = body[:attachmentReadBytes]
	}
	partial := int64(len(body)) < out.Size
	if partial {
		body = withoutPartialRune(body)
	}
	if !plainText(body) {
		out.Hint = "This is not a plain text file. Open it in the web app to see it."
		return out, nil
	}
	text, wasCut := cut(string(body), attachmentTextLimit, "Open the file in the web app to read the rest")
	if partial && !wasCut {
		text, wasCut = text+"\n[cut: the file is longer. Open the file in the web app to read the rest]", true
	}
	out.IsText, out.TextTruncated, out.UntrustedCustomerContent.Text = true, wasCut, &text
	return out, nil
}

// shownImageTypes are the sniffed types a call shows as an image: the ones every MCP client renders.
var shownImageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// withoutPartialRune drops the bytes of a character that the read limit split.
func withoutPartialRune(data []byte) []byte {
	for index := 0; index < utf8.UTFMax && len(data) > 0 && !utf8.Valid(data); index++ {
		data = data[:len(data)-1]
	}
	return data
}

// plainText reports whether data is UTF-8 text without control characters other than line breaks and tabs.
func plainText(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	for _, char := range string(data) {
		if unicode.IsControl(char) && char != '\n' && char != '\r' && char != '\t' && char != '\f' {
			return false
		}
	}
	return true
}

type contactGetArgs struct {
	ContactID string `json:"contact_id"`
	Limit     int    `json:"limit"`
}

func contactGet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[contactGetArgs](arguments)
	if err != nil {
		return nil, err
	}
	if args.Limit == 0 {
		args.Limit = 20
	}
	c.notFound = "No contact " + args.ContactID + " in this workspace. Use an id from colliedesk_contacts_find."
	var detail restContactDetail
	if err := c.get("/api/v1/contacts/"+url.PathEscape(args.ContactID), nil, &detail); err != nil {
		return nil, err
	}
	me, err := c.me()
	if err != nil {
		return nil, err
	}
	agents, err := c.agentsByID()
	if err != nil {
		return nil, err
	}
	var page struct {
		Items []restConversation `json:"items"`
	}
	query := url.Values{"contact": {args.ContactID}, "page_size": {strconv.Itoa(args.Limit)}}
	if err := c.get("/api/v1/conversations", query, &page); err != nil {
		return nil, err
	}
	rows := make([]conversationOut, 0, len(page.Items))
	for _, conversation := range page.Items {
		rows = append(rows, conversationRow(conversation, &detail.restContact, agents, false))
	}
	merges := make([]contactMergeOut, 0, len(detail.Merges))
	for _, merge := range detail.Merges {
		merges = append(merges, contactMergeRow(merge))
	}
	return map[string]any{"contact": contactRow(detail.restContact), "conversations": rows,
		"conversations_total": detail.ConversationCount, "merged_into_id": detail.MergedIntoID, "merges": merges, "timezone": me.Workspace.Timezone}, nil
}

func contactRow(contact restContact) contactOutRow {
	return contactOutRow{ID: contact.ID, CreatedAt: utc(contact.CreatedAt),
		UntrustedCustomerContent: contactOut{Name: contact.Name, Email: contact.Email, Organization: contact.Organization}}
}

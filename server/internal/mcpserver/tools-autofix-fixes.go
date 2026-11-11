package mcpserver

import (
	"net/http"
	"net/url"
)

type fixSendArguments struct {
	ConversationID string   `json:"conversation_id"`
	FixID          string   `json:"fix_id"`
	RepoID         string   `json:"repo_id"`
	Connector      string   `json:"connector"`
	Title          string   `json:"title"`
	Body           string   `json:"body"`
	MessageIDs     []string `json:"message_ids"`
	NoteIDs        []string `json:"note_ids"`
	ClientKey      string   `json:"client_key"`
	URL            string   `json:"url"`
	RowVersion     int      `json:"row_version"`
}

func fixPath(id, rest string) string { return "/api/v1/autofix/fixes/" + url.PathEscape(id) + rest }

func fixPreview(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[fixSendArguments](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No conversation or connected repository with that id in this workspace."
	body := map[string]any{"repo_id": args.RepoID, "connector": args.Connector}
	if len(args.NoteIDs) > 0 {
		body["note_ids"] = args.NoteIDs
	}
	var preview map[string]any
	if err := c.send(http.MethodPost, "/api/v1/conversations/"+url.PathEscape(args.ConversationID)+"/fixes/preview", body, &preview); err != nil {
		return nil, err
	}
	// The title, body and note excerpts are built from what the customer and the team wrote.
	content := map[string]any{}
	for _, key := range []string{"title", "body", "notes"} {
		content[key] = preview[key]
		delete(preview, key)
	}
	preview["untrusted_customer_content"] = content
	return preview, nil
}

func fixSendBody(args fixSendArguments) map[string]any {
	body := map[string]any{"repo_id": args.RepoID, "connector": args.Connector, "title": args.Title, "body": args.Body, "client_key": args.ClientKey}
	if len(args.MessageIDs) > 0 {
		body["message_ids"] = args.MessageIDs
	}
	if len(args.NoteIDs) > 0 {
		body["note_ids"] = args.NoteIDs
	}
	return body
}

// fixSent answers a fix that was sent: the fix as the tools show it, and the GitHub page where the
// person behind the token confirms an issue connector's send.
func fixSent(c *call, path string, body map[string]any) (any, error) {
	var sent struct {
		Fix          map[string]any `json:"fix"`
		AuthorizeURL *string        `json:"authorize_url"`
	}
	if err := c.send(http.MethodPost, path, body, &sent); err != nil {
		return nil, err
	}
	out := map[string]any{"fix": fixForModel(sent.Fix), "authorize_url": sent.AuthorizeURL}
	if sent.AuthorizeURL != nil {
		out["next"] = forThePerson(c, "it opens GitHub, where they confirm within 10 minutes and the issue is created with their GitHub account. Follow the fix with colliedesk_fix_get.")
	} else {
		out["next"] = "The coding agent has it; follow the fix with colliedesk_fix_get."
	}
	return out, nil
}

func fixSend(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[fixSendArguments](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No conversation " + args.ConversationID + " in this workspace."
	return fixSent(c, "/api/v1/conversations/"+url.PathEscape(args.ConversationID)+"/fixes", fixSendBody(args))
}

func fixRetry(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[fixSendArguments](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = fixNotFound
	return fixSent(c, fixPath(args.FixID, "/retry"), fixSendBody(args))
}

// fixChange makes a person's change to a fix and answers it as the tools show a fix.
func fixChange(c *call, arguments map[string]any, method, rest string, withURL bool) (any, error) {
	args, err := decodeArguments[fixSendArguments](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = fixNotFound
	body := map[string]any{}
	if args.RowVersion > 0 {
		body["row_version"] = args.RowVersion
	}
	if withURL {
		body["url"] = args.URL
	}
	var payload any
	if len(body) > 0 {
		payload = body
	}
	var fix map[string]any
	if err := c.send(method, fixPath(args.FixID, rest), payload, &fix); err != nil {
		return nil, err
	}
	return map[string]any{"fix": fixForModel(fix)}, nil
}

func fixCancel(c *call, arguments map[string]any) (any, error) {
	return fixChange(c, arguments, http.MethodPost, "/cancel", false)
}

func fixMarkDeployed(c *call, arguments map[string]any) (any, error) {
	return fixChange(c, arguments, http.MethodPut, "/deployed", false)
}

func fixUnmarkDeployed(c *call, arguments map[string]any) (any, error) {
	return fixChange(c, arguments, http.MethodDelete, "/deployed", false)
}

func fixPRLink(c *call, arguments map[string]any) (any, error) {
	return fixChange(c, arguments, http.MethodPut, "/pull-request", true)
}

func fixPRUnlink(c *call, arguments map[string]any) (any, error) {
	return fixChange(c, arguments, http.MethodDelete, "/pull-request", false)
}

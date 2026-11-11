package mcpserver

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// passOn makes a change as the caller and answers what Colliedesk answered (nothing for a 204).
func passOn(c *call, method, path string, body any) (map[string]any, error) {
	var out map[string]any
	if err := c.send(method, path, body, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

// personName names the person this token acts as, for a step only they can take.
func personName(c *call) string {
	if me, err := c.me(); err == nil {
		return agentName(me.Agent)
	}
	return "the person this token acts as"
}

// forThePerson is the next step of an address only the token's person can finish, in their browser.
func forThePerson(c *call, what string) string {
	return fmt.Sprintf("Give this address to %s: %s It works only for them, in a browser signed in to Colliedesk, "+
		"so do not open it yourself and do not pass it to anyone else.", personName(c), what)
}

func replyDraft(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ConversationID string          `json:"conversation_id"`
		Body           string          `json:"body"`
		CC             []string        `json:"cc"`
		Files          []inlineFileArg `json:"files"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Body) == "" {
		return nil, &toolError{"The draft is empty. Write the reply text in body."}
	}
	files, err := decodeFiles(args.Files)
	if err != nil {
		return nil, err
	}
	c.notFound = notFoundConversation(args.ConversationID)
	path := "/api/v1/conversations/" + url.PathEscape(args.ConversationID) + "/reply-drafts"
	var draft struct {
		ID             string             `json:"id"`
		ConversationID string             `json:"conversation_id"`
		Status         string             `json:"status"`
		Files          []attachmentRowOut `json:"files"`
	}
	if len(files) > 0 {
		fields := [][2]string{{"text", args.Body}}
		for _, address := range args.CC {
			fields = append(fields, [2]string{"cc", address})
		}
		contentType, form := messageForm(fields, files)
		err = c.sendBytes(http.MethodPost, path, contentType, nil, form, &draft)
	} else {
		body := map[string]any{"text": args.Body}
		if len(args.CC) > 0 {
			body["cc"] = args.CC
		}
		err = c.send(http.MethodPost, path, body, &draft)
	}
	if err != nil {
		return nil, err
	}
	if draft.Files == nil {
		draft.Files = []attachmentRowOut{}
	}
	next := "Nothing reached the customer. A person sees the draft above the reply box and sends, edits or discards it."
	if len(draft.Files) > 0 {
		next += " Its files go with the reply only if the person keeps them."
	}
	return map[string]any{"draft": draft, "next": next}, nil
}

func replyDraftRestore(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[aiDraftArguments](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = aiDraftNotFound
	var draft json.RawMessage
	if err := c.send(http.MethodPost, aiDraftsPath(args.ConversationID, "/"+url.PathEscape(args.DraftID)+"/restore"), nil, &draft); err != nil {
		return nil, err
	}
	return map[string]any{"draft": draft,
		"next": "The draft waits above the reply box again, with its files; nothing reached the customer. A person sends, edits or discards it."}, nil
}

func workspaceExport(c *call, arguments map[string]any) (any, error) {
	if delivery, _ := arguments["delivery"].(string); delivery == "download" {
		header, body, err := c.download("/api/v1/export", inlineFilesLimit+1)
		if err != nil {
			return nil, err
		}
		if len(body) > inlineFilesLimit {
			return nil, &toolError{"The export is larger than " + inlineFilesLimitWords + ", too large for one answer. Call again with delivery link and give the link to the person you work for."}
		}
		filename := "colliedesk-export.zip"
		if _, params, err := mime.ParseMediaType(header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
			filename = params["filename"]
		}
		return map[string]any{"filename": filename, "size": len(body), "data_base64": base64.StdEncoding.EncodeToString(body),
			"next": "This is every conversation, message and contact of the workspace: keep it with the person you work for, never in a conversation, note or reply."}, nil
	}
	out, err := passOn(c, http.MethodPost, "/api/v1/export/link", nil)
	if err != nil {
		return nil, err
	}
	out["next"] = "url is a path on this Colliedesk: put the address the person opens Colliedesk at in front of it and give it to the person you work for. It downloads the whole workspace once, then stops working. Never paste it in a conversation, note or reply."
	return out, nil
}

func billingAction(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Action           string `json:"action"`
		Plan             string `json:"plan"`
		FreshdeskAddress string `json:"freshdesk_address"`
		Times            string `json:"times"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	switch args.Action {
	case "portal":
		out, err := passOn(c, http.MethodPost, "/api/v1/billing/portal", nil)
		if err != nil {
			return nil, err
		}
		out["next"] = forThePerson(c, "it opens the billing portal, where they change the plan, the card or download invoices.")
		return out, nil
	case "migration_request":
		if strings.TrimSpace(args.FreshdeskAddress) == "" {
			return nil, &toolError{"A migration request needs freshdesk_address, like yourcompany.freshdesk.com."}
		}
		if _, err := passOn(c, http.MethodPost, "/api/v1/billing/migration-request", map[string]any{"freshdesk_address": args.FreshdeskAddress, "times": args.Times}); err != nil {
			return nil, err
		}
		return map[string]any{"next": "Colliedesk support has the migration request and will write to " + personName(c) + "; colliedesk_billing_get shows when it was sent."}, nil
	}
	if args.Plan == "" {
		return nil, &toolError{"A checkout needs a plan: solo_monthly, solo_yearly, small_monthly, small_yearly, team_monthly, team_yearly, team25_monthly, team25_yearly or founding."}
	}
	out, err := passOn(c, http.MethodPost, "/api/v1/billing/checkout", map[string]any{"plan": args.Plan})
	if err != nil {
		return nil, err
	}
	out["next"] = forThePerson(c, "it opens the payment page for "+args.Plan+"; nothing is paid until they pay there.")
	return out, nil
}

// offerToken is the token of a trial offer link, or the argument itself when it is only the token.
func offerToken(link string) string {
	link = strings.TrimSpace(link)
	if parsed, err := url.Parse(link); err == nil && parsed.Query().Get("token") != "" {
		return parsed.Query().Get("token")
	}
	return link
}

func trialOfferAccept(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Link    string `json:"link"`
		Preview bool   `json:"preview"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	token := offerToken(args.Link)
	if token == "" {
		return nil, &toolError{"Give link: the trial offer link from the email, or only its token."}
	}
	if args.Preview {
		var offer map[string]any
		if err := c.get("/api/v1/trial-offer", url.Values{"token": {token}}, &offer); err != nil {
			return nil, err
		}
		next := "Nothing changed. To accept, call colliedesk_trial_offer_accept again without preview."
		if offer["can_accept"] != true {
			next = "This token cannot accept it now (refusal " + fmt.Sprint(offer["refusal"]) + ")."
			if names, _ := offer["accept_by"].([]any); len(names) > 0 && offer["refusal"] == "not_admin" {
				next += " These members can accept it: " + joinNames(names) + "."
			}
		}
		return map[string]any{"offer": offer, "next": next}, nil
	}
	out, err := passOn(c, http.MethodPost, "/api/v1/trial-offer/accept", map[string]any{"token": token})
	if err != nil {
		return nil, err
	}
	if out["extended"] == true {
		out["next"] = fmt.Sprintf("The trial now ends at %v; the workspace is open until then.", out["trial_ends_at"])
	} else {
		out["next"] = fmt.Sprintf("The trial already ended later, at %v, and was kept; the offer is spent.", out["trial_ends_at"])
	}
	return out, nil
}

func joinNames(names []any) string {
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprint(name))
	}
	return strings.Join(parts, ", ")
}

func emailChannelDisconnect(c *call, _ map[string]any) (any, error) {
	if _, err := passOn(c, http.MethodDelete, "/api/v1/channels/email", nil); err != nil {
		return nil, err
	}
	return map[string]any{"done": true, "next": "The default email channel is disconnected; its conversations stay. Connect it again in Settings → Channels or with colliedesk_channels_create."}, nil
}

func emailSecretRotate(c *call, arguments map[string]any) (any, error) {
	out, err := passOn(c, http.MethodPost, emailChannelPath(c, arguments, "/secret"), nil)
	if err != nil {
		return nil, err
	}
	out["next"] = "The old secret stopped working. Give the new one only to the person who updates the mail forwarder; never write it in a conversation, note or reply."
	return out, nil
}

func consentLink(c *call, arguments map[string]any, provider, suffix string) (any, error) {
	out, err := passOn(c, http.MethodPost, emailChannelPath(c, arguments, suffix), nil)
	if err != nil {
		return nil, err
	}
	out["next"] = forThePerson(c, "it opens "+provider+"'s page, where they sign in and agree within 10 minutes. colliedesk_email_channel_get then shows the connection.")
	return out, nil
}

func gmailConnect(c *call, arguments map[string]any) (any, error) {
	return consentLink(c, arguments, "Google", "/gmail")
}

func microsoftConnect(c *call, arguments map[string]any) (any, error) {
	return consentLink(c, arguments, "Microsoft", "/microsoft")
}

func mailboxDisconnect(c *call, arguments map[string]any, provider, suffix, again string) (any, error) {
	if _, err := passOn(c, http.MethodDelete, emailChannelPath(c, arguments, suffix), nil); err != nil {
		return nil, err
	}
	return map[string]any{"done": true, "next": provider + " is disconnected; replies go out the way they did before. Connect it again with " + again + "."}, nil
}

func gmailDisconnect(c *call, arguments map[string]any) (any, error) {
	return mailboxDisconnect(c, arguments, "Gmail", "/gmail", "colliedesk_gmail_connect")
}

func microsoftDisconnect(c *call, arguments map[string]any) (any, error) {
	return mailboxDisconnect(c, arguments, "Microsoft 365", "/microsoft", "colliedesk_microsoft_connect")
}

func numberSeriesSet(c *call, arguments map[string]any) (any, error) {
	series, _ := arguments["series"].(string)
	body := map[string]any{}
	for key, value := range arguments {
		if key != "series" {
			body[key] = value
		}
	}
	return passOn(c, http.MethodPut, "/api/v1/conversation-number-series/"+url.PathEscape(series), body)
}

func responseTargetsSet(c *call, arguments map[string]any) (any, error) {
	return passOn(c, http.MethodPut, "/api/v1/response-targets", arguments)
}

func workspaceSlugChange(c *call, arguments map[string]any) (any, error) {
	out, err := passOn(c, http.MethodPatch, "/api/v1/workspace/slug", arguments)
	if err != nil {
		return nil, err
	}
	delete(out, "continue_url")
	return map[string]any{"workspace": out}, nil
}

func numberSeriesList(c *call, _ map[string]any) (any, error) {
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err := c.get("/api/v1/conversation-number-series", nil, &page); err != nil {
		return nil, err
	}
	return map[string]any{"items": page.Items}, nil
}

func responseTargetsGet(c *call, _ map[string]any) (any, error) {
	var targets map[string]any
	if err := c.get("/api/v1/response-targets", nil, &targets); err != nil {
		return nil, err
	}
	return targets, nil
}

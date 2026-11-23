package mcpserver

import (
	"net/http"
	"net/url"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// kbReplySourceSchema is the sent reply an article was made from. The subject and the question are
// the customer's words, so they sit under untrusted_customer_content.
const kbReplySourceSchema = `{"type":"object","required":["conversation_id","display_ref","message_id","untrusted_customer_content"],"properties":{` +
	`"conversation_id":{"type":"string"},"display_ref":{"type":"string","description":"The conversation's code, as the team sees it."},"message_id":{"type":"string"},` +
	`"untrusted_customer_content":{"type":"object","required":["subject","question"],"properties":{"subject":{"type":"string"},` +
	`"question":{"type":"string","description":"The customer message just before the reply, masked; empty when there is none."}}}}}`

const kbArticleRequestOutput = `{"type":"object","required":["gap","created","added_to_existing"],"properties":{"gap":` + kbGapOutput + `,` +
	`"created":{"type":"boolean","description":"False when the conversation had asked for this already: nothing changed."},` +
	`"added_to_existing":{"type":"boolean","description":"True when the request joined a gap that was already open."}}}`

const kbConversationIDProperty = `"conversation_id":{"type":"string",` + uuid + `,"description":"The id from colliedesk_inbox_search."}`

const kbDraftCollectionProperty = `"collection_id":{"type":"string",` + uuid + `,"description":"A collection of the workspace (colliedesk_kb_collections_list). Default: the first one."}`

func kbArticleRequestSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_article_request",
		Title: "Ask for a help center article from a conversation",
		Description: `File the conversation's request for a help center article in Knowledge gaps, as a signal of kind requested linked to the conversation. ` +
			`It joins the open gap gap_id names, else an open gap whose title is close to title (added_to_existing), else it opens a gap; ` +
			`find the close ones first with colliedesk_kb_gaps_list and similar. title defaults to the conversation subject. ` +
			`The customer's names and company, email addresses and runs of 6 or more digits are masked before they reach the gap. ` +
			`A conversation asks once: the same words again change nothing (created false), other words move its request. Nobody is told and nothing is sent. ` +
			`Take it back with colliedesk_kb_article_request_withdraw. Also needs kb_articles:read.` + "\n\n" + untrustedNotice,
		REST: []string{"POST /api/v1/conversations/{id}/article-request"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id"],"properties":{` + kbConversationIDProperty + `,` +
			`"title":{"type":"string","minLength":1,"maxLength":300,"description":"What the article should answer. Default: the conversation subject."},` +
			`"note":{"type":"string","minLength":1,"maxLength":300,"description":"One line for the writer."},` +
			`"gap_id":{"type":"string",` + uuid + `,"description":"An open gap to add the conversation to, from colliedesk_kb_gaps_list with similar."}}}`,
		Output: kbArticleRequestOutput,
		run:    kbArticleRequest,
	}
}

func kbArticleRequestWithdrawSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_article_request_withdraw",
		Title: "Take back a conversation's request for an article",
		Description: `Take back the conversation's request for a help center article: its signal leaves the gap, and a gap with no signal left goes away. ` +
			`File it again with colliedesk_kb_article_request. Also needs kb_articles:read.`,
		REST:   []string{"DELETE /api/v1/conversations/{id}/article-request"},
		Input:  `{"type":"object","additionalProperties":false,"required":["conversation_id"],"properties":{` + kbConversationIDProperty + `}}`,
		Output: `{"type":"object","required":["conversation_id","withdrawn"],"properties":{"conversation_id":{"type":"string"},"withdrawn":{"type":"boolean"}}}`,
		run:    kbArticleRequestWithdraw,
	}
}

func kbGapArticleStartSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_gap_article_start",
		Title: "Start a draft article for a knowledge gap, without AI",
		Description: `Open an empty draft article titled as the knowledge gap, in collection_id or the first collection, with the up to 8 conversations that asked for it ` +
			`or keep asking its question as its sources (gap_source, empty for a token without conversations:read). No AI writes it and it is never published: ` +
			`write it with colliedesk_kb_article_update and publish it with colliedesk_kb_articles_publish. For an AI-written draft use colliedesk_kb_gap_draft.` + "\n\n" + untrustedNotice,
		REST:   []string{"POST /api/v1/kb/gaps/{id}/article"},
		Input:  `{"type":"object","additionalProperties":false,"required":["gap_id"],"properties":{` + kbGapIDProperty + `,` + kbDraftCollectionProperty + `}}`,
		Output: kbGapDraftOutput,
		run:    kbGapArticleStart,
	}
}

func kbArticleFromReplySpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_article_from_reply",
		Title: "Make a draft article from a reply the team sent",
		Description: `Keep a reply the team sent as a draft help center article in collection_id or the first collection: the reply is its body, the conversation subject its title, ` +
			`and the customer question just before the reply sits beside it (reply_source). Email addresses, runs of 6 or more digits and the customer's and team's names are masked. ` +
			`A note or a notice is refused. It is never published: edit it with colliedesk_kb_article_update. Also needs conversations:read.` + "\n\n" + untrustedNotice,
		REST: []string{"POST /api/v1/conversations/{id}/messages/{messageId}/article"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id","message_id"],"properties":{` + kbConversationIDProperty + `,` +
			`"message_id":{"type":"string",` + uuid + `,"description":"A reply of the conversation (kind outgoing), from colliedesk_conversation_get."},` + kbDraftCollectionProperty + `}}`,
		Output: `{"type":"object","required":["article","reply_source"],"properties":{"article":` + kbArticleSchema + `,"reply_source":` + kbReplySourceSchema + `}}`,
		run:    kbArticleFromReply,
	}
}

func kbArticleRequest(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID    string `json:"conversation_id"`
		Title string `json:"title"`
		Note  string `json:"note"`
		GapID string `json:"gap_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	for name, value := range map[string]string{"title": args.Title, "note": args.Note, "gap_id": args.GapID} {
		if value != "" {
			body[name] = value
		}
	}
	c.notFound = "Conversation " + args.ID + " not found in this workspace, or gap_id is not an open gap. Find them with colliedesk_inbox_search and colliedesk_kb_gaps_list."
	var filed domain.KBArticleRequest
	if err := c.send(http.MethodPost, kbArticleRequestPath(args.ID), body, &filed); err != nil {
		return nil, err
	}
	return map[string]any{"gap": kbGapAnswer(filed.Gap), "created": filed.Created, "added_to_existing": filed.AddedToExisting}, nil
}

func kbArticleRequestWithdraw(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID string `json:"conversation_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "Conversation " + args.ID + " has not asked for an article, or is not in this workspace."
	if err := c.send(http.MethodDelete, kbArticleRequestPath(args.ID), nil, nil); err != nil {
		return nil, err
	}
	return map[string]any{"conversation_id": args.ID, "withdrawn": true}, nil
}

func kbGapArticleStart(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID         string `json:"gap_id"`
		Collection string `json:"collection_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No knowledge gap " + args.ID + " in this workspace. List them with colliedesk_kb_gaps_list."
	var started restKBArticle
	if err := c.send(http.MethodPost, "/api/v1/kb/gaps/"+url.PathEscape(args.ID)+"/article", kbCollectionBody(args.Collection), &started); err != nil {
		return nil, err
	}
	answer := kbArticleView(started)
	source := domain.KBArticleGapSource{GapID: args.ID, Requested: true}
	if started.GapSource != nil {
		source = *started.GapSource
	}
	answer["gap_source"] = kbGapSourceView(source)
	return answer, nil
}

func kbArticleFromReply(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID         string `json:"conversation_id"`
		Message    string `json:"message_id"`
		Collection string `json:"collection_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "Message " + args.Message + " not found in conversation " + args.ID + " of this workspace. Read its messages with colliedesk_conversation_get."
	var made restKBArticle
	path := "/api/v1/conversations/" + url.PathEscape(args.ID) + "/messages/" + url.PathEscape(args.Message) + "/article"
	if err := c.send(http.MethodPost, path, kbCollectionBody(args.Collection), &made); err != nil {
		return nil, err
	}
	answer := kbArticleView(made)
	source := domain.KBArticleReplySource{ConversationID: args.ID, MessageID: args.Message}
	if made.ReplySource != nil {
		source = *made.ReplySource
	}
	answer["reply_source"] = kbReplySourceView(source)
	return answer, nil
}

func kbArticleRequestPath(conversationID string) string {
	return "/api/v1/conversations/" + url.PathEscape(conversationID) + "/article-request"
}

func kbCollectionBody(collectionID string) map[string]any {
	body := map[string]any{}
	if collectionID != "" {
		body["collection_id"] = collectionID
	}
	return body
}

// kbReplySourceView puts the subject and the customer question under untrusted_customer_content.
func kbReplySourceView(source domain.KBArticleReplySource) map[string]any {
	return map[string]any{"conversation_id": source.ConversationID, "display_ref": source.DisplayRef, "message_id": source.MessageID,
		"untrusted_customer_content": map[string]any{"subject": source.Subject, "question": source.Question}}
}

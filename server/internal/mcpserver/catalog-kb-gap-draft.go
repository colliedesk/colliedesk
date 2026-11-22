package mcpserver

import (
	"net/http"
	"net/url"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// kbGapDraftOutput is the draft article and the conversations it was written from. A subject is the
// customer's words, so it sits under untrusted_customer_content.
const kbGapDraftOutput = `{"type":"object","required":["article","gap_source"],"properties":{"article":` + kbArticleSchema + `,` +
	`"gap_source":{"type":"object","required":["gap_id","conversations"],"properties":{"gap_id":{"type":"string"},` +
	`"conversations":{"type":"array","maxItems":8,"description":"The resolved conversations it was written from, best match first; empty for a token without conversations:read.",` +
	`"items":{"type":"object","required":["conversation_id","display_ref","untrusted_customer_content"],"properties":{"conversation_id":{"type":"string"},` +
	`"display_ref":{"type":"string","description":"The conversation's code, as the team sees it."},` +
	`"untrusted_customer_content":{"type":"object","required":["subject"],"properties":{"subject":{"type":"string"}}}}}}}}}}`

func kbGapDraftSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_gap_draft",
		Title: "Draft the missing article for a knowledge gap with Colliedesk AI",
		Description: `Have the workspace's reply-draft AI model write the article a knowledge gap asks for, from the gap's signals and the up to 8 resolved conversations of the workspace that match it best, ` +
			`with emails and runs of 6 or more digits removed before the model reads them. The article is kept as a draft in collection_id, or the first collection; it is never published: ` +
			`read and edit it with colliedesk_kb_article_get and colliedesk_kb_article_update, then publish it with colliedesk_kb_articles_publish (which needs kb_articles:publish and any approval). ` +
			`Each call writes a new draft and counts against the included AI. Fails with ai_feature_off when reply drafts are off, ai_allowance_reached when the included AI is used up.` + "\n\n" + untrustedNotice,
		REST: []string{"POST /api/v1/kb/gaps/{id}/draft"},
		Input: `{"type":"object","additionalProperties":false,"required":["gap_id"],"properties":{` + kbGapIDProperty + `,` +
			`"collection_id":{"type":"string",` + uuid + `,"description":"A collection of the workspace (colliedesk_kb_collections_list). Default: the first one."}}}`,
		Output: kbGapDraftOutput,
		run:    kbGapDraft,
	}
}

func kbGapDraft(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID         string `json:"gap_id"`
		Collection string `json:"collection_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	if args.Collection != "" {
		body["collection_id"] = args.Collection
	}
	c.notFound = "No knowledge gap " + args.ID + " in this workspace. List them with colliedesk_kb_gaps_list."
	var drafted struct {
		restKBArticle
		GapSource *domain.KBArticleGapSource `json:"gap_source"`
	}
	if err := c.send(http.MethodPost, "/api/v1/kb/gaps/"+url.PathEscape(args.ID)+"/draft", body, &drafted); err != nil {
		return nil, err
	}
	answer := kbArticleView(drafted.restKBArticle)
	sources := []map[string]any{}
	gapID := args.ID
	if drafted.GapSource != nil {
		gapID = drafted.GapSource.GapID
		for _, conversation := range drafted.GapSource.Conversations {
			sources = append(sources, map[string]any{"conversation_id": conversation.ConversationID, "display_ref": conversation.DisplayRef,
				"untrusted_customer_content": map[string]any{"subject": conversation.Subject}})
		}
	}
	answer["gap_source"] = map[string]any{"gap_id": gapID, "conversations": sources}
	return answer, nil
}

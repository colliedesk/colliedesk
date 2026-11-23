package mcpserver

import (
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// kbConflictsOutput is the replies that contradict one article as a tool answers them: the subject of
// the conversation, which the customer wrote, sits under untrusted_customer_content.
const kbConflictsOutput = `{"type":"object","required":["article_id","open","conflicts"],"properties":{` +
	`"article_id":{"type":"string"},"open":{"type":"integer","description":"Conflicts no editor cleared; above 0 the article may be out of date."},` +
	`"conflicts":{"type":"array","description":"Open ones first, then cleared ones, newest first; at most 100.","items":{"type":"object","required":["id","reason","created_at","reply","cleared_at","cleared_by","cleared_with_token"],"properties":{` +
	`"id":{"type":"string"},"created_at":{"type":"string"},` +
	`"reason":{"type":"string","description":"The small model's sentence quoting the reply and the article: model output, not a human decision."},` +
	`"reply":{"type":"object","required":["message_id","excerpt","author","sent_at","conversation"],"properties":{` +
	`"message_id":{"type":"string"},"excerpt":{"type":"string","description":"The start of the reply the team sent, emails and long numbers removed."},` +
	`"author":{"type":["object","null"],"properties":{"agent_id":{"type":["string","null"]},"name":{"type":"string"}}},"sent_at":{"type":"string"},` +
	`"conversation":{"type":["object","null"],"description":"Where it was sent; null without conversations:read.","properties":{"id":{"type":"string"},"display_ref":{"type":"string"},` +
	`"untrusted_customer_content":{"type":"object","properties":{"subject":{"type":"string"}}}}}}},` +
	`"cleared_at":{"type":["string","null"]},` +
	`"cleared_by":{"type":["object","null"],"properties":{"agent_id":{"type":["string","null"]},"name":{"type":"string"}}},` +
	`"cleared_with_token":{"type":["object","null"],"properties":{"id":{"type":["string","null"]},"label":{"type":"string"}}}}}}}}`

func kbArticleConflictsListSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_article_conflicts_list",
		Title: "List the replies that contradict a knowledge base article",
		Description: `Answer the sent replies the workspace's small model found to contradict one article, each with its reason, its author and its conversation. ` +
			`open above 0 is the "May be out of date" flag. Fix the article with colliedesk_kb_article_update, then clear the flag with colliedesk_kb_article_conflicts_clear.` +
			"\n\n" + untrustedNotice,
		REST:   []string{"GET /api/v1/kb/articles/{id}/conflicts"},
		Input:  `{"type":"object","additionalProperties":false,"required":["article_id"],"properties":{` + kbArticleIDProperty + `}}`,
		Output: kbConflictsOutput,
		run:    kbArticleConflictsList,
	}
}

func kbArticleConflictsClearSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_article_conflicts_clear",
		Title: "Clear the out of date flag of a knowledge base article",
		Description: `Mark every open conflict of the article cleared by you, after you fixed the article or found the replies wrong. ` +
			`This cannot be undone; the cleared replies stay listed with who cleared them, and a new contradicting reply flags the article again. ` +
			`Calling it again with nothing open changes nothing.` + "\n\n" + untrustedNotice,
		Idempotent: true,
		REST:       []string{"POST /api/v1/kb/articles/{id}/conflicts/clear"},
		Input:      `{"type":"object","additionalProperties":false,"required":["article_id"],"properties":{` + kbArticleIDProperty + `}}`,
		Output:     kbConflictsOutput,
		run:        kbArticleConflictsClear,
	}
}

func kbArticleConflictsList(c *call, arguments map[string]any) (any, error) {
	return kbArticleConflicts(c, arguments, http.MethodGet)
}

func kbArticleConflictsClear(c *call, arguments map[string]any) (any, error) {
	return kbArticleConflicts(c, arguments, http.MethodPost)
}

func kbArticleConflicts(c *call, arguments map[string]any, method string) (any, error) {
	args, err := decodeArguments[struct {
		ID string `json:"article_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forArticle(args.ID)
	var conflicts domain.KBArticleConflicts
	if method == http.MethodGet {
		err = c.get(kbArticlePath(args.ID, "/conflicts"), nil, &conflicts)
	} else {
		err = c.send(method, kbArticlePath(args.ID, "/conflicts/clear"), nil, &conflicts)
	}
	if err != nil {
		return nil, err
	}
	return kbConflictsAnswer(conflicts), nil
}

func kbConflictsAnswer(conflicts domain.KBArticleConflicts) map[string]any {
	rows := make([]map[string]any, 0, len(conflicts.Conflicts))
	for _, conflict := range conflicts.Conflicts {
		var conversation any
		if where := conflict.Reply.Conversation; where != nil {
			conversation = map[string]any{"id": where.ID, "display_ref": where.DisplayRef,
				"untrusted_customer_content": map[string]any{"subject": where.Subject}}
		}
		rows = append(rows, map[string]any{
			"id": conflict.ID, "reason": conflict.Reason, "created_at": conflict.CreatedAt,
			"reply": map[string]any{"message_id": conflict.Reply.MessageID, "excerpt": conflict.Reply.Excerpt, "author": conflict.Reply.Author,
				"sent_at": conflict.Reply.SentAt, "conversation": conversation},
			"cleared_at": conflict.ClearedAt, "cleared_by": conflict.ClearedBy, "cleared_with_token": conflict.ClearedWithToken,
		})
	}
	return map[string]any{"article_id": conflicts.ArticleID, "open": conflicts.Open, "conflicts": rows}
}

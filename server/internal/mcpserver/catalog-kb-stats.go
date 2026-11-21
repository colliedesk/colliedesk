package mcpserver

import (
	"net/url"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// kbStatsOutput is how much one article was read and used per calendar day in the workspace's timezone.
// A conversation's subject is customer text, so it sits under untrusted_customer_content.
const kbStatsOutput = `{"type":"object","required":["article_id","timezone","from","to","first_day","last_day","views","uses","days","replies","replies_total"],"properties":{` +
	`"article_id":{"type":"string"},"timezone":{"type":"string","description":"The workspace's timezone; each day is a calendar day there."},` +
	`"from":{"type":"string","description":"UTC start, included."},"to":{"type":"string","description":"UTC end, excluded."},` +
	`"first_day":{"type":"string"},"last_day":{"type":"string","description":"The last day counted (the day before to)."},` +
	`"views":{"type":"integer","description":"Help center views: browsers only, one per reader per article per 30 minutes."},` +
	`"uses":{"type":"integer","description":"Sent replies that linked the article or were an AI draft quoting it, each reply once."},` +
	`"days":{"type":"array","items":{"type":"object","required":["day","views","uses"],"properties":{"day":{"type":"string"},"views":{"type":"integer"},"uses":{"type":"integer"}}}},` +
	`"replies":{"type":"array","description":"The newest 50 of those replies; open one with colliedesk_conversation_get.","items":{"type":"object","required":["conversation_id","display_ref","message_id","kinds","at","untrusted_customer_content"],"properties":{` +
	`"conversation_id":{"type":"string"},"display_ref":{"type":"string"},"message_id":{"type":"string"},` +
	`"kinds":{"type":"array","items":{"type":"string","enum":["ai_source","link"]},"description":"link: the reply linked the article; ai_source: it was an AI draft that quoted it."},` +
	`"at":{"type":"string"},"untrusted_customer_content":{"type":"object","required":["subject"],"properties":{"subject":{"type":"string"}}}}}},` +
	`"replies_total":{"type":"integer"}}}`

func kbArticleStatsSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_article_stats",
		Title: "Show how much a knowledge base article was read and used",
		Description: `Count one article's help center views and the sent replies that used it (linked it, or an AI draft quoting it), per calendar day in the workspace's timezone, ` +
			`with the conversations of those replies. Without from and to it reads the last 30 days; colliedesk_kb_article_get already carries those two totals. ` +
			`Use it to find the articles worth improving or linking.` + "\n\n" + untrustedNotice,
		REST: []string{"GET /api/v1/kb/articles/{id}/stats"},
		Input: `{"type":"object","additionalProperties":false,"required":["article_id"],"properties":{` + kbArticleIDProperty + `,` +
			`"from":{"type":"string","format":"date","description":"First day counted, YYYY-MM-DD in the workspace's timezone; give it with to."},` +
			`"to":{"type":"string","format":"date","description":"The day after the last one counted (excluded); at most 366 days after from."}}}`,
		Output: kbStatsOutput,
		run:    kbArticleStats,
	}
}

func kbArticleStats(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ID   string `json:"article_id"`
		From string `json:"from"`
		To   string `json:"to"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forArticle(args.ID)
	query := url.Values{}
	if args.From != "" {
		query.Set("from", args.From)
	}
	if args.To != "" {
		query.Set("to", args.To)
	}
	var stats domain.KBArticleStats
	if err := c.get(kbArticlePath(args.ID, "/stats"), query, &stats); err != nil {
		return nil, err
	}
	replies := make([]map[string]any, 0, len(stats.Replies))
	for _, reply := range stats.Replies {
		replies = append(replies, map[string]any{
			"conversation_id": reply.ConversationID, "display_ref": reply.DisplayRef, "message_id": reply.MessageID, "kinds": reply.Kinds, "at": reply.At,
			"untrusted_customer_content": map[string]any{"subject": reply.Subject},
		})
	}
	days := stats.Days
	if days == nil {
		days = []domain.KBArticleDay{}
	}
	return map[string]any{
		"article_id": stats.ArticleID, "timezone": stats.Timezone, "from": stats.From, "to": stats.To, "first_day": stats.FirstDay, "last_day": stats.LastDay,
		"views": stats.Views, "uses": stats.Uses, "days": days, "replies": replies, "replies_total": stats.RepliesTotal,
	}, nil
}

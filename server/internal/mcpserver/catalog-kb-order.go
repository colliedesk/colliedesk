package mcpserver

import "net/http"

const kbOrderSchema = `{"type":"object","required":["parent_id","collection_ids","article_ids"],"properties":{"parent_id":{"type":["string","null"]},` +
	`"collection_ids":{"type":"array","items":{"type":"string"}},"article_ids":{"type":"array","items":{"type":"string"}}}}`

func kbOrderSetSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_order_set",
		Title: "Order knowledge base collections and articles",
		Description: `Set the order of what sits directly under one collection (parent_id), or under the top when parent_id is left out: its collections first to last (collection_ids) ` +
			`and, under a collection, its articles first to last, drafts included (article_ids; the top holds no articles). Name every one outside the trash, each once: ` +
			`read them with colliedesk_kb_collections_list (parent_id) and colliedesk_kb_articles_search (collection_id, which lists them in their current order). ` +
			`When the lists miss one or name another, nothing changes and you are told to read them again. The help center, the collection's article list and the reply box's article picker follow the order. ` +
			`To undo, send the earlier order. It is safe to repeat. ` + kbProposeBelowSend + "\n\nTier: send.",
		Tier: "send", Idempotent: true,
		REST: []string{"PUT /api/v1/kb/order"},
		Input: `{"type":"object","additionalProperties":false,"properties":{"parent_id":{"type":"string",` + uuid + `,"description":"From colliedesk_kb_collections_list; leave it out for the top."},` +
			`"collection_ids":{"type":"array","maxItems":1000,"items":{"type":"string",` + uuid + `}},"article_ids":{"type":"array","maxItems":2000,"items":{"type":"string",` + uuid + `}}}}`,
		Output: kbOrderSchema,
		run:    kbOrderSet,
	}
}

func kbOrderSet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ParentID      *string  `json:"parent_id"`
		CollectionIDs []string `json:"collection_ids"`
		ArticleIDs    []string `json:"article_ids"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"parent_id": args.ParentID, "collection_ids": orEmpty(args.CollectionIDs), "article_ids": orEmpty(args.ArticleIDs)}
	if args.ParentID != nil {
		c.forCollection(*args.ParentID)
	}
	var order map[string]any
	if err := c.send(http.MethodPut, "/api/v1/kb/order", body, &order); err != nil {
		return nil, err
	}
	return order, nil
}

func orEmpty(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

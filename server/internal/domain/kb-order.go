package domain

// KBOrder is what sits directly under one parent collection (nil: the top of the knowledge base),
// first to last: its collections and, under a collection, its articles, drafts included. With
// HelpCenterID set it is the home of that help center: the top-level collections shown on it.
type KBOrder struct {
	ParentID      *string  `json:"parent_id"`
	HelpCenterID  *string  `json:"help_center_id,omitempty"`
	CollectionIDs []string `json:"collection_ids"`
	ArticleIDs    []string `json:"article_ids"`
}

// KBOrderStaleError refuses an order whose lists are not exactly what the parent holds outside the
// trash; Current is what it holds, in its current order, so a client can apply its move again.
type KBOrderStaleError struct{ Current KBOrder }

func (e *KBOrderStaleError) Error() string {
	return "the order names other collections or articles than the parent holds now"
}

package mcpserver

// Who can see an article or a collection, in a change, a filter and an answer.
const (
	kbVisibilityEnum = `"type":"string","enum":["everyone","help_chat","agents"]`

	// kbVisibilityProperty sets who can see an article or a collection.
	kbVisibilityProperty = `"visibility":{` + kbVisibilityEnum + `,"description":"Who can see it. everyone: the help center, help chat and the team. ` +
		`help_chat: help chat may quote it but never links it; the help center never shows it. agents: only the team (reply suggestions and AI drafts, which never link it). ` +
		`An article is as restricted as the most restrictive of itself and every collection it is in, their parents included. ` +
		`Restricting applies at once with kb_articles:update; showing published articles to more people needs kb_articles:publish, and in a collection that needs approval an approved review of each.","default":"everyone"}`

	// kbVisibilityFilter keeps the articles with one effective visibility.
	kbVisibilityFilter = `"visibility":{` + kbVisibilityEnum + `,"description":"Only the articles with this effective_visibility."}`

	// kbVisibilityOutput is the visibility set on an article or a collection and the one that applies.
	kbVisibilityOutput = `"visibility":{` + kbVisibilityEnum + `,"description":"Its own visibility, as set on it."},` +
		`"effective_visibility":{` + kbVisibilityEnum + `,"description":"Who can see it now: the most restrictive of its own visibility and every collection it is in, their parents included."}`

	// kbVisibilityFromOutput is the collection that keeps an article more restricted than its own visibility.
	kbVisibilityFromOutput = `"visibility_from":{"type":["object","null"],"description":"The collection (or a parent of one) that makes it more restricted than its own visibility, ` +
		`as in Agents only because of <name>; null when its own visibility decides.","required":["id","name","visibility"],` +
		`"properties":{"id":{"type":"string"},"name":{"type":"string"},"visibility":{` + kbVisibilityEnum + `}}}`
)

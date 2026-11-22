package store

import (
	"strconv"
	"strings"
)

// A workspace's knowledge base is one, and each of its help centers shows part of it: the top-level
// collections placed on it (kb_collection_help_centers) and every collection inside them. The public
// reads write the help center they serve as $H; onHelpCenter binds it.

// onHelpCenter binds $H in query to helpCenterID, appended to args as the last parameter; "" is the
// workspace's primary help center. The id is read once per statement, never per row.
func onHelpCenter(helpCenterID, query string, args ...any) (string, []any) {
	args = append(args, helpCenterID)
	return bindHelpCenter(query, len(args)), args
}

// bindHelpCenter writes $H of query as parameter n, a help center id or "" for the primary one.
func bindHelpCenter(query string, n int) string {
	param := "$" + strconv.Itoa(n)
	return strings.ReplaceAll(query, "$H", `(SELECT COALESCE(NULLIF(`+param+`::text, '')::uuid, primary_help_center_id($1)))`)
}

// shownKBCollections is the set of collections of workspace $1 that help center $H shows: those placed
// on it and every collection inside them, at any depth.
const shownKBCollections = `(WITH RECURSIVE placed AS (SELECT p.collection_id AS id FROM kb_collection_help_centers p
		WHERE p.workspace_id=$1 AND p.help_center_id=$H
		UNION SELECT c.id FROM kb_collections c JOIN placed s ON c.parent_id=s.id WHERE c.workspace_id=$1) SELECT id FROM placed)`

// kbArticleOnHelpCenter says article a is on help center $H: its home collection is shown there, or
// another collection it is listed in is.
const kbArticleOnHelpCenter = `(a.collection_id IN ` + shownKBCollections + ` OR EXISTS (SELECT 1 FROM kb_article_collections x
		WHERE x.workspace_id=a.workspace_id AND x.article_id=a.id AND x.collection_id IN ` + shownKBCollections + `))`

// shownKBArticle is publicKBArticle on help center $H: the article is public and placed there.
const shownKBArticle = publicKBArticle + ` AND ` + kbArticleOnHelpCenter

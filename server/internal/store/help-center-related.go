package store

import (
	"context"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// relatedArticlesSQL lists, for the article numbered $2 of workspace $1 that a reader of help center
// $H may open, at most $3 other articles that help center shows: first those whose words are closest to its own,
// then the rest of its home collection in that collection's order. With a translation join (the
// articles of language $4 only), the words, titles and slugs are those of the translations; words
// joins the row whose vector is read to an article already known to be shown, without checking again.
//
// The source's words are its title's and the 24 longest of its body without digits (ids, versions
// and hex parts of links say nothing about the subject). Each is weighted by how rare it is among the
// candidates (ln((N+1)/(df+0.5)), the AI retrieval's weight) and doubled when it is in the title; a
// word that more than half of the candidates hold (and more than two) is a common word and not
// counted, so "the" and "and" never make a match. The articles shown are listed first, so only
// their vectors are read, each once (OFFSET 0), as search does: there is no index on them,
// row-level security keeps Postgres from using one.
func relatedArticlesSQL(translation, words, search, title, columns string) string {
	return `WITH src AS MATERIALIZED (SELECT a.id, a.collection_id, ` + search + ` AS search, kb_search_vector(` + title + `) AS title
		FROM kb_articles a` + translation + ` WHERE ` + shownKBArticle + ` AND a.number=$2),
	terms AS MATERIALIZED (SELECT w.lexeme, bool_or(w.in_title) AS in_title FROM (
			SELECT x AS lexeme, true AS in_title FROM src, unnest(tsvector_to_array(src.title)) x
			UNION ALL SELECT b.x, false FROM (SELECT x FROM src, unnest(tsvector_to_array(src.search)) x
				WHERE length(x) >= 3 AND x !~ '[0-9]' ORDER BY length(x) DESC, x LIMIT 24) b) w
		GROUP BY w.lexeme),
	q AS MATERIALIZED (SELECT lexeme, in_title, ('''' || replace(replace(lexeme, '\', '\\'), '''', '''''') || '''')::tsquery AS query FROM terms),
	shown AS MATERIALIZED (SELECT a.id FROM kb_articles a` + translation + ` WHERE ` + shownKBArticle + ` AND a.id <> (SELECT id FROM src)),
	hits AS MATERIALIZED (SELECT a.id, a.number, ARRAY(SELECT q.lexeme FROM q WHERE v.search @@ q.query) AS words
		FROM shown JOIN kb_articles a ON a.workspace_id=$1 AND a.id=shown.id` + words + `
		CROSS JOIN LATERAL (SELECT ` + search + ` || ''::tsvector AS search OFFSET 0) v),
	n AS (SELECT count(*)::float8 AS n FROM hits),
	df AS (SELECT w AS lexeme, count(*)::float8 AS df FROM hits, unnest(hits.words) w GROUP BY w),
	kept AS (SELECT df.lexeme, ln((n.n + 1) / (df.df + 0.5)) * CASE WHEN q.in_title THEN 2 ELSE 1 END AS weight
		FROM df JOIN q USING (lexeme), n WHERE df.df <= greatest(2, n.n / 2)),
	score AS MATERIALIZED (SELECT hits.id, hits.number, sum(kept.weight) AS s FROM hits, unnest(hits.words) w JOIN kept ON kept.lexeme = w
		GROUP BY hits.id, hits.number ORDER BY s DESC, hits.number LIMIT $3),
	fill AS (SELECT a.id, row_number() OVER (ORDER BY a.extra, CASE WHEN a.extra THEN 0 ELSE a.position END, lower(` + title + `), a.number) AS place
		FROM ` + listedKBArticles + ` JOIN shown ON shown.id=a.id` + words + `
		WHERE a.workspace_id=$1 AND a.listed = (SELECT collection_id FROM src) AND a.id NOT IN (SELECT id FROM score)
		ORDER BY place LIMIT $3),
	pick AS (SELECT id, 0 AS tier, s, number AS place FROM score UNION ALL SELECT id, 1, 0, place FROM fill)
	SELECT ` + columns + ` FROM pick r JOIN kb_articles a ON a.workspace_id=$1 AND a.id=r.id` + words + `
	ORDER BY r.tier, r.s DESC, r.place LIMIT $3`
}

var (
	relatedPrimarySQL    = relatedArticlesSQL("", "", "a.search", "a.title", publicKBArticleColumns)
	relatedTranslatedSQL = relatedArticlesSQL(shownTranslation(`$4`),
		` JOIN kb_article_translations t ON t.workspace_id=a.workspace_id AND t.article_id=a.id AND t.locale=$4`,
		"t.search", "t.title", publicKBTranslationColumns)
)

// HelpCenterRelatedArticles is HelpCenterRelatedArticlesIn on the workspace's primary help center.
func (s *Store) HelpCenterRelatedArticles(ctx context.Context, workspaceID, locale string, number int64, limit int) ([]domain.PublicKBArticle, error) {
	return s.HelpCenterRelatedArticlesIn(ctx, workspaceID, "", locale, number, limit)
}

// HelpCenterRelatedArticlesIn returns the published articles a reader of article number on help
// center helpCenterID ("" the primary one) may want instead, in language locale ("" the primary
// one): the closest in words first, from any collection, then the rest of its home collection in
// order, at most limit. It is empty when the article is not one a reader of that help center may
// open. Only articles that help center shows are listed, in a language only those published and
// shown in it, as on every other help center list.
func (s *Store) HelpCenterRelatedArticlesIn(ctx context.Context, workspaceID, helpCenterID, locale string, number int64, limit int) ([]domain.PublicKBArticle, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.PublicKBArticle, error) {
		query, args := onHelpCenter(helpCenterID, relatedPrimarySQL, workspaceID, number, limit)
		if locale != "" {
			query, args = onHelpCenter(helpCenterID, relatedTranslatedSQL, workspaceID, number, limit, locale)
		}
		rows, err := tx.Query(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.PublicKBArticle, error) {
			return scanPublicKBArticle(row)
		})
	})
}

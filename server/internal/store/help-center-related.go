package store

import (
	"context"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// relatedArticlesSQL lists, for the article numbered $2 of workspace $1 that a reader of the help
// center may open, at most $3 other articles it shows: first those whose words are closest to its own,
// then the rest of its home collection in that collection's order. With a translation join (the
// articles of language $4 only), the words, titles and slugs are those of the translations: page is
// the row (a or t) that holds them. It runs as the reader of one help center: row-level security keeps
// the articles of every other one out, and $H, bound only with a translation join, picks the
// languages that help center shows.
//
// The source's words are its title's and the 24 longest of its body without digits (ids, versions
// and hex parts of links say nothing about the subject). Each is weighted by how rare it is among the
// candidates (ln((N+1)/(df+0.5)), the AI retrieval's weight) and doubled when it is in the title; a
// word that more than half of the candidates hold (and more than two) is a common word and not
// counted, so "the" and "and" never make a match. The articles shown are listed once with all a
// match and a link need, so only their vectors are read, each once: there is no index on them,
// row-level security keeps Postgres from using one. A vector is mostly stored out of line, and each
// match would fetch it again; a row built around it (OFFSET 0 keeps Postgres from unwrapping it)
// fetches it once without the copy a `|| ''` costs. Reading kb_articles and its
// translations again after that would make Postgres plan their row security again for every read,
// which costs more than the rows on a page opened right after an edit.
func relatedArticlesSQL(translation, page string) string {
	return `WITH src AS MATERIALIZED (SELECT a.id, a.collection_id, ` + page + `.search AS search, kb_search_vector(` + page + `.title) AS title
		FROM kb_articles a` + translation + ` WHERE ` + publicKBArticle + ` AND a.number=$2),
	terms AS MATERIALIZED (SELECT w.lexeme, bool_or(w.in_title) AS in_title FROM (
			SELECT x AS lexeme, true AS in_title FROM src, unnest(tsvector_to_array(src.title)) x
			UNION ALL SELECT b.x, false FROM (SELECT x FROM src, unnest(tsvector_to_array(src.search)) x
				WHERE length(x) >= 3 AND x !~ '[0-9]' ORDER BY length(x) DESC, x LIMIT 24) b) w
		GROUP BY w.lexeme),
	q AS MATERIALIZED (SELECT lexeme, in_title, ('''' || replace(replace(lexeme, '\', '\\'), '''', '''''') || '''')::tsquery AS query FROM terms),
	shown AS MATERIALIZED (SELECT a.id, a.number, a.collection_id, ` + page + `.slug, ` + page + `.title, ` + page + `.published_at, ` + page + `.updated_at, ` + page + `.search
		FROM kb_articles a` + translation + ` WHERE ` + publicKBArticle + ` AND a.id <> (SELECT id FROM src)),
	hits AS MATERIALIZED (SELECT shown.id, shown.number, ARRAY(SELECT q.lexeme FROM q WHERE (v.r).f1 @@ q.query) AS words
		FROM shown CROSS JOIN LATERAL (SELECT ROW(shown.search) AS r OFFSET 0) v),
	n AS (SELECT count(*)::float8 AS n FROM hits),
	df AS (SELECT w AS lexeme, count(*)::float8 AS df FROM hits, unnest(hits.words) w GROUP BY w),
	kept AS (SELECT df.lexeme, ln((n.n + 1) / (df.df + 0.5)) * CASE WHEN q.in_title THEN 2 ELSE 1 END AS weight
		FROM df JOIN q USING (lexeme), n WHERE df.df <= greatest(2, n.n / 2)),
	score AS MATERIALIZED (SELECT hits.id, hits.number, sum(kept.weight) AS s FROM hits, unnest(hits.words) w JOIN kept ON kept.lexeme = w
		GROUP BY hits.id, hits.number ORDER BY s DESC, hits.number LIMIT $3),
	fill AS (SELECT a.id, row_number() OVER (ORDER BY a.extra, CASE WHEN a.extra THEN 0 ELSE a.position END, lower(shown.title), a.number) AS place
		FROM ` + listedKBArticles + ` JOIN shown ON shown.id=a.id
		WHERE a.workspace_id=$1 AND a.listed = (SELECT collection_id FROM src) AND a.id NOT IN (SELECT id FROM score)
		ORDER BY place LIMIT $3),
	pick AS (SELECT id, 0 AS tier, s, number AS place FROM score UNION ALL SELECT id, 1, 0, place FROM fill)
	SELECT a.number, a.slug, a.title, a.collection_id::text, a.published_at, a.updated_at FROM pick r JOIN shown a ON a.id=r.id
	ORDER BY r.tier, r.s DESC, r.place LIMIT $3`
}

var (
	relatedPrimarySQL    = relatedArticlesSQL("", "a")
	relatedTranslatedSQL = relatedArticlesSQL(shownTranslation(`$4`), "t")
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
	return helpCenterScoped(ctx, s, workspaceID, helpCenterID, func(tx pgx.Tx, helpCenterID string) ([]domain.PublicKBArticle, error) {
		query, args := relatedPrimarySQL, []any{workspaceID, number, limit}
		if locale != "" {
			query, args = onHelpCenter(helpCenterID, relatedTranslatedSQL, workspaceID, number, limit, locale)
		}
		if err := genericPlan(ctx, tx); err != nil {
			return nil, err
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

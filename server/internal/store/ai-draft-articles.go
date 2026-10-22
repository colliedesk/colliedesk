package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// A keyword search misses an article whose words differ from the customer's ("give my website name to
// my brother" against "Domain Push"). Before a draft, a small model may choose articles from the list
// of titles; these read that list and the chosen articles (docs/internals/ai-drafts.md).

// aiArticleIndexLimit caps the list of titles a chooser reads: 800 titles are about 12,000 tokens.
const aiArticleIndexLimit = 800

// AIArticleIndexEntry is one article AI may read, as the chooser sees it.
type AIArticleIndexEntry struct {
	ID      string
	Number  int64
	Title   string
	Section string
}

func aiArticleIndex(ctx context.Context, tx pgx.Tx, workspaceID string) ([]AIArticleIndexEntry, error) {
	rows, err := tx.Query(ctx, `SELECT d.kb_article_id::text, a.number, d.title, COALESCE(c.name, '')
		FROM ai_retrieval_docs d JOIN kb_articles a ON `+aiReadableArticle+`
		LEFT JOIN kb_collections c ON c.workspace_id=a.workspace_id AND c.id=a.collection_id
		WHERE d.workspace_id=$1 AND d.kb_article_id IS NOT NULL AND d.kb_locale IS NULL ORDER BY a.number LIMIT $2`, workspaceID, aiArticleIndexLimit)
	if err != nil {
		return nil, fmt.Errorf("list articles for AI: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (AIArticleIndexEntry, error) {
		var entry AIArticleIndexEntry
		return entry, row.Scan(&entry.ID, &entry.Number, &entry.Title, &entry.Section)
	})
}

// AIDraftArticles returns the articles of the workspace with these ids, in their own language, in the
// order asked, each cut to the paragraphs that best match query. An article that may no longer be read
// (unpublished, trashed, excluded from AI) is left out.
func (s *Store) AIDraftArticles(ctx context.Context, workspaceID string, ids []string, query string) ([]AIRetrievedDoc, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]AIRetrievedDoc, error) {
		terms, err := aiTerms(ctx, tx, query)
		if err != nil {
			return nil, err
		}
		rows, err := tx.Query(ctx, `SELECT d.kb_article_id::text, a.number, d.title, d.body
			FROM unnest($2::uuid[]) WITH ORDINALITY AS chosen(id, position)
			JOIN ai_retrieval_docs d ON d.workspace_id=$1 AND d.kb_article_id=chosen.id AND d.kb_locale IS NULL
			JOIN kb_articles a ON `+aiReadableArticle+` ORDER BY chosen.position`, workspaceID, ids)
		if err != nil {
			return nil, fmt.Errorf("read chosen articles for AI: %w", err)
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (AIRetrievedDoc, error) {
			doc := AIRetrievedDoc{Kind: AIRetrievedKBArticle}
			err := row.Scan(&doc.KBArticleID, &doc.Number, &doc.Title, &doc.Text)
			doc.Text = aiExcerpt(doc.Text, terms, aiKBExcerptRunes)
			return doc, err
		})
	})
}

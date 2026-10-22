package store

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// A keyword search misses an article whose words differ from the customer's ("give my website name to
// my brother" against "Domain Push"). Before a draft, a small model may choose articles from the list
// of titles; these read that list and the chosen articles (docs/internals/ai-drafts.md).

// aiArticleIndexLimit caps the list of titles a chooser reads: 800 titles are about 12,000 tokens.
const aiArticleIndexLimit = 800

const (
	// aiDraftWholeArticles is how many of retrieval's best articles a draft reads whole instead of an
	// excerpt. Measured: drafts citing an article the model saw only in part missed one of its
	// conditions about two and a half times as often.
	aiDraftWholeArticles = 3
	// aiDraftArticleRunes is the most of one article a draft reads, about 5,000 tokens.
	aiDraftArticleRunes = 20000
)

// wholeArticles gives the first n articles of docs their whole text when it fits in
// aiDraftArticleRunes, and returns the articles still shown only in part.
func wholeArticles(ctx context.Context, tx pgx.Tx, workspaceID string, docs []AIRetrievedDoc, n int) ([]AIRetrievedDoc, map[string]bool, error) {
	cut := map[string]bool{}
	seen := 0
	for i := range docs {
		if docs[i].Kind != AIRetrievedKBArticle {
			continue
		}
		var body string
		if err := tx.QueryRow(ctx, `SELECT body FROM ai_retrieval_docs WHERE workspace_id=$1 AND kb_article_id=$2 AND kb_locale IS NOT DISTINCT FROM NULLIF($3,'')`,
			workspaceID, docs[i].KBArticleID, docs[i].KBLocale).Scan(&body); err != nil {
			return nil, nil, fmt.Errorf("read whole article for AI: %w", err)
		}
		whole := utf8.RuneCountInString(body) <= aiDraftArticleRunes
		if seen < n && whole {
			docs[i].Text = body
		} else if utf8.RuneCountInString(docs[i].Text) < utf8.RuneCountInString(body) {
			cut[docs[i].KBArticleID] = true
		}
		seen++
	}
	return docs, cut, nil
}

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
// order asked, whole when they fit in aiDraftArticleRunes and otherwise cut to the paragraphs that best
// match query (and named in the second result). An article that may no longer be read (unpublished,
// trashed, excluded from AI) is left out.
func (s *Store) AIDraftArticles(ctx context.Context, workspaceID string, ids []string, query string) ([]AIRetrievedDoc, map[string]bool, error) {
	if len(ids) == 0 {
		return nil, nil, nil
	}
	cut := map[string]bool{}
	docs, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]AIRetrievedDoc, error) {
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
			if utf8.RuneCountInString(doc.Text) > aiDraftArticleRunes {
				doc.Text = aiExcerpt(doc.Text, terms, aiKBExcerptRunes)
				cut[doc.KBArticleID] = true
			}
			return doc, err
		})
	})
	return docs, cut, err
}

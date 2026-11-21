package store

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// KBArticleFilter selects articles outside the trash. Query matches every word of Query as a word or
// the start of one, or Query inside the title; a search orders by relevance. Without one, Sort orders
// the list; empty is KBSortManual inside one collection and KBSortRecent otherwise.
type KBArticleFilter struct {
	Query, Status, CollectionID, Sort string
	Limit                             int
	Cursor                            string
	// IncludeSubcollections widens CollectionID to its live collections at every depth, and to the
	// articles listed in any of them besides their home.
	IncludeSubcollections bool
	// AuthorIDs keeps the articles credited to one of these team members.
	AuthorIDs []string
	// Tags keeps the articles with one of these published tags, or with all of them when TagsAll is set.
	Tags    []string
	TagsAll bool
	// The date bounds are half-open: an article at After is kept, one at Before is not. Updated is
	// the article's token, which its unpublished changes move too.
	CreatedAfter, CreatedBefore, UpdatedAfter, UpdatedBefore *time.Time
	// Translation (KBLanguageMissing or KBLanguageOutdated) keeps the articles without a translation, or
	// with an outdated one, in Locale; in any language of the help center when Locale is empty.
	Translation, Locale string
	// HasDraft keeps only the articles with unpublished changes.
	HasDraft bool
	// Visibility keeps only the articles whose effective visibility is this one.
	Visibility string
	// ReviewState keeps only the articles whose own unpublished text waits for a reviewer
	// (KBInReview) or was approved as it is now (KBApproved).
	ReviewState string
}

// kbLiveSubtreeSQL is the live collection $n and its live collections at every depth, as an array.
const kbLiveSubtreeSQL = `ARRAY(WITH RECURSIVE sub(id) AS (SELECT id FROM kb_collections WHERE workspace_id=$1 AND id=$%[1]d AND deleted_at IS NULL
	UNION ALL SELECT c.id FROM kb_collections c JOIN sub ON c.parent_id=sub.id WHERE c.workspace_id=$1 AND c.deleted_at IS NULL) SELECT id FROM sub)`

// where adds filter's conditions on article a to args, whose $1 is the workspace. It reports false
// when the filter can match nothing (a collection id that is not one), so the caller skips the query.
func (filter KBArticleFilter) where(args []any) (string, []any, bool) {
	where := ` WHERE a.workspace_id=$1 AND a.deleted_at IS NULL`
	add := func(value any) int {
		args = append(args, value)
		return len(args)
	}
	if filter.Status != "" {
		where += fmt.Sprintf(` AND a.status=$%d`, add(filter.Status))
	}
	if filter.CollectionID != "" {
		if !savedReplyID.MatchString(filter.CollectionID) {
			return "", nil, false
		}
		n := add(filter.CollectionID)
		if filter.IncludeSubcollections {
			subtree := fmt.Sprintf(kbLiveSubtreeSQL, n)
			where += ` AND (a.collection_id = ANY(` + subtree + `) OR EXISTS (SELECT 1 FROM kb_article_collections x
				WHERE x.workspace_id=a.workspace_id AND x.article_id=a.id AND x.collection_id = ANY(` + subtree + `)))`
		} else {
			where += fmt.Sprintf(` AND a.collection_id=$%d`, n)
		}
	}
	if len(filter.AuthorIDs) > 0 {
		where += fmt.Sprintf(` AND a.author_agent_id = ANY($%d::uuid[])`, add(filter.AuthorIDs))
	}
	if len(filter.Tags) > 0 {
		n := add(filter.Tags)
		tagged := fmt.Sprintf(`FROM kb_article_tags g WHERE g.workspace_id=a.workspace_id AND g.article_id=a.id AND g.tag = ANY($%d::text[])`, n)
		if filter.TagsAll {
			where += fmt.Sprintf(` AND (SELECT count(DISTINCT g.tag) %s) = cardinality($%d::text[])`, tagged, n)
		} else {
			where += ` AND EXISTS (SELECT 1 ` + tagged + `)`
		}
	}
	for _, bound := range []struct {
		at     *time.Time
		column string
		op     string
	}{
		{filter.CreatedAfter, `a.created_at`, `>=`}, {filter.CreatedBefore, `a.created_at`, `<`},
		{filter.UpdatedAfter, kbArticleToken, `>=`}, {filter.UpdatedBefore, kbArticleToken, `<`},
	} {
		if bound.at != nil {
			where += fmt.Sprintf(` AND %s %s $%d`, bound.column, bound.op, add(*bound.at))
		}
	}
	live := `FROM kb_article_translations t WHERE t.workspace_id=a.workspace_id AND t.article_id=a.id AND t.deleted_at IS NULL`
	switch {
	case filter.Translation == domain.KBLanguageMissing && filter.Locale != "":
		where += fmt.Sprintf(` AND NOT EXISTS (SELECT 1 %s AND t.locale=$%d)`, live, add(filter.Locale))
	case filter.Translation == domain.KBLanguageMissing:
		where += ` AND EXISTS (SELECT 1 FROM help_center_languages l WHERE l.workspace_id=a.workspace_id AND NOT EXISTS (SELECT 1 ` + live + ` AND t.locale=l.locale))`
	case filter.Translation == domain.KBLanguageOutdated && filter.Locale != "":
		where += fmt.Sprintf(` AND EXISTS (SELECT 1 %s AND t.outdated AND t.locale=$%d)`, live, add(filter.Locale))
	case filter.Translation == domain.KBLanguageOutdated:
		where += ` AND EXISTS (SELECT 1 ` + live + ` AND t.outdated)`
	}
	if filter.HasDraft {
		where += ` AND ` + kbHasDraft
	}
	if filter.Visibility != "" {
		where += fmt.Sprintf(` AND EXISTS (SELECT 1 FROM kb_article_visibility w WHERE w.workspace_id=a.workspace_id AND w.id=a.id AND w.visibility_rank=kb_visibility_rank($%d))`, add(filter.Visibility))
	}
	switch filter.ReviewState {
	case KBInReview:
		where += ` AND EXISTS (SELECT 1 FROM kb_reviews r WHERE r.workspace_id=a.workspace_id AND r.article_id=a.id AND r.locale IS NULL AND r.state='open')`
	case KBApproved:
		where += ` AND EXISTS (SELECT 1 FROM kb_reviews r WHERE r.workspace_id=a.workspace_id AND r.article_id=a.id AND r.locale IS NULL AND r.approves_pending)`
	}
	return where, args, true
}

// CheckKBFilterLocale refuses, as ListKBArticles does, a filter locale that is not a language of the
// workspace's help center; a saved view checks its filters with it before they are stored.
func (s *Store) CheckKBFilterLocale(ctx context.Context, workspaceID, locale string) error {
	if locale == "" {
		return nil
	}
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		return struct{}{}, checkKBFilterLocale(ctx, tx, workspaceID, locale)
	})
	return err
}

// checkKBFilterLocale refuses a filter Locale that is not a language of the help center.
func checkKBFilterLocale(ctx context.Context, tx pgx.Tx, workspaceID, locale string) error {
	if locale == "" {
		return nil
	}
	var listed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM help_center_languages WHERE workspace_id=$1 AND locale=$2)`, workspaceID, locale).Scan(&listed); err != nil {
		return err
	}
	if !listed {
		return &domain.InvalidKBError{Message: "locale is not a language of the help center; GET /api/v1/help-center/languages lists them."}
	}
	return nil
}

// fillKBLanguages sets the Languages of articles read by scanKBSummary: their translations outside
// the trash, by locale. One query for the whole page.
func fillKBLanguages(ctx context.Context, tx pgx.Tx, workspaceID string, items ...*domain.KBArticleSummary) error {
	ids := make([]string, 0, len(items))
	byID := make(map[string]*domain.KBArticleSummary, len(items))
	for _, item := range items {
		item.Languages = []domain.KBArticleLanguage{}
		ids = append(ids, item.ID)
		byID[item.ID] = item
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT article_id::text, locale, outdated FROM kb_article_translations
		WHERE workspace_id=$1 AND article_id=ANY($2::uuid[]) AND deleted_at IS NULL ORDER BY locale`, workspaceID, ids)
	if err != nil {
		return fmt.Errorf("read knowledge base article languages: %w", err)
	}
	var articleID string
	var language domain.KBArticleLanguage
	if _, err := pgx.ForEachRow(rows, []any{&articleID, &language.Locale, &language.Outdated}, func() error {
		if item := byID[articleID]; item != nil {
			item.Languages = append(item.Languages, language)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("read knowledge base article languages: %w", err)
	}
	return nil
}

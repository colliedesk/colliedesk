package store

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// kbSearchTermsQuery groups the searches of one workspace in [$2, $3] by their folded words. Each
// group shows the newest words and result count. %s is the extra filter of the list.
const kbSearchTermsQuery = `SELECT (array_agg(query ORDER BY created_at DESC, id DESC))[1],
		count(*), count(opened_article_id),
		(array_agg(results ORDER BY created_at DESC, id DESC))[1], max(created_at)
	FROM kb_searches
	WHERE workspace_id=$1 AND created_at >= $2 AND created_at <= $3 %s
	GROUP BY query_key
	ORDER BY count(*) DESC, max(created_at) DESC, query_key
	LIMIT $4`

// KBSearchReport is what readers searched for on the help center in the last KBSearchReportDays:
// the most searched words, the words that found nothing and the searches whose results nobody opened.
func (s *Store) KBSearchReport(ctx context.Context, workspaceID string) (domain.KBSearchReport, error) {
	to := s.now().UTC()
	report := domain.KBSearchReport{From: to.Add(-domain.KBSearchReportDays * 24 * time.Hour), To: to}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBSearchReport, error) {
		err := tx.QueryRow(ctx, `SELECT count(*) FROM kb_searches
			WHERE workspace_id=$1 AND created_at >= $2 AND created_at <= $3`,
			workspaceID, report.From, report.To).Scan(&report.Searches)
		if err != nil {
			return report, fmt.Errorf("count help center searches: %w", err)
		}
		lists := []struct {
			filter string
			into   *[]domain.KBSearchTerm
		}{
			{"", &report.Top},
			{"AND results = 0", &report.NoResults},
			{"AND results > 0 AND opened_article_id IS NULL", &report.NothingOpened},
		}
		for _, list := range lists {
			terms, err := kbSearchTerms(ctx, tx, list.filter, workspaceID, report.From, report.To)
			if err != nil {
				return report, err
			}
			*list.into = terms
		}
		return report, nil
	})
}

func kbSearchTerms(ctx context.Context, tx pgx.Tx, filter, workspaceID string, from, to time.Time) ([]domain.KBSearchTerm, error) {
	rows, err := tx.Query(ctx, fmt.Sprintf(kbSearchTermsQuery, filter), workspaceID, from, to, domain.MaxKBSearchReportRows)
	if err != nil {
		return nil, fmt.Errorf("list help center searches: %w", err)
	}
	defer rows.Close()
	terms := []domain.KBSearchTerm{}
	for rows.Next() {
		var term domain.KBSearchTerm
		if err := rows.Scan(&term.Query, &term.Searches, &term.Opened, &term.Results, &term.LastSearchedAt); err != nil {
			return nil, fmt.Errorf("read help center search: %w", err)
		}
		term.LastSearchedAt = term.LastSearchedAt.UTC()
		terms = append(terms, term)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list help center searches: %w", err)
	}
	return terms, nil
}

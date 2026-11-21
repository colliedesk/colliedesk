package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/results"
)

// KBArticleStats reads the views and uses of article id of workspaceID per calendar day in the
// workspace's timezone, for the days [from, to) written YYYY-MM-DD, or the last
// KBArticleStatsDefaultDays up to now, today included, when both are empty. An article in the trash
// keeps its stats. domain.ErrKBArticleNotFound when the workspace has no such article.
func (s *Store) KBArticleStats(ctx context.Context, workspaceID, id string, now time.Time, from, to string) (domain.KBArticleStats, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBArticleStats{}, domain.ErrKBArticleNotFound
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticleStats, error) {
		var timezone string
		var found bool
		if err := tx.QueryRow(ctx, `SELECT w.timezone, EXISTS(SELECT 1 FROM kb_articles a WHERE a.workspace_id=w.id AND a.id=$2)
			FROM workspaces w WHERE w.id=$1`, workspaceID, id).Scan(&timezone, &found); err != nil {
			return domain.KBArticleStats{}, fmt.Errorf("read article stats: %w", err)
		}
		if !found {
			return domain.KBArticleStats{}, domain.ErrKBArticleNotFound
		}
		loc := results.Location(timezone)
		start, end, err := kbArticleStatsRange(now, loc, from, to)
		if err != nil {
			return domain.KBArticleStats{}, err
		}
		stats := domain.KBArticleStats{ArticleID: id, Timezone: loc.String(), From: start, To: end, Days: []domain.KBArticleDay{}}
		firstDay, endDay := start.In(loc).Format(results.DateLayout), end.In(loc).Format(results.DateLayout)
		views, err := dailyCounts(ctx, tx, `SELECT to_char(day, 'YYYY-MM-DD'), sum(views)::bigint FROM kb_article_views
			WHERE workspace_id=$1 AND kb_article_id=$2 AND day>=$3::date AND day<$4::date GROUP BY day`, workspaceID, id, firstDay, endDay)
		if err != nil {
			return domain.KBArticleStats{}, fmt.Errorf("read article views: %w", err)
		}
		uses, err := dailyCounts(ctx, tx, `SELECT to_char(created_at AT TIME ZONE $5, 'YYYY-MM-DD'), count(DISTINCT message_id) FROM kb_article_uses
			WHERE workspace_id=$1 AND kb_article_id=$2 AND created_at>=$3 AND created_at<$4 GROUP BY 1`, workspaceID, id, start, end, loc.String())
		if err != nil {
			return domain.KBArticleStats{}, fmt.Errorf("read article uses: %w", err)
		}
		for day := start.In(loc); day.Before(end); day = time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc) {
			key := day.Format(results.DateLayout)
			stats.Days = append(stats.Days, domain.KBArticleDay{Day: key, Views: views[key], Uses: uses[key]})
			stats.Views += views[key]
		}
		stats.FirstDay, stats.LastDay = firstDay, stats.Days[len(stats.Days)-1].Day
		if stats.Replies, stats.RepliesTotal, err = kbArticleReplies(ctx, tx, workspaceID, id, start, end); err != nil {
			return domain.KBArticleStats{}, err
		}
		stats.Uses = stats.RepliesTotal
		return stats, nil
	})
}

// kbArticleStatsRange is the days [from, to) in loc, or the default days up to now when both are empty.
func kbArticleStatsRange(now time.Time, loc *time.Location, from, to string) (time.Time, time.Time, error) {
	if from == "" && to == "" {
		start, end := results.Period(now, loc, domain.KBArticleStatsDefaultDays)
		return start, end, nil
	}
	first, errFrom := time.ParseInLocation(results.DateLayout, from, loc)
	end, errTo := time.ParseInLocation(results.DateLayout, to, loc)
	switch {
	case errFrom != nil || errTo != nil:
		return time.Time{}, time.Time{}, &domain.InvalidKBError{Message: "Give from and to together, as days written YYYY-MM-DD (for example from=2026-11-01&to=2026-12-01)."}
	case !end.After(first):
		return time.Time{}, time.Time{}, &domain.InvalidKBError{Message: "to must be a later day than from: the days are [from, to), to excluded."}
	case end.After(first.AddDate(0, 0, domain.MaxKBArticleStatsDays)):
		return time.Time{}, time.Time{}, &domain.InvalidKBError{Message: fmt.Sprintf("Ask for at most %d days at a time.", domain.MaxKBArticleStatsDays)}
	}
	return first.UTC(), end.UTC(), nil
}

// kbArticleReplies are the newest sent replies in [from, to) that used the article, and how many there are.
func kbArticleReplies(ctx context.Context, tx pgx.Tx, workspaceID, id string, from, to time.Time) ([]domain.KBArticleUse, int64, error) {
	rows, err := tx.Query(ctx, `SELECT u.conversation_id::text, COALESCE(c.display_ref, ''), c.subject, u.message_id::text,
			array_agg(u.kind ORDER BY u.kind), min(u.created_at), count(*) OVER ()
		FROM kb_article_uses u JOIN conversations c ON c.workspace_id=u.workspace_id AND c.id=u.conversation_id
		WHERE u.workspace_id=$1 AND u.kb_article_id=$2 AND u.created_at>=$3 AND u.created_at<$4
		GROUP BY u.conversation_id, c.display_ref, c.subject, u.message_id
		ORDER BY min(u.created_at) DESC, u.message_id LIMIT $5`, workspaceID, id, from, to, domain.MaxKBArticleStatsReplies)
	if err != nil {
		return nil, 0, fmt.Errorf("read the replies that used the article: %w", err)
	}
	defer rows.Close()
	replies, total := []domain.KBArticleUse{}, int64(0)
	for rows.Next() {
		var use domain.KBArticleUse
		if err := rows.Scan(&use.ConversationID, &use.DisplayRef, &use.Subject, &use.MessageID, &use.Kinds, &use.At, &total); err != nil {
			return nil, 0, fmt.Errorf("read the replies that used the article: %w", err)
		}
		use.At = use.At.UTC()
		replies = append(replies, use)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read the replies that used the article: %w", err)
	}
	return replies, total, nil
}

package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/results"
)

// helpCenterResults reads the help center section of the results page for now, live: views and
// conversations opened per calendar day of the longest window, then each window's articles. A
// helpCenterID reads one help center: the articles it shows and their views (views are not recorded per
// help center), and the conversations that came in through it; "" reads the whole workspace.
func helpCenterResults(ctx context.Context, tx pgx.Tx, workspaceID, helpCenterID string, now time.Time) (*results.HelpCenter, error) {
	var timezone string
	if err := tx.QueryRow(ctx, `SELECT timezone FROM workspaces WHERE id=$1`, workspaceID).Scan(&timezone); err != nil {
		return nil, fmt.Errorf("read the workspace timezone: %w", err)
	}
	loc := results.Location(timezone)
	longest := 0
	for _, window := range results.Windows {
		longest = max(longest, window.Days)
	}
	from, to := results.Period(now, loc, longest)
	viewsSQL := `SELECT to_char(day, 'YYYY-MM-DD'), sum(views)::bigint FROM kb_article_views
		WHERE workspace_id=$1 AND day>=$2::date AND day<$3::date GROUP BY day`
	viewsArgs := []any{workspaceID, from.In(loc).Format(results.DateLayout), to.In(loc).Format(results.DateLayout)}
	if helpCenterID != "" {
		viewsSQL, viewsArgs = onHelpCenter(helpCenterID, `SELECT to_char(v.day, 'YYYY-MM-DD'), sum(v.views)::bigint FROM kb_article_views v
			JOIN kb_articles a ON a.workspace_id=v.workspace_id AND a.id=v.kb_article_id AND `+kbArticleOnHelpCenter+`
			WHERE v.workspace_id=$1 AND v.day>=$2::date AND v.day<$3::date GROUP BY v.day`, viewsArgs...)
	}
	views, err := dailyCounts(ctx, tx, viewsSQL, viewsArgs...)
	if err != nil {
		return nil, fmt.Errorf("read help center views: %w", err)
	}
	// The inbox's source=native list for the same range: every status but spam, never imported.
	requestsSQL := `SELECT to_char(c.created_at AT TIME ZONE $4, 'YYYY-MM-DD'), count(*) FROM conversations c
		WHERE c.workspace_id=$1 AND c.created_at>=$2 AND c.created_at<$3 AND c.status<>'spam' AND c.origin IS NULL`
	requestsArgs := []any{workspaceID, from, to, loc.String()}
	if helpCenterID != "" {
		requestsSQL += ` AND c.help_center_id=$5`
		requestsArgs = append(requestsArgs, helpCenterID)
	}
	requests, err := dailyCounts(ctx, tx, requestsSQL+` GROUP BY 1`, requestsArgs...)
	if err != nil {
		return nil, fmt.Errorf("read conversations opened: %w", err)
	}
	section := &results.HelpCenter{Timezone: loc.String(), Windows: map[string]*results.HelpCenterWindow{}}
	for _, window := range results.Windows {
		start, end := results.Period(now, loc, window.Days)
		span := results.HelpCenterSpan(start, end, loc, views, requests)
		if span.Articles, span.ArticlesTotal, err = helpCenterArticles(ctx, tx, workspaceID, helpCenterID, start, end, loc); err != nil {
			return nil, err
		}
		section.Windows[window.Key] = span
	}
	return section, nil
}

func dailyCounts(ctx context.Context, tx pgx.Tx, sql string, args ...any) (map[string]int64, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	for rows.Next() {
		var day string
		var count int64
		if err := rows.Scan(&day, &count); err != nil {
			return nil, err
		}
		counts[day] = count
	}
	return counts, rows.Err()
}

// helpCenterArticleVotes are an article's votes as its page shows them, for the row top.
var helpCenterArticleVotes = strings.ReplaceAll(kbVoteCountsSQL, "$2", "top.id")

// helpCenterArticles are the articles viewed or used in a sent reply in [from, to), most viewed first.
// Only articles the help center shows are listed: results reach members without a knowledge base
// permission, so an article kept for the team or for help chat is never named. A helpCenterID lists
// only the articles that help center shows.
func helpCenterArticles(ctx context.Context, tx pgx.Tx, workspaceID, helpCenterID string, from, to time.Time, loc *time.Location) ([]results.HelpCenterArticle, int, error) {
	shown, args := publicKBArticle, []any{workspaceID, from.In(loc).Format(results.DateLayout), to.In(loc).Format(results.DateLayout), from, to, results.MaxHelpCenterArticles}
	if helpCenterID != "" {
		shown, args = onHelpCenter(helpCenterID, shownKBArticle, args...)
	}
	rows, err := tx.Query(ctx, `WITH v AS (
			SELECT kb_article_id AS id, sum(views)::bigint AS views FROM kb_article_views
			WHERE workspace_id=$1 AND day>=$2::date AND day<$3::date GROUP BY 1),
		u AS (
			SELECT kb_article_id AS id, count(DISTINCT message_id) AS uses FROM kb_article_uses
			WHERE workspace_id=$1 AND created_at>=$4 AND created_at<$5 GROUP BY 1),
		top AS (
			SELECT a.id, a.number, a.title, COALESCE(v.views, 0) AS views, COALESCE(u.uses, 0) AS uses, count(*) OVER () AS total
			FROM v FULL JOIN u ON u.id=v.id JOIN kb_articles a ON a.id=COALESCE(v.id, u.id) AND `+shown+`
			ORDER BY views DESC, uses DESC, a.number LIMIT $6)
		SELECT top.id::text, top.number, top.title, top.views, top.uses, top.total, votes.up, votes.down, votes.imported_up, votes.imported_down
		FROM top, LATERAL (`+helpCenterArticleVotes+`) votes(up, down, imported_up, imported_down)
		ORDER BY top.views DESC, top.uses DESC, top.number`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("read help center articles: %w", err)
	}
	defer rows.Close()
	articles, total := []results.HelpCenterArticle{}, 0
	for rows.Next() {
		var article results.HelpCenterArticle
		var up, down, importedUp, importedDown int64
		if err := rows.Scan(&article.ID, &article.Number, &article.Title, &article.Views, &article.Uses, &total, &up, &down, &importedUp, &importedDown); err != nil {
			return nil, 0, fmt.Errorf("read help center articles: %w", err)
		}
		article.Helpful, article.Votes = up+importedUp, up+importedUp+down+importedDown
		if article.Votes > 0 {
			percent := int((article.Helpful*100 + article.Votes/2) / article.Votes)
			article.HelpfulPercent = &percent
		}
		articles = append(articles, article)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read help center articles: %w", err)
	}
	return articles, total, nil
}

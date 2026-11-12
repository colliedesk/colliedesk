package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// countedStatuses are the statuses the inbox has a tab for; each is counted, zero included.
var countedStatuses = []string{"open", "pending", "resolved", "closed", "spam"}

// conversationStatusCounts counts the conversations of each status that match every other filter.
func conversationStatusCounts(ctx context.Context, tx pgx.Tx, workspaceID string, filter ConversationFilter, now time.Time) (map[string]int, error) {
	filter.Statuses = countedStatuses
	q := conversationConditions(workspaceID, filter, now)
	rows, err := tx.Query(ctx, `SELECT c.status, count(*) FROM conversations c WHERE `+strings.Join(q.where, " AND ")+` GROUP BY c.status`, q.args...)
	if err != nil {
		return nil, fmt.Errorf("count conversations by status: %w", err)
	}
	defer rows.Close()
	counts := make(map[string]int, len(countedStatuses))
	for _, status := range countedStatuses {
		counts[status] = 0
	}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, fmt.Errorf("scan status count: %w", err)
		}
		counts[status] = n
	}
	return counts, rows.Err()
}

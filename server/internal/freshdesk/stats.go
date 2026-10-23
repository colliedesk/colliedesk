package freshdesk

import (
	"context"
	"encoding/json"
	"fmt"
)

// statsBatch is how many unchanged tickets get their stats in one statement.
const statsBatch = 500

// fillStats queues stats for a ticket the run skips because it did not change. The stats are read
// once: the statement fills only a conversation that has none, so a later run never moves what the
// results baseline measured, and it never touches updated_at (the team's activity).
func (r *run) fillStats(ctx context.Context, conversationID string, stats *fdStats) error {
	encoded, err := json.Marshal(originStats(stats))
	if err != nil {
		return fmt.Errorf("encode ticket stats: %w", err)
	}
	r.pendingStats = append(r.pendingStats, pendingStats{conversationID: conversationID, stats: string(encoded)})
	if len(r.pendingStats) >= statsBatch {
		return r.flushStats(ctx)
	}
	return nil
}

type pendingStats struct {
	conversationID string
	stats          string
}

func (r *run) flushStats(ctx context.Context) error {
	if len(r.pendingStats) == 0 {
		return nil
	}
	ids, values := make([]string, len(r.pendingStats)), make([]string, len(r.pendingStats))
	for index, item := range r.pendingStats {
		ids[index], values[index] = item.conversationID, item.stats
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("fill ticket stats: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `UPDATE conversations c SET origin=c.origin || jsonb_build_object('stats',s.stats::jsonb)
		FROM unnest($2::uuid[],$3::text[]) AS s(id,stats)
		WHERE c.workspace_id=$1 AND c.id=s.id AND c.origin IS NOT NULL AND NOT c.origin ? 'stats'`, r.workspaceID, ids, values); err != nil {
		return fmt.Errorf("fill ticket stats: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("fill ticket stats: %w", err)
	}
	r.pendingStats = r.pendingStats[:0]
	return nil
}

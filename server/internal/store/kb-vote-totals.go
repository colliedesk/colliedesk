package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// KBArticleVoteTotals returns what the page of each of the workspace's articles ids counts of its
// votes: current and imported together, as publicKBVoteCount adds them. One query for all of them; an
// id of no article of the workspace is left out.
func (s *Store) KBArticleVoteTotals(ctx context.Context, workspaceID string, ids []string) (map[string]domain.KBVoteTally, error) {
	totals := make(map[string]domain.KBVoteTally, len(ids))
	if len(ids) == 0 {
		return totals, nil
	}
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		rows, err := tx.Query(ctx, `SELECT x.id::text, v.up + v.imported_up, v.down + v.imported_down
			FROM unnest($2::uuid[]) x(id) JOIN kb_articles a ON a.workspace_id=$1 AND a.id=x.id,
			LATERAL (`+strings.ReplaceAll(kbVoteCountsSQL, "$2", "x.id")+`) v(up, down, imported_up, imported_down)`, workspaceID, ids)
		if err != nil {
			return struct{}{}, err
		}
		var id string
		var helpful, notHelpful int64
		_, err = pgx.ForEachRow(rows, []any{&id, &helpful, &notHelpful}, func() error {
			totals[id] = domain.KBVoteTally{Helpful: int(helpful), NotHelpful: int(notHelpful)}
			return nil
		})
		return struct{}{}, err
	})
	if err != nil {
		return nil, fmt.Errorf("count knowledge base article votes: %w", err)
	}
	return totals, nil
}

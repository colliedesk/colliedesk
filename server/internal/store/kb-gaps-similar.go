package store

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/textmask"
)

// similarOpenKBGaps are the open gaps whose title scores at least KBGapMatchThreshold against key,
// the closest first.
func similarOpenKBGaps(ctx context.Context, tx pgx.Tx, workspaceID, key string) ([]string, error) {
	candidates, err := kbGapCandidatesFor(ctx, tx, workspaceID, key, "open")
	if err != nil {
		return nil, err
	}
	type scored struct {
		id    string
		score float64
	}
	var matches []scored
	for _, g := range candidates {
		if score := kbGapScore(key, g.key); score >= KBGapMatchThreshold {
			matches = append(matches, scored{g.id, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	ids := make([]string, len(matches))
	for index, m := range matches {
		ids[index] = m.id
	}
	return ids, nil
}

// listSimilarKBGaps lists the open gaps close to filter.Similar, the closest first: the gaps a new
// request with those words would join or could be added to.
func listSimilarKBGaps(ctx context.Context, tx pgx.Tx, workspaceID string, filter domain.KBGapFilter) ([]domain.KBGap, error) {
	key := kbGapKey(textmask.Mask(filter.Similar))
	gaps := []domain.KBGap{}
	if len(kbGapTerms(key)) == 0 {
		return gaps, nil
	}
	ids, err := similarOpenKBGaps(ctx, tx, workspaceID, key)
	if err != nil || len(ids) == 0 {
		return gaps, err
	}
	rows, err := tx.Query(ctx, fmt.Sprintf(kbGapSelect, `WHERE g.workspace_id=$1 AND g.id = ANY($2::uuid[])
		AND ($3 = '' OR EXISTS (SELECT 1 FROM kb_gap_signals k WHERE k.workspace_id=g.workspace_id AND k.gap_id=g.id AND k.kind=$3))`),
		workspaceID, ids, filter.Kind)
	if err != nil {
		return nil, err
	}
	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBGap, error) { return scanKBGap(row) })
	if err != nil {
		return nil, err
	}
	byID := make(map[string]domain.KBGap, len(found))
	for _, g := range found {
		byID[g.ID] = g
	}
	for _, id := range ids {
		if g, ok := byID[id]; ok && len(gaps) < filter.Limit {
			gaps = append(gaps, g)
		}
	}
	return gaps, nil
}

package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/textmask"
)

const (
	// kbGapTextRunes is the longest signal text and gap title kept.
	kbGapTextRunes = 1000
	// kbGapCandidates bounds the gaps sharing a word with a signal that are scored against it.
	kbGapCandidates = 200
	// kbGapReopenAfter is how long after a fix new signals reopen the gap instead of only counting.
	kbGapReopenAfter = "7 days"
)

// placeKBGapSignal masks a signal's text and files it under the gap with its folded words, else the
// best gap scoring at least KBGapMatchThreshold, else a new gap; then counts that gap again. A
// source filed before with the same text is left as it is; with new text it moves.
func placeKBGapSignal(ctx context.Context, tx pgx.Tx, workspaceID string, signal kbGapSignal) error {
	text := cutRunes(strings.Join(strings.Fields(textmask.Mask(signal.text)), " "), kbGapTextRunes)
	key := kbGapKey(text)
	if key == "" {
		return nil
	}
	var oldGap, oldText string
	err := tx.QueryRow(ctx, `SELECT gap_id::text, text FROM kb_gap_signals WHERE workspace_id=$1 AND kind=$2 AND source_id=$3`,
		workspaceID, signal.kind, signal.sourceID).Scan(&oldGap, &oldText)
	switch {
	case err == nil && oldText == text:
		return nil
	case err == nil:
		if _, err := tx.Exec(ctx, `DELETE FROM kb_gap_signals WHERE workspace_id=$1 AND kind=$2 AND source_id=$3`,
			workspaceID, signal.kind, signal.sourceID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT kb_gap_recount($1, $2)`, workspaceID, oldGap); err != nil {
			return err
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return err
	}
	gap, err := findKBGap(ctx, tx, workspaceID, key)
	if err != nil {
		return err
	}
	if gap == "" {
		if err := tx.QueryRow(ctx, `INSERT INTO kb_gaps(workspace_id, title, query_key, first_seen, last_seen)
			VALUES ($1, $2, $3, $4, $4) RETURNING id::text`, workspaceID, text, key, signal.at).Scan(&gap); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO kb_gap_signals(workspace_id, gap_id, kind, source_id, text, asker_key, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, workspaceID, gap, signal.kind, signal.sourceID, text, signal.asker[:], signal.at); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT kb_gap_recount($1, $2)`, workspaceID, gap); err != nil {
		return err
	}
	// A fix that did not stop the questions: open the gap again. A dismissed gap only counts.
	_, err = tx.Exec(ctx, `UPDATE kb_gaps SET status='open', fixed_by_article_id=NULL, fixed_automatically=false,
			status_changed_at=now(), status_changed_by_agent_id=NULL
		WHERE workspace_id=$1 AND id=$2 AND status='fixed'
			AND $3 > COALESCE(status_changed_at, created_at) + interval '`+kbGapReopenAfter+`'`, workspaceID, gap, signal.at)
	return err
}

// findKBGap is the gap whose folded words are key, else the best of the gaps sharing a word with it
// that scores at least KBGapMatchThreshold against key; "" when there is none. Gaps of every status
// count: a dismissed gap keeps counting, a fixed one may reopen.
func findKBGap(ctx context.Context, tx pgx.Tx, workspaceID, key string) (string, error) {
	var gap string
	err := tx.QueryRow(ctx, `SELECT id::text FROM kb_gaps WHERE workspace_id=$1 AND query_key=$2 ORDER BY created_at, id LIMIT 1`,
		workspaceID, key).Scan(&gap)
	if !errors.Is(err, pgx.ErrNoRows) {
		return gap, err
	}
	gaps, err := kbGapCandidatesFor(ctx, tx, workspaceID, key, "")
	if err != nil {
		return "", err
	}
	best, bestScore := "", 0.0
	for _, g := range gaps {
		if score := kbGapScore(key, g.key); score >= KBGapMatchThreshold && score > bestScore {
			best, bestScore = g.id, score
		}
	}
	return best, nil
}

type kbGapCandidate struct{ id, key string }

// kbGapCandidatesFor lists the gaps sharing a word, or the start of a long word, with key, those
// sharing the most first. With a status the candidates are the gaps with it, keyed by their titles.
func kbGapCandidatesFor(ctx context.Context, tx pgx.Tx, workspaceID, key, status string) ([]kbGapCandidate, error) {
	words := kbGapWordsQuery(key)
	if words == "" {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `SELECT id::text, CASE WHEN $4 = '' THEN query_key ELSE title END FROM kb_gaps
		WHERE workspace_id=$1 AND kb_search_vector(query_key) @@ to_tsquery('simple', $2) AND ($4 = '' OR status = $4)
		ORDER BY ts_rank(kb_search_vector(query_key), to_tsquery('simple', $2)) DESC, created_at, id LIMIT $3`,
		workspaceID, words, kbGapCandidates, status)
	if err != nil {
		return nil, err
	}
	gaps, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (kbGapCandidate, error) {
		var g kbGapCandidate
		return g, row.Scan(&g.id, &g.key)
	})
	if err != nil {
		return nil, err
	}
	if status != "" {
		for index := range gaps {
			gaps[index].key = kbGapKey(gaps[index].key)
		}
	}
	return gaps, nil
}

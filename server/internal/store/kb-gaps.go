package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// kbGapSelect reads gaps with their kinds, their rank (distinct askers of the last
// domain.KBGapRankDays days), the article that fixes them and who changed their status last.
// %s is the rest of the query after FROM; it names the gap g.
var kbGapSelect = `SELECT g.id::text, g.title, g.status, s.kinds, g.askers, s.recent, g.first_seen, g.last_seen,
		a.id::text, a.title, g.fixed_automatically, p.id::text, COALESCE(NULLIF(p.display_name, ''), p.email), g.status_changed_at
	FROM kb_gaps g
	CROSS JOIN LATERAL (SELECT COALESCE(array_agg(DISTINCT k.kind ORDER BY k.kind), '{}') AS kinds,
			count(DISTINCT k.asker_key) FILTER (WHERE k.created_at > now() - interval '` + kbGapRankWindow + `') AS recent
		FROM kb_gap_signals k WHERE k.workspace_id=g.workspace_id AND k.gap_id=g.id) s
	LEFT JOIN kb_articles a ON a.workspace_id=g.workspace_id AND a.id=g.fixed_by_article_id
	LEFT JOIN agents p ON p.workspace_id=g.workspace_id AND p.id=g.status_changed_by_agent_id
	%s`

var kbGapRankWindow = strconv.Itoa(domain.KBGapRankDays) + " days"

func scanKBGap(row pgx.Row) (domain.KBGap, error) {
	var g domain.KBGap
	var articleID, articleTitle, agentID, agentName *string
	err := row.Scan(&g.ID, &g.Title, &g.Status, &g.Kinds, &g.Askers, &g.RecentAskers, &g.FirstSeen, &g.LastSeen,
		&articleID, &articleTitle, &g.FixedAutomatically, &agentID, &agentName, &g.StatusChangedAt)
	if articleID != nil && articleTitle != nil {
		g.FixedByArticle = &domain.KBGapArticle{ID: *articleID, Title: *articleTitle}
	}
	g.StatusChangedBy = savedReplyPerson(agentID, agentName)
	return g, err
}

// ListKBGaps lists the workspace's gaps with filter's status, and a signal of its kind when it has
// one: most distinct askers in the last 30 days first, then the gap seen last.
func (s *Store) ListKBGaps(ctx context.Context, workspaceID string, filter domain.KBGapFilter) ([]domain.KBGap, error) {
	filter, err := filter.Clean()
	if err != nil {
		return nil, err
	}
	gaps, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.KBGap, error) {
		rows, err := tx.Query(ctx, fmt.Sprintf(kbGapSelect, `WHERE g.workspace_id=$1 AND g.status=$2
			AND ($3 = '' OR EXISTS (SELECT 1 FROM kb_gap_signals k WHERE k.workspace_id=g.workspace_id AND k.gap_id=g.id AND k.kind=$3))
			ORDER BY s.recent DESC, g.last_seen DESC, g.id LIMIT $4`), workspaceID, filter.Status, filter.Kind, filter.Limit)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBGap, error) { return scanKBGap(row) })
	})
	return gaps, kbError("list knowledge gaps", err)
}

// GetKBGap is one gap with its newest domain.MaxKBGapSignalsShown signals. domain.ErrKBGapNotFound
// for a gap of another workspace.
func (s *Store) GetKBGap(ctx context.Context, workspaceID, id string) (domain.KBGapDetail, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBGapDetail{}, domain.ErrKBGapNotFound
	}
	detail, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBGapDetail, error) {
		return readKBGap(ctx, tx, workspaceID, id)
	})
	return detail, kbError("read knowledge gap", err)
}

// KBGapForDraft reads one gap of the workspace with its newest signals, for an AI draft of the article
// it misses. Run it in the workspace's scoped transaction.
func KBGapForDraft(ctx context.Context, tx pgx.Tx, workspaceID, id string) (domain.KBGapDetail, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBGapDetail{}, domain.ErrKBGapNotFound
	}
	return readKBGap(ctx, tx, workspaceID, id)
}

func readKBGapRow(ctx context.Context, tx pgx.Tx, workspaceID, id string) (domain.KBGap, error) {
	gap, err := scanKBGap(tx.QueryRow(ctx, fmt.Sprintf(kbGapSelect, `WHERE g.workspace_id=$1 AND g.id=$2`), workspaceID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return gap, domain.ErrKBGapNotFound
	}
	return gap, err
}

func readKBGap(ctx context.Context, tx pgx.Tx, workspaceID, id string) (domain.KBGapDetail, error) {
	gap, err := readKBGapRow(ctx, tx, workspaceID, id)
	if err != nil {
		return domain.KBGapDetail{}, err
	}
	detail := domain.KBGapDetail{KBGap: gap}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM kb_gap_signals WHERE workspace_id=$1 AND gap_id=$2`, workspaceID, id).Scan(&detail.SignalCount); err != nil {
		return detail, err
	}
	rows, err := tx.Query(ctx, `SELECT kind, text, created_at FROM kb_gap_signals WHERE workspace_id=$1 AND gap_id=$2
		ORDER BY created_at DESC, id DESC LIMIT $3`, workspaceID, id, domain.MaxKBGapSignalsShown)
	if err != nil {
		return detail, err
	}
	detail.Signals, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBGapSignal, error) {
		var signal domain.KBGapSignal
		return signal, row.Scan(&signal.Kind, &signal.Text, &signal.CreatedAt)
	})
	return detail, err
}

// UpdateKBGap sets the gap's status as actor: fixed by an article of the workspace outside the trash,
// dismissed, or open again. Open and dismissed clear the article; every change undoes an automatic fix.
// domain.ErrKBGapNotFound for a gap and domain.ErrKBArticleNotFound for an article of another workspace.
func (s *Store) UpdateKBGap(ctx context.Context, workspaceID string, actor Actor, id string, change domain.KBGapChange) (domain.KBGap, error) {
	status, articleID, err := change.Clean()
	if err != nil {
		return domain.KBGap{}, err
	}
	if !savedReplyID.MatchString(id) {
		return domain.KBGap{}, domain.ErrKBGapNotFound
	}
	if articleID != "" && !savedReplyID.MatchString(articleID) {
		return domain.KBGap{}, domain.ErrKBArticleNotFound
	}
	gap, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBGap, error) {
		if articleID != "" {
			var found bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM kb_articles WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL)`,
				workspaceID, articleID).Scan(&found); err != nil {
				return domain.KBGap{}, err
			}
			if !found {
				return domain.KBGap{}, domain.ErrKBArticleNotFound
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE kb_gaps SET status=$3,
				fixed_by_article_id = CASE WHEN $3 = 'fixed' THEN COALESCE($4::uuid, fixed_by_article_id) END,
				fixed_automatically=false, status_changed_by_agent_id=$5, status_changed_at=now()
			WHERE workspace_id=$1 AND id=$2`, workspaceID, id, status, optionalID(articleID), optionalID(actor.AgentID))
		if err != nil {
			return domain.KBGap{}, err
		}
		if tag.RowsAffected() == 0 {
			return domain.KBGap{}, domain.ErrKBGapNotFound
		}
		return readKBGapRow(ctx, tx, workspaceID, id)
	})
	return gap, kbError("change knowledge gap", err)
}

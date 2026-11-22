package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/textmask"
	"github.com/jackc/pgx/v5"
)

// KBSearchesPerMinute is how many help center searches one workspace keeps in a minute. The search
// limiter bounds each client address; this bounds the rows many addresses can add to one workspace.
// A search past it is still answered, only not kept.
const KBSearchesPerMinute = 120

// kbSearchOpenWindow is how long after a search a reader's open from its results still counts.
const kbSearchOpenWindow = "24 hours"

// LogKBSearch keeps a reader's search of help center helpCenterID ("" is the primary): the words
// masked, their folded key, and how many articles the page listed. It returns the new row's id, or 0
// when the workspace already kept KBSearchesPerMinute searches in the last minute or the words are
// empty once masked. A help center of another workspace is refused by the table's foreign key.
func (s *Store) LogKBSearch(ctx context.Context, workspaceID, helpCenterID, query string, results int) (int64, error) {
	query = strings.Join(strings.Fields(textmask.Mask(query)), " ")
	if query == "" {
		return 0, nil
	}
	key := strings.Join(strings.Fields(FoldForAIRetrieval(query)), " ")
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (int64, error) {
		// One statement: the count and the insert read the same snapshot, so the cap holds per
		// statement; searches arriving at the same instant can pass it by as many as run at once.
		var id int64
		insert, args := onHelpCenter(helpCenterID, `INSERT INTO kb_searches (workspace_id, help_center_id, query, query_key, results)
			SELECT $1, $H, $2, $3, $4
			WHERE (SELECT count(*) FROM kb_searches WHERE workspace_id=$1 AND created_at > now() - interval '1 minute') < $5
			RETURNING id`, workspaceID, query, key, results, KBSearchesPerMinute)
		err := tx.QueryRow(ctx, insert, args...).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		if err != nil {
			return 0, fmt.Errorf("log help center search: %w", err)
		}
		return id, nil
	})
}

// OpenKBSearch records that the reader of search searchID opened articleID from its results. Only the
// first open of a search made in the last 24 hours counts; any other call changes nothing.
func (s *Store) OpenKBSearch(ctx context.Context, workspaceID string, searchID int64, articleID string) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		_, err := tx.Exec(ctx, `UPDATE kb_searches SET opened_article_id=$3
			WHERE workspace_id=$1 AND id=$2 AND opened_article_id IS NULL AND created_at > now() - $4::interval
			AND EXISTS (SELECT 1 FROM kb_articles a WHERE a.workspace_id=$1 AND a.id=$3)`,
			workspaceID, searchID, articleID, kbSearchOpenWindow)
		if err != nil {
			return struct{}{}, fmt.Errorf("record article opened from a search: %w", err)
		}
		return struct{}{}, nil
	})
	return err
}

// DeleteOldKBSearches deletes, in every workspace, the help center searches older than retention.
// serve runs it at start and daily, with kb_searches.retention_days.
func (s *Store) DeleteOldKBSearches(ctx context.Context, retention time.Duration) (int64, error) {
	var deleted int64
	err := s.db.Privileged(ctx, database.PrivilegeKBSearchRetention, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM kb_searches WHERE created_at < $1`, s.now().Add(-retention))
		if err != nil {
			return fmt.Errorf("delete old help center searches: %w", err)
		}
		deleted = tag.RowsAffected()
		return nil
	})
	return deleted, err
}

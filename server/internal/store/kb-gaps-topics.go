package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/database"
)

// kbGapTopicsMinConversations is how many conversations a workspace must have resolved in the last
// 30 days before its resolved conversations are read for recurring topics: fewer say nothing yet.
const kbGapTopicsMinConversations = 20

// fixKBGapsByArticle marks fixed, by the article, every open gap whose title scores at least
// KBGapMatchThreshold against the published article's title. False leaves the queued row for later
// while another transaction holds the article.
func fixKBGapsByArticle(ctx context.Context, tx pgx.Tx, workspaceID, articleID string) (bool, error) {
	var title string
	err := tx.QueryRow(ctx, `SELECT a.title FROM kb_articles a WHERE a.workspace_id=$1 AND a.id=$2::uuid
			AND EXISTS (SELECT 1 FROM kb_published_articles p WHERE p.workspace_id=a.workspace_id AND p.id=a.id)
		FOR KEY SHARE OF a SKIP LOCKED`, workspaceID, articleID).Scan(&title)
	if errors.Is(err, pgx.ErrNoRows) {
		var held bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM kb_published_articles WHERE workspace_id=$1 AND id=$2::uuid)`,
			workspaceID, articleID).Scan(&held)
		return !held, err
	}
	if err != nil {
		return false, err
	}
	key := kbGapKey(title)
	gaps, err := kbGapCandidatesFor(ctx, tx, workspaceID, key, "open")
	if err != nil {
		return false, err
	}
	var fixed []string
	for _, g := range gaps {
		if kbGapScore(key, g.key) >= KBGapMatchThreshold {
			fixed = append(fixed, g.id)
		}
	}
	_, err = tx.Exec(ctx, `UPDATE kb_gaps SET status='fixed', fixed_by_article_id=$2, fixed_automatically=true,
			status_changed_at=now(), status_changed_by_agent_id=NULL
		WHERE workspace_id=$1 AND id=ANY($3::uuid[]) AND status='open'`, workspaceID, articleID, fixed)
	return true, err
}

// BuildKBGapTopics files, as recurring topics, the conversations resolved in the last 30 days whose
// subject no published article title matches, in every workspace that resolved at least
// kbGapTopicsMinConversations of them; it calls no AI. It returns how many conversations it read.
func (s *Store) BuildKBGapTopics(ctx context.Context) (int, error) {
	var workspaces []string
	err := s.db.Privileged(ctx, database.PrivilegeKBGapTopics, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT d.workspace_id::text FROM ai_retrieval_docs d
			JOIN conversations c ON c.workspace_id=d.workspace_id AND c.id=d.conversation_id
			WHERE d.conversation_id IS NOT NULL AND c.status='resolved' AND d.updated_at > now() - interval '30 days'
			GROUP BY d.workspace_id HAVING count(*) >= $1 ORDER BY d.workspace_id`, kbGapTopicsMinConversations)
		if err != nil {
			return err
		}
		workspaces, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("list workspaces with resolved conversations: %w", err)
	}
	read := 0
	var failed []error
	for _, workspaceID := range workspaces {
		n, err := s.buildKBGapTopics(ctx, workspaceID)
		read += n
		if err != nil {
			failed = append(failed, fmt.Errorf("recurring topics of workspace %s: %w", workspaceID, err))
		}
		if ctx.Err() != nil {
			break
		}
	}
	return read, errors.Join(append(failed, ctx.Err())...)
}

type kbGapConversation struct {
	id, subject, contact string
	at                   time.Time
}

func (s *Store) buildKBGapTopics(ctx context.Context, workspaceID string) (read int, err error) {
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if locked, err := lockKBGaps(ctx, tx, workspaceID); err != nil || !locked {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT a.title FROM kb_articles a
			JOIN kb_published_articles p ON p.workspace_id=a.workspace_id AND p.id=a.id WHERE a.workspace_id=$1`, workspaceID)
		if err != nil {
			return err
		}
		titles, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT d.conversation_id::text, d.title, COALESCE(c.contact_id::text, c.id::text), d.updated_at
			FROM ai_retrieval_docs d JOIN conversations c ON c.workspace_id=d.workspace_id AND c.id=d.conversation_id
			WHERE d.workspace_id=$1 AND d.conversation_id IS NOT NULL AND c.status='resolved' AND d.updated_at > now() - interval '30 days'
			ORDER BY d.updated_at, d.conversation_id`, workspaceID)
		if err != nil {
			return err
		}
		conversations, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (kbGapConversation, error) {
			var c kbGapConversation
			return c, row.Scan(&c.id, &c.subject, &c.contact, &c.at)
		})
		if err != nil || len(conversations) < kbGapTopicsMinConversations {
			return err
		}
		keys := make([]string, len(titles))
		for index, title := range titles {
			keys[index] = kbGapKey(title)
		}
		for _, c := range conversations {
			if answeredByAny(kbGapKey(c.subject), keys) {
				continue
			}
			signal := kbGapSignal{kind: "recurring_topic", sourceID: c.id, text: c.subject, at: c.at,
				asker: kbGapAsker("contact", []byte(c.contact))}
			if err := placeKBGapSignal(ctx, tx, workspaceID, signal); err != nil {
				return err
			}
		}
		read = len(conversations)
		return nil
	})
	return read, err
}

func answeredByAny(key string, articles []string) bool {
	for _, article := range articles {
		if kbGapScore(key, article) >= KBGapMatchThreshold {
			return true
		}
	}
	return false
}

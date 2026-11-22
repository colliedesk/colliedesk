package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// kbGapSignal is one thing a customer could not find, before it is masked and filed under a gap.
type kbGapSignal struct {
	kind, sourceID, text string
	asker                [32]byte
	at                   time.Time
}

func kbGapAsker(prefix string, who []byte) [32]byte {
	return sha256.Sum256(append([]byte(prefix+":"), who...))
}

// readKBGapSource reads the source of a queued signal. ok is false while another transaction holds
// the source; a nil signal with ok true means there is nothing to file (the source is gone, or no
// longer a signal: the vote became "Yes", the chat has no question).
func readKBGapSource(ctx context.Context, tx pgx.Tx, workspaceID, kind, sourceID string) (signal *kbGapSignal, ok bool, err error) {
	s := kbGapSignal{kind: kind, sourceID: sourceID}
	var query, exists string
	// A chat turn may have no question (a reader who only opened the chat).
	var question *string
	var scan func(pgx.Row) error
	switch kind {
	case "search_no_result":
		// A search is its own asker: searches leave no trace of who made them.
		query = `SELECT query, created_at FROM kb_searches WHERE workspace_id=$1 AND id=$2::bigint`
		scan = func(row pgx.Row) error {
			s.asker = kbGapAsker("search", []byte(sourceID))
			return row.Scan(&s.text, &s.at)
		}
	case "vote_no":
		// What the reader said was missing, or else the article that did not help.
		query = `SELECT COALESCE(v.missing, a.title), v.voter, v.updated_at FROM kb_article_votes v
			JOIN kb_articles a ON a.workspace_id=v.workspace_id AND a.id=v.article_id
			WHERE v.workspace_id=$1 AND v.id=$2::uuid AND NOT v.helpful AND v.reset_id IS NULL`
		scan = func(row pgx.Row) error {
			var voter []byte
			err := row.Scan(&s.text, &voter, &s.at)
			s.asker = kbGapAsker("vote", voter)
			return err
		}
	case "chat_handoff":
		// The session is locked so a chat deleted at the same time takes this signal with it. Its
		// newest question says what the reader came for.
		query = `SELECT (SELECT t.question FROM ai_chat_turns t WHERE t.workspace_id=c.workspace_id AND t.session_id=c.id
				AND t.question IS NOT NULL ORDER BY t.seq DESC LIMIT 1), COALESCE(c.handed_off_at, now())
			FROM ai_chat_sessions c WHERE c.workspace_id=$1 AND c.id=$2::uuid AND c.status='handed_off'
			FOR KEY SHARE OF c SKIP LOCKED`
		exists = `SELECT EXISTS (SELECT 1 FROM ai_chat_sessions WHERE workspace_id=$1 AND id=$2::uuid AND status='handed_off')`
		scan = func(row pgx.Row) error {
			s.asker = kbGapAsker("chat", []byte(sourceID))
			return row.Scan(&question, &s.at)
		}
	case "chat_unanswered":
		const unanswered = `FROM ai_chat_turns t WHERE t.workspace_id=$1 AND t.id=$2::uuid
			AND (t.status='no_source' OR EXISTS (SELECT 1 FROM ai_chat_feedback f
				WHERE f.workspace_id=t.workspace_id AND f.turn_id=t.id AND f.rating='not_helpful'))`
		query = `SELECT t.question, t.session_id::text, t.created_at ` + unanswered + ` FOR KEY SHARE OF t SKIP LOCKED`
		exists = `SELECT EXISTS (SELECT 1 ` + unanswered + `)`
		scan = func(row pgx.Row) error {
			var session string
			err := row.Scan(&question, &session, &s.at)
			s.asker = kbGapAsker("chat", []byte(session))
			return err
		}
	default:
		return nil, false, fmt.Errorf("unknown knowledge gap signal kind %q", kind)
	}
	err = scan(tx.QueryRow(ctx, query, workspaceID, sourceID))
	if errors.Is(err, pgx.ErrNoRows) {
		// Not found under a lock: held by another transaction when the row is still there.
		held := false
		if exists != "" {
			if err := tx.QueryRow(ctx, exists, workspaceID, sourceID).Scan(&held); err != nil {
				return nil, false, err
			}
		}
		return nil, !held, nil
	}
	if err != nil {
		return nil, false, err
	}
	if kind == "chat_handoff" || kind == "chat_unanswered" {
		if question == nil {
			return nil, true, nil
		}
		s.text = *question
	}
	return &s, true, nil
}

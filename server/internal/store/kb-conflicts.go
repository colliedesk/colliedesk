package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/textmask"
)

// KBArticleConflicts are the sent replies the small model found to contradict article id of
// workspaceID: open ones first, then cleared ones, newest first. An article in the trash keeps them.
// domain.ErrKBArticleNotFound when the workspace has no such article.
func (s *Store) KBArticleConflicts(ctx context.Context, workspaceID, id string) (domain.KBArticleConflicts, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBArticleConflicts{}, domain.ErrKBArticleNotFound
	}
	conflicts, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticleConflicts, error) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM kb_articles WHERE workspace_id=$1 AND id=$2)`, workspaceID, id).Scan(&exists); err != nil {
			return domain.KBArticleConflicts{}, err
		}
		if !exists {
			return domain.KBArticleConflicts{}, domain.ErrKBArticleNotFound
		}
		return readKBArticleConflicts(ctx, tx, workspaceID, id)
	})
	return conflicts, kbError("read knowledge base article conflicts", err)
}

// ClearKBArticleConflicts marks every open conflict of article id cleared by actor at now: the article
// no longer shows as out of date until another reply contradicts it. A conflict cleared before keeps
// who cleared it. domain.ErrKBArticleNotFound for an article in the trash or of another workspace.
func (s *Store) ClearKBArticleConflicts(ctx context.Context, workspaceID string, actor Actor, id string, now time.Time) (domain.KBArticleConflicts, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBArticleConflicts{}, domain.ErrKBArticleNotFound
	}
	conflicts, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticleConflicts, error) {
		err := tx.QueryRow(ctx, `SELECT id::text FROM kb_articles WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL`, workspaceID, id).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.KBArticleConflicts{}, domain.ErrKBArticleNotFound
		}
		if err != nil {
			return domain.KBArticleConflicts{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE kb_article_conflicts k SET cleared_at=$3, cleared_by_agent_id=$4, cleared_by_token_id=t.id, cleared_by_token_label=t.label
			FROM (SELECT 1) one LEFT JOIN sessions t ON t.workspace_id=$1 AND t.id=$5
			WHERE k.workspace_id=$1 AND k.article_id=$2 AND k.cleared_at IS NULL`,
			workspaceID, id, now.UTC(), optionalID(actor.AgentID), optionalID(actor.TokenID)); err != nil {
			return domain.KBArticleConflicts{}, fmt.Errorf("clear article conflicts: %w", err)
		}
		return readKBArticleConflicts(ctx, tx, workspaceID, id)
	})
	return conflicts, kbError("clear knowledge base article conflicts", err)
}

// readKBArticleConflicts reads the open count and the newest conflicts of article id in tx.
func readKBArticleConflicts(ctx context.Context, tx pgx.Tx, workspaceID, id string) (domain.KBArticleConflicts, error) {
	out := domain.KBArticleConflicts{ArticleID: id, Conflicts: []domain.KBArticleConflict{}}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM kb_article_conflicts WHERE workspace_id=$1 AND article_id=$2 AND cleared_at IS NULL`,
		workspaceID, id).Scan(&out.Open); err != nil {
		return out, fmt.Errorf("count article conflicts: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT k.id::text, k.reason, k.created_at, m.id::text, m.body, m.created_at,
			m.author_agent_id::text, COALESCE(NULLIF(a.display_name, ''), a.email),
			c.id::text, COALESCE(c.display_ref, ''), c.subject,
			k.cleared_at, k.cleared_by_agent_id::text, COALESCE(NULLIF(p.display_name, ''), p.email),
			k.cleared_by_token_id::text, k.cleared_by_token_label
		FROM kb_article_conflicts k
		JOIN messages m ON m.workspace_id=k.workspace_id AND m.id=k.message_id
		JOIN conversations c ON c.workspace_id=m.workspace_id AND c.id=m.conversation_id
		LEFT JOIN agents a ON a.workspace_id=m.workspace_id AND a.id=m.author_agent_id
		LEFT JOIN agents p ON p.workspace_id=k.workspace_id AND p.id=k.cleared_by_agent_id
		WHERE k.workspace_id=$1 AND k.article_id=$2
		ORDER BY k.cleared_at IS NOT NULL, k.created_at DESC, k.id LIMIT $3`, workspaceID, id, domain.MaxKBArticleConflictsListed)
	if err != nil {
		return out, fmt.Errorf("list article conflicts: %w", err)
	}
	conflicts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBArticleConflict, error) {
		var conflict domain.KBArticleConflict
		var conversation domain.KBConflictConversation
		var body string
		var authorID, authorName, clearedByID, clearedByName, tokenID, tokenLabel *string
		if err := row.Scan(&conflict.ID, &conflict.Reason, &conflict.CreatedAt, &conflict.Reply.MessageID, &body, &conflict.Reply.SentAt,
			&authorID, &authorName, &conversation.ID, &conversation.DisplayRef, &conversation.Subject,
			&conflict.ClearedAt, &clearedByID, &clearedByName, &tokenID, &tokenLabel); err != nil {
			return conflict, err
		}
		conflict.CreatedAt, conflict.Reply.SentAt = domain.UTC(conflict.CreatedAt), domain.UTC(conflict.Reply.SentAt)
		conflict.Reply.Excerpt = kbConflictExcerpt(body)
		conflict.Reply.Author = savedReplyPerson(authorID, authorName)
		conflict.Reply.Conversation = &conversation
		if conflict.ClearedAt != nil {
			at := domain.UTC(*conflict.ClearedAt)
			conflict.ClearedAt = &at
		}
		conflict.ClearedBy = savedReplyPerson(clearedByID, clearedByName)
		if tokenLabel != nil {
			conflict.ClearedWithToken = &domain.SavedReplyToken{ID: tokenID, Label: *tokenLabel}
		}
		return conflict, nil
	})
	if err != nil {
		return out, fmt.Errorf("list article conflicts: %w", err)
	}
	out.Conflicts = append(out.Conflicts, conflicts...)
	return out, nil
}

// kbConflictExcerpt is the start of a reply on one line: masked whole first, so a cut never leaves part
// of an address or number behind.
func kbConflictExcerpt(body string) string {
	line := strings.Join(strings.Fields(textmask.Mask(body)), " ")
	if runes := []rune(line); len(runes) > domain.KBConflictExcerptRunes {
		line = strings.TrimSpace(string(runes[:domain.KBConflictExcerptRunes])) + "…"
	}
	return line
}

// fillKBOpenConflicts sets the OpenConflicts of articles: the conflicts no editor cleared yet.
func fillKBOpenConflicts(ctx context.Context, tx pgx.Tx, workspaceID string, items ...*domain.KBArticleSummary) error {
	ids := make([]string, 0, len(items))
	byID := make(map[string]*domain.KBArticleSummary, len(items))
	for _, item := range items {
		item.OpenConflicts = 0
		ids = append(ids, item.ID)
		byID[item.ID] = item
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT article_id::text, count(*) FROM kb_article_conflicts
		WHERE workspace_id=$1 AND article_id=ANY($2::uuid[]) AND cleared_at IS NULL GROUP BY article_id`, workspaceID, ids)
	if err != nil {
		return fmt.Errorf("count knowledge base article conflicts: %w", err)
	}
	var articleID string
	var open int
	if _, err := pgx.ForEachRow(rows, []any{&articleID, &open}, func() error {
		if item := byID[articleID]; item != nil {
			item.OpenConflicts = open
		}
		return nil
	}); err != nil {
		return fmt.Errorf("count knowledge base article conflicts: %w", err)
	}
	return nil
}

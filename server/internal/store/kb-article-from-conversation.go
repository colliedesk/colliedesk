package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// maxKBStartConversations bounds the conversations a draft started from a gap keeps as its sources.
const maxKBStartConversations = 8

// untitledKBAnswer titles an article made from a reply whose subject has no words left once masked.
const untitledKBAnswer = "Untitled answer"

// KBGapForArticle is the title of one gap of the workspace and the conversations that asked for its
// article or keep asking its question, the one that asked last first: what a draft started from the
// gap is titled and opened beside. domain.ErrKBGapNotFound for a gap of another workspace.
func (s *Store) KBGapForArticle(ctx context.Context, workspaceID, gapID string) (string, []string, error) {
	if !savedReplyID.MatchString(gapID) {
		return "", nil, domain.ErrKBGapNotFound
	}
	type started struct {
		title string
		ids   []string
	}
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (started, error) {
		var out started
		err := tx.QueryRow(ctx, `SELECT title FROM kb_gaps WHERE workspace_id=$1 AND id=$2`, workspaceID, gapID).Scan(&out.title)
		if errors.Is(err, pgx.ErrNoRows) {
			return out, domain.ErrKBGapNotFound
		}
		if err != nil {
			return out, err
		}
		conversations, err := kbGapConversations(ctx, tx, workspaceID, gapID, maxKBStartConversations)
		for _, c := range conversations {
			out.ids = append(out.ids, c.ConversationID)
		}
		return out, err
	})
	return result.title, result.ids, kbError("start an article from a gap", err)
}

// KBReplyForArticle reads a sent reply of the conversation for an article made from it: a title from
// the subject, the reply as its body, and the customer question just before the reply, all with
// email addresses, runs of 6 or more digits and the customer's and team's names masked.
// domain.ErrKBSourceConversationNotFound or domain.ErrKBSourceMessageNotFound when either is not the
// workspace's; a note or a notice is refused.
func (s *Store) KBReplyForArticle(ctx context.Context, workspaceID, conversationID, messageID string) (string, string, domain.KBArticleReplySource, error) {
	type made struct {
		title, body string
		source      domain.KBArticleReplySource
	}
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (made, error) {
		var out made
		conversation, err := readKBSourceConversation(ctx, tx, workspaceID, conversationID)
		if err != nil {
			return out, err
		}
		if !savedReplyID.MatchString(messageID) {
			return out, domain.ErrKBSourceMessageNotFound
		}
		var reply, question string
		var isReply bool
		err = tx.QueryRow(ctx, `SELECT m.body, m.kind='outgoing' AND m.notice_kind IS NULL,
				COALESCE((SELECT q.body FROM messages q WHERE q.workspace_id=m.workspace_id AND q.conversation_id=m.conversation_id
					AND q.kind='incoming' AND q.detached_at IS NULL AND (q.created_at, q.id) < (m.created_at, m.id)
					ORDER BY q.created_at DESC, q.id DESC LIMIT 1), '')
			FROM messages m WHERE m.workspace_id=$1 AND m.conversation_id=$2 AND m.id=$3`,
			workspaceID, conversationID, messageID).Scan(&reply, &isReply, &question)
		if errors.Is(err, pgx.ErrNoRows) {
			return out, domain.ErrKBSourceMessageNotFound
		}
		if err != nil {
			return out, err
		}
		if !isReply {
			return out, &domain.InvalidKBError{Message: "an article is made from a reply sent to the customer, not from a note or a notice"}
		}
		subject := strings.Join(strings.Fields(conversation.mask(conversation.subject)), " ")
		out.title = subject
		if len(kbGapTerms(kbGapKey(subject))) == 0 {
			out.title = untitledKBAnswer
		}
		out.body = strings.TrimSpace(conversation.mask(reply))
		out.source = domain.KBArticleReplySource{ConversationID: conversationID, DisplayRef: conversation.displayRef,
			Subject: subject, MessageID: messageID, Question: strings.TrimSpace(conversation.mask(question))}
		return out, nil
	})
	return result.title, result.body, result.source, kbError("make an article from a reply", err)
}

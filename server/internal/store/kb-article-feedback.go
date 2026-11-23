package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// ArticleFeedbackTag marks every conversation a reader's "No" on an article opened.
const ArticleFeedbackTag = "article-feedback"

// articleFeedbackNote asks the article's author on the conversation; it carries nothing of the reader.
const articleFeedbackNote = "A reader of an article you wrote said it did not help. Their line is above."

// How many conversations article feedback may open in articleFeedbackWindow: from one client address,
// and in one workspace. Anyone can type any address into a help center, and each one adds a contact.
const (
	articleFeedbackPerAddress   = 5
	articleFeedbackPerWorkspace = 50
	articleFeedbackWindow       = "10 minutes"
)

// KBArticleFeedback is a reader's "No" that left an address: the article's number, the reader (voter) and
// client address (address) as VoteKBArticle keeps them, the address typed (From), the article's public
// address for its slug (Page) and the help center the vote came through (HelpCenterID, "" for the primary),
// which the conversation opens on.
type KBArticleFeedback struct {
	Number         int64
	Voter, Address []byte
	From           string
	Page           func(slug string) string
	HelpCenterID   string
}

// OpenKBArticleFeedback opens one conversation in workspaceID from the reader's current "No" vote on the
// article, when the knowledge base turned feedback_conversations on and the vote has a line and no
// conversation yet: the address becomes (or is found as) a contact, the article and the masked line are
// the incoming message, the conversation is tagged ArticleFeedbackTag and the article's author, if still
// active, is asked on it. Past the caps nothing opens and nothing is stored, with no error. It runs as the
// app role (the help center reader has no grant on conversations or contacts), after the vote itself was
// written as the reader.
func (s *Store) OpenKBArticleFeedback(ctx context.Context, workspaceID string, in KBArticleFeedback) error {
	tx, err := s.db.BeginWorkspace(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("begin article feedback: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('article_feedback:'||$1::text,0))`, workspaceID); err != nil {
		return fmt.Errorf("lock article feedback: %w", err)
	}
	var voteID, title, slug, missing string
	var authorID *string
	err = tx.QueryRow(ctx, `SELECT v.id::text, a.title, a.slug, v.missing, au.id::text
		FROM kb_article_votes v JOIN kb_articles a ON a.workspace_id=v.workspace_id AND a.id=v.article_id
		JOIN help_centers h ON h.workspace_id=a.workspace_id AND h.primary_help_center
		LEFT JOIN agents au ON au.workspace_id=a.workspace_id AND au.id=a.author_agent_id AND au.deactivated_at IS NULL
		WHERE v.workspace_id=$1 AND a.number=$2 AND v.voter=$3 AND v.reset_id IS NULL AND NOT v.helpful
			AND v.missing IS NOT NULL AND v.conversation_id IS NULL AND h.feedback_conversations
		FOR UPDATE OF v`, workspaceID, in.Number, in.Voter).Scan(&voteID, &title, &slug, &missing, &authorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find article feedback vote: %w", err)
	}
	var full bool
	if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM kb_article_votes v JOIN conversations c ON c.workspace_id=v.workspace_id AND c.id=v.conversation_id
			WHERE v.workspace_id=$1 AND v.address=$2 AND c.created_at > now() - $3::interval) >= $4
		OR (SELECT count(*) FROM conversations WHERE workspace_id=$1 AND channel=$5 AND created_at > now() - $3::interval) >= $6`,
		workspaceID, in.Address, articleFeedbackWindow, articleFeedbackPerAddress, domain.ChannelArticleFeedback, articleFeedbackPerWorkspace).Scan(&full); err != nil {
		return fmt.Errorf("count article feedback: %w", err)
	}
	if full {
		return nil
	}
	from := strings.ToLower(strings.TrimSpace(in.From))
	now := s.now()
	var contactID, tagID, conversationID, noteID string
	if contactID, err = contactForEmail(ctx, tx, workspaceID, "", from); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `INSERT INTO tags(workspace_id,name) VALUES($1,$2)
		ON CONFLICT(workspace_id,name) DO UPDATE SET name=tags.name RETURNING id::text`, workspaceID, ArticleFeedbackTag).Scan(&tagID); err != nil {
		return fmt.Errorf("find or create tag: %w", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,created_at,updated_at,channel,help_center_id)
		VALUES($1,$2,$3,$4,$4,$5,(SELECT h.id FROM help_centers h
			WHERE h.workspace_id=$1 AND h.id=COALESCE(NULLIF($6,'')::uuid, primary_help_center_id($1)))) RETURNING id::text`,
		workspaceID, contactID, "Article feedback: "+title, now, domain.ChannelArticleFeedback, in.HelpCenterID).Scan(&conversationID); err != nil {
		return fmt.Errorf("create conversation: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO conversation_tags(workspace_id,conversation_id,tag_id) VALUES($1,$2,$3)`, workspaceID, conversationID, tagID); err != nil {
		return fmt.Errorf("tag conversation: %w", err)
	}
	// Authentication none: the address was typed into a public form, nobody proved they own it.
	meta, err := json.Marshal(domain.IncomingEmail{From: from, Authentication: "none", Via: domain.ViaHelpCenter, Attachments: []domain.EmailAttachment{}})
	if err != nil {
		return fmt.Errorf("encode article feedback metadata: %w", err)
	}
	body := "Article: " + title + " " + in.Page(slug) + "\n\n" + missing
	if _, err := tx.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,email_meta,created_at) VALUES($1,$2,'incoming',$3,$4,$5)`,
		workspaceID, conversationID, body, meta, now); err != nil {
		return fmt.Errorf("store article feedback message: %w", err)
	}
	if authorID != nil {
		if err := tx.QueryRow(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at) VALUES($1,$2,'note',$3,$4) RETURNING id::text`,
			workspaceID, conversationID, articleFeedbackNote, now).Scan(&noteID); err != nil {
			return fmt.Errorf("store article feedback note: %w", err)
		}
		if err := mention(ctx, tx, workspaceID, noteID, "", []string{*authorID}); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE kb_article_votes SET conversation_id=$3 WHERE workspace_id=$1 AND id=$2`, workspaceID, voteID, conversationID); err != nil {
		return fmt.Errorf("link vote to conversation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit article feedback: %w", err)
	}
	return nil
}
